// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Package controller holds the operator's reconcile loop. PdfRender is the
// primary resource and the Job it creates is the secondary one, found by
// name and owner reference. The Job's status drives the PdfRender's phase.
package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	rendersv1alpha1 "github.com/Steward-GRC/steward-pdf-renderer/api/v1alpha1"
)

// Config is the operator's static configuration, read once at start-up.
type Config struct {
	// RendererImage is the image every Job runs.
	RendererImage string
	// S3SecretName names the Secret in the render namespace that Jobs load
	// their object-storage settings from. The operator never reads it.
	S3SecretName string
	// JobBackoffLimit is the BackoffLimit of every Job.
	JobBackoffLimit int32
	// JobTTLSecondsAfterFinished is how long a finished Job and its pod are
	// kept, long enough for the caller to see the result and for debugging.
	JobTTLSecondsAfterFinished int32
}

// PdfRenderReconciler reconciles a PdfRender. Now, when set, replaces
// time.Now for status timestamps.
type PdfRenderReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Config Config
	Now    func() time.Time
}

// +kubebuilder:rbac:groups=renders.steward-grc.com,resources=pdfrenders,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=renders.steward-grc.com,resources=pdfrenders/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=renders.steward-grc.com,resources=pdfrenders/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile drives the PdfRender state machine:
//
//	"" (just created)  -> create Job, transition to Pending
//	Pending            -> if Job has an active pod, transition to Running
//	Running            -> if Job succeeded/failed, transition to Succeeded/Failed
//	Succeeded | Failed -> terminal, Job TTL GCs the pod
//
// The Job watch is the main trigger; RequeueAfter covers a missed event.
func (r *PdfRenderReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("pdfrender", req.NamespacedName)
	logger.V(1).Info("reconcile")

	var pdf rendersv1alpha1.PdfRender
	if err := r.Get(ctx, req.NamespacedName, &pdf); err != nil {
		if apierrors.IsNotFound(err) {
			// PdfRender deleted. Owner-reference cleanup deletes the Job.
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get pdfrender: %w", err)
	}

	switch pdf.Status.Phase {
	case "":
		return r.reconcileNew(ctx, &pdf, logger)
	case rendersv1alpha1.PdfRenderPhasePending:
		return r.reconcilePending(ctx, &pdf, logger)
	case rendersv1alpha1.PdfRenderPhaseRunning:
		return r.reconcileRunning(ctx, &pdf, logger)
	case rendersv1alpha1.PdfRenderPhaseSucceeded, rendersv1alpha1.PdfRenderPhaseFailed:
		// Terminal. Job's TTL handles pod GC.
		return ctrl.Result{}, nil
	default:
		// Defensive: an unknown phase should never appear because the type
		// system constrains it. If it does (manual edit, schema drift), log
		// loudly and stop reconciling rather than masking the bug.
		logger.Error(fmt.Errorf("unknown phase %q", pdf.Status.Phase), "refusing to reconcile")
		return ctrl.Result{}, nil
	}
}

func (r *PdfRenderReconciler) reconcileNew(ctx context.Context, pdf *rendersv1alpha1.PdfRender, logger logr.Logger) (ctrl.Result, error) {
	logger.Info("creating Job for new PdfRender")

	job := r.buildJob(pdf)
	if err := ctrl.SetControllerReference(pdf, job, r.Scheme); err != nil {
		return ctrl.Result{}, fmt.Errorf("set owner ref: %w", err)
	}
	if err := r.Create(ctx, job); err != nil {
		// AlreadyExists is benign on retry: a previous reconcile got the Job
		// in but failed to update status before crashing. Fall through to
		// move status forward.
		if !apierrors.IsAlreadyExists(err) {
			return ctrl.Result{}, fmt.Errorf("create job: %w", err)
		}
		logger.Info("job already exists; advancing status")
	}

	now := metav1.NewTime(r.now())
	pdf.Status.Phase = rendersv1alpha1.PdfRenderPhasePending
	pdf.Status.StartTime = &now
	setCondition(&pdf.Status, "Progressing", metav1.ConditionTrue, "JobCreated", "Job created; waiting for pod to start")
	if err := r.Status().Update(ctx, pdf); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status to Pending: %w", err)
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *PdfRenderReconciler) reconcilePending(ctx context.Context, pdf *rendersv1alpha1.PdfRender, logger logr.Logger) (ctrl.Result, error) {
	job, err := r.getOwnedJob(ctx, pdf)
	if err != nil {
		return ctrl.Result{}, err
	}
	if job == nil {
		// Job we created has vanished (manual delete or GC race). Re-create.
		logger.Info("owned Job missing in Pending phase; recreating")
		pdf.Status.Phase = ""
		if err := r.Status().Update(ctx, pdf); err != nil {
			return ctrl.Result{}, fmt.Errorf("reset status to empty: %w", err)
		}
		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	// Check terminal state first — a fast-failing Job can leapfrog Running.
	if isJobSucceeded(job) {
		logger.Info("job succeeded; phase Succeeded")
		return r.markSucceeded(ctx, pdf, job)
	}
	if isJobFailed(job, r.Config.JobBackoffLimit) {
		logger.Info("job failed; phase Failed", "failed", job.Status.Failed)
		return r.markFailed(ctx, pdf, job)
	}

	if job.Status.Active > 0 {
		logger.Info("job pod active; phase Running")
		pdf.Status.Phase = rendersv1alpha1.PdfRenderPhaseRunning
		setCondition(&pdf.Status, "Progressing", metav1.ConditionTrue, "JobRunning", "Job pod is active")
		if err := r.Status().Update(ctx, pdf); err != nil {
			return ctrl.Result{}, fmt.Errorf("update status to Running: %w", err)
		}
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}
	return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
}

func (r *PdfRenderReconciler) reconcileRunning(ctx context.Context, pdf *rendersv1alpha1.PdfRender, logger logr.Logger) (ctrl.Result, error) {
	job, err := r.getOwnedJob(ctx, pdf)
	if err != nil {
		return ctrl.Result{}, err
	}
	if job == nil {
		// Lost the Job mid-run. Treat as Failed — we cannot tell what
		// happened to the render and we should NOT silently re-create.
		logger.Info("owned Job missing in Running phase; marking Failed")
		pdf.Status.Phase = rendersv1alpha1.PdfRenderPhaseFailed
		pdf.Status.Error = "underlying Job disappeared mid-render"
		now := metav1.NewTime(r.now())
		pdf.Status.CompleteTime = &now
		setCondition(&pdf.Status, "Ready", metav1.ConditionFalse, "JobMissing", "owned Job vanished while running")
		if err := r.Status().Update(ctx, pdf); err != nil {
			return ctrl.Result{}, fmt.Errorf("update status to Failed (job missing): %w", err)
		}
		return ctrl.Result{}, nil
	}

	if isJobSucceeded(job) {
		logger.Info("job succeeded; phase Succeeded")
		return r.markSucceeded(ctx, pdf, job)
	}
	if isJobFailed(job, r.Config.JobBackoffLimit) {
		logger.Info("job failed; phase Failed", "failed", job.Status.Failed)
		return r.markFailed(ctx, pdf, job)
	}
	return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
}

func (r *PdfRenderReconciler) markSucceeded(ctx context.Context, pdf *rendersv1alpha1.PdfRender, _ *batchv1.Job) (ctrl.Result, error) {
	pdf.Status.Phase = rendersv1alpha1.PdfRenderPhaseSucceeded
	// OutputURL is the canonical s3:// URI built from spec. The renderer
	// itself uploads to exactly this location, so the operator can
	// confidently materialize the URL without inspecting Job logs.
	pdf.Status.OutputURL = fmt.Sprintf("s3://%s/%s", pdf.Spec.OutputBucket, pdf.Spec.OutputKey)
	now := metav1.NewTime(r.now())
	pdf.Status.CompleteTime = &now
	setCondition(&pdf.Status, "Ready", metav1.ConditionTrue, "PdfRendered", "PDF successfully rendered and uploaded")
	if err := r.Status().Update(ctx, pdf); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status to Succeeded: %w", err)
	}
	return ctrl.Result{}, nil
}

func (r *PdfRenderReconciler) markFailed(ctx context.Context, pdf *rendersv1alpha1.PdfRender, job *batchv1.Job) (ctrl.Result, error) {
	pdf.Status.Phase = rendersv1alpha1.PdfRenderPhaseFailed
	pdf.Status.Error = jobFailureReason(job)
	now := metav1.NewTime(r.now())
	pdf.Status.CompleteTime = &now
	setCondition(&pdf.Status, "Ready", metav1.ConditionFalse, "JobFailed", pdf.Status.Error)
	if err := r.Status().Update(ctx, pdf); err != nil {
		return ctrl.Result{}, fmt.Errorf("update status to Failed: %w", err)
	}
	return ctrl.Result{}, nil
}

// getOwnedJob looks up the Job we created for this PdfRender. The Job's name
// is deterministic (= PdfRender name) so we don't need to list-and-filter by
// owner reference on every reconcile — direct get is cheaper.
func (r *PdfRenderReconciler) getOwnedJob(ctx context.Context, pdf *rendersv1alpha1.PdfRender) (*batchv1.Job, error) {
	var job batchv1.Job
	key := types.NamespacedName{Namespace: pdf.Namespace, Name: pdf.Name}
	if err := r.Get(ctx, key, &job); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("get owned job: %w", err)
	}
	// Defensive: confirm the Job is actually owned by this PdfRender. A name
	// collision between an unrelated Job and our CRD is unlikely in practice
	// (the operator owns this namespace's pdfrender-named Jobs) but the
	// check is cheap and prevents accidental cross-resource interference.
	for _, ref := range job.OwnerReferences {
		if ref.UID == pdf.UID {
			return &job, nil
		}
	}
	return nil, nil
}

func (r *PdfRenderReconciler) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// buildJob constructs the batch/v1.Job spec from a PdfRender. Pure function;
// no API calls. The Job's name matches the PdfRender's name for easy lookup.
func (r *PdfRenderReconciler) buildJob(pdf *rendersv1alpha1.PdfRender) *batchv1.Job {
	backoff := r.Config.JobBackoffLimit
	ttl := r.Config.JobTTLSecondsAfterFinished
	return &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pdf.Name,
			Namespace: pdf.Namespace,
			Labels: map[string]string{
				"app.kubernetes.io/managed-by":      "steward-pdf-renderer",
				"app.kubernetes.io/name":            "steward-pdf-renderer",
				"renders.steward-grc.com/pdfrender": pdf.Name,
			},
		},
		Spec: batchv1.JobSpec{
			BackoffLimit:            &backoff,
			TTLSecondsAfterFinished: &ttl,
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app.kubernetes.io/name":            "steward-pdf-renderer",
						"renders.steward-grc.com/pdfrender": pdf.Name,
					},
				},
				Spec: corev1.PodSpec{
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "renderer",
							Image: r.Config.RendererImage,
							Env: []corev1.EnvVar{
								{Name: "PV_ID", Value: pdf.Spec.PolicyVersionID},
								{Name: "FETCH_URL", Value: pdf.Spec.FetchURL},
								{Name: "OUTPUT_BUCKET", Value: pdf.Spec.OutputBucket},
								{Name: "OUTPUT_KEY", Value: pdf.Spec.OutputKey},
								{Name: "SENSITIVITY", Value: pdf.Spec.Sensitivity},
								{Name: "REQUESTED_BY", Value: pdf.Spec.RequestedBy},
								{Name: "TRACE_ID", Value: pdf.Spec.Trace},
							},
							EnvFrom: []corev1.EnvFromSource{
								{
									SecretRef: &corev1.SecretEnvSource{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: r.Config.S3SecretName,
										},
										// The renderer reports missing settings itself.
										Optional: ptr.To(true),
									},
								},
							},
							Resources: corev1.ResourceRequirements{
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("250m"),
									corev1.ResourceMemory: resource.MustParse("256Mi"),
								},
							},
						},
					},
				},
			},
		},
	}
}

// SetupWithManager wires the reconciler into the controller-runtime manager.
// Watches PdfRender as primary, batch/v1.Job as secondary (owned). Pod events
// are not watched directly — Job status is sufficient and a single watch is
// cheaper than two.
func (r *PdfRenderReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.Now == nil {
		r.Now = time.Now
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&rendersv1alpha1.PdfRender{}).
		Owns(&batchv1.Job{}).
		Named("pdfrender").
		Complete(r)
}
