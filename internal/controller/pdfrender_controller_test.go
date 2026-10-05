// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package controller_test

import (
	"context"
	"strings"
	"testing"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	rendersv1alpha1 "github.com/Steward-GRC/steward-pdf-renderer/api/v1alpha1"
	"github.com/Steward-GRC/steward-pdf-renderer/internal/controller"
)

// newTestReconciler builds a Reconciler backed by the controller-runtime
// fake client with our scheme + the supplied initial objects. A deterministic
// Now hook keeps status timestamps comparable in assertions.
func newTestReconciler(t *testing.T, objs ...client.Object) (*controller.PdfRenderReconciler, client.Client) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("add core scheme: %v", err)
	}
	if err := rendersv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("add renders scheme: %v", err)
	}

	c := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objs...).
		WithStatusSubresource(&rendersv1alpha1.PdfRender{}).
		Build()

	r := &controller.PdfRenderReconciler{
		Client: c,
		Scheme: scheme,
		Config: controller.Config{
			RendererImage:              "registry.example.org/steward-pdf-renderer/renderer:test",
			S3SecretName:               "steward-pdf-renderer-s3",
			JobBackoffLimit:            3,
			JobTTLSecondsAfterFinished: 600,
		},
		Now: func() time.Time {
			return time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
		},
	}
	return r, c
}

func newPdfRender(name, namespace string) *rendersv1alpha1.PdfRender {
	return &rendersv1alpha1.PdfRender{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			UID:       types.UID("uid-" + name),
		},
		Spec: rendersv1alpha1.PdfRenderSpec{
			PolicyVersionID: "pv-42",
			FetchURL:        "http://delivery.example.org/versions/pv-42/html",
			OutputBucket:    "steward-artifacts",
			OutputKey:       "artifacts/pv-42/" + name + ".pdf",
			Sensitivity:     "standard",
			RequestedBy:     "user-1",
		},
	}
}

// TestReconcileNewCreatesJobAndPending is the load-bearing happy-path test:
// a freshly-created PdfRender (empty phase) becomes Pending and a Job with
// the expected env + owner-ref appears.
func TestReconcileNewCreatesJobAndPending(t *testing.T) {
	t.Parallel()

	pdf := newPdfRender("render-1", "steward")
	r, c := newTestReconciler(t, pdf)

	res, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: pdf.Name, Namespace: pdf.Namespace},
	})
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	if res.RequeueAfter == 0 {
		t.Errorf("expected RequeueAfter > 0 after creating Job; got %+v", res)
	}

	var got rendersv1alpha1.PdfRender
	if err := c.Get(context.Background(), types.NamespacedName{Name: pdf.Name, Namespace: pdf.Namespace}, &got); err != nil {
		t.Fatalf("get pdfrender: %v", err)
	}
	if got.Status.Phase != rendersv1alpha1.PdfRenderPhasePending {
		t.Errorf("phase = %q, want Pending", got.Status.Phase)
	}
	if got.Status.StartTime == nil {
		t.Errorf("StartTime not set")
	}

	var job batchv1.Job
	if err := c.Get(context.Background(), types.NamespacedName{Name: pdf.Name, Namespace: pdf.Namespace}, &job); err != nil {
		t.Fatalf("get job: %v", err)
	}
	// OwnerReference + name = pdf name = how reconcilePending finds it.
	if len(job.OwnerReferences) != 1 || job.OwnerReferences[0].UID != pdf.UID {
		t.Errorf("job missing owner ref to pdf: %+v", job.OwnerReferences)
	}
	if got, want := job.Spec.Template.Spec.Containers[0].Image, "registry.example.org/steward-pdf-renderer/renderer:test"; got != want {
		t.Errorf("renderer image = %q, want %q", got, want)
	}
	envByName := map[string]string{}
	for _, e := range job.Spec.Template.Spec.Containers[0].Env {
		envByName[e.Name] = e.Value
	}
	for _, kv := range [][2]string{
		{"PV_ID", "pv-42"},
		{"FETCH_URL", pdf.Spec.FetchURL},
		{"OUTPUT_BUCKET", "steward-artifacts"},
		{"OUTPUT_KEY", pdf.Spec.OutputKey},
		{"SENSITIVITY", "standard"},
	} {
		if got := envByName[kv[0]]; got != kv[1] {
			t.Errorf("env %s = %q, want %q", kv[0], got, kv[1])
		}
	}
}

// TestReconcilePendingTransitionsToRunning covers Pending → Running when the
// underlying Job has an active pod.
func TestReconcilePendingTransitionsToRunning(t *testing.T) {
	t.Parallel()

	pdf := newPdfRender("render-2", "steward")
	pdf.Status.Phase = rendersv1alpha1.PdfRenderPhasePending
	now := metav1.NewTime(time.Date(2026, 6, 1, 11, 59, 0, 0, time.UTC))
	pdf.Status.StartTime = &now

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pdf.Name,
			Namespace: pdf.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{APIVersion: rendersv1alpha1.GroupVersion.String(), Kind: "PdfRender", Name: pdf.Name, UID: pdf.UID},
			},
		},
		Status: batchv1.JobStatus{Active: 1},
	}

	r, c := newTestReconciler(t, pdf, job)

	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: pdf.Name, Namespace: pdf.Namespace},
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var got rendersv1alpha1.PdfRender
	if err := c.Get(context.Background(), types.NamespacedName{Name: pdf.Name, Namespace: pdf.Namespace}, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status.Phase != rendersv1alpha1.PdfRenderPhaseRunning {
		t.Errorf("phase = %q, want Running", got.Status.Phase)
	}
}

// TestReconcileRunningSucceeded covers Running → Succeeded with the canonical
// s3:// URL synthesis.
func TestReconcileRunningSucceeded(t *testing.T) {
	t.Parallel()

	pdf := newPdfRender("render-3", "steward")
	pdf.Status.Phase = rendersv1alpha1.PdfRenderPhaseRunning
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pdf.Name,
			Namespace: pdf.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{APIVersion: rendersv1alpha1.GroupVersion.String(), Kind: "PdfRender", Name: pdf.Name, UID: pdf.UID},
			},
		},
		Status: batchv1.JobStatus{
			Succeeded:  1,
			Conditions: []batchv1.JobCondition{{Type: batchv1.JobComplete, Status: corev1.ConditionTrue}},
		},
	}

	r, c := newTestReconciler(t, pdf, job)
	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: pdf.Name, Namespace: pdf.Namespace},
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var got rendersv1alpha1.PdfRender
	if err := c.Get(context.Background(), types.NamespacedName{Name: pdf.Name, Namespace: pdf.Namespace}, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status.Phase != rendersv1alpha1.PdfRenderPhaseSucceeded {
		t.Errorf("phase = %q, want Succeeded", got.Status.Phase)
	}
	want := "s3://steward-artifacts/" + pdf.Spec.OutputKey
	if got.Status.OutputURL != want {
		t.Errorf("OutputURL = %q, want %q", got.Status.OutputURL, want)
	}
	if got.Status.CompleteTime == nil {
		t.Errorf("CompleteTime not set on Succeeded")
	}
}

// TestReconcileRunningFailedExhaustsBackoff covers Running → Failed when the
// underlying Job exhausts backoffLimit. This exercises the "Job failed"
// condition path and asserts the error message is surfaced.
func TestReconcileRunningFailedExhaustsBackoff(t *testing.T) {
	t.Parallel()

	pdf := newPdfRender("render-4", "steward")
	pdf.Status.Phase = rendersv1alpha1.PdfRenderPhaseRunning

	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pdf.Name,
			Namespace: pdf.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{APIVersion: rendersv1alpha1.GroupVersion.String(), Kind: "PdfRender", Name: pdf.Name, UID: pdf.UID},
			},
		},
		Status: batchv1.JobStatus{
			Failed: 3,
			Conditions: []batchv1.JobCondition{{
				Type:    batchv1.JobFailed,
				Status:  corev1.ConditionTrue,
				Reason:  "BackoffLimitExceeded",
				Message: "Job has reached the specified backoff limit",
			}},
		},
	}

	r, c := newTestReconciler(t, pdf, job)
	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: pdf.Name, Namespace: pdf.Namespace},
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var got rendersv1alpha1.PdfRender
	if err := c.Get(context.Background(), types.NamespacedName{Name: pdf.Name, Namespace: pdf.Namespace}, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status.Phase != rendersv1alpha1.PdfRenderPhaseFailed {
		t.Errorf("phase = %q, want Failed", got.Status.Phase)
	}
	if !strings.Contains(got.Status.Error, "backoff") {
		t.Errorf("Error = %q, want it to mention backoff", got.Status.Error)
	}
	if got.Status.CompleteTime == nil {
		t.Errorf("CompleteTime not set on Failed")
	}
}

// TestReconcileTerminalIsNoop confirms Succeeded/Failed states don't try to
// re-create Jobs or otherwise churn the cluster.
func TestReconcileTerminalIsNoop(t *testing.T) {
	t.Parallel()

	for _, phase := range []rendersv1alpha1.PdfRenderPhase{
		rendersv1alpha1.PdfRenderPhaseSucceeded,
		rendersv1alpha1.PdfRenderPhaseFailed,
	} {
		phase := phase
		t.Run(string(phase), func(t *testing.T) {
			t.Parallel()
			pdf := newPdfRender("render-terminal-"+strings.ToLower(string(phase)), "steward")
			pdf.Status.Phase = phase
			r, c := newTestReconciler(t, pdf)

			res, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Name: pdf.Name, Namespace: pdf.Namespace},
			})
			if err != nil {
				t.Fatalf("Reconcile: %v", err)
			}
			if res.RequeueAfter != 0 {
				t.Errorf("terminal phase should not requeue; got %+v", res)
			}
			var jobs batchv1.JobList
			if err := c.List(context.Background(), &jobs); err != nil {
				t.Fatalf("list jobs: %v", err)
			}
			if len(jobs.Items) != 0 {
				t.Errorf("terminal reconcile created %d Jobs; want 0", len(jobs.Items))
			}
		})
	}
}

// TestReconcileNotFound covers the deleted-CRD path: the reconciler should
// silently no-op rather than returning an error.
func TestReconcileNotFound(t *testing.T) {
	t.Parallel()

	r, _ := newTestReconciler(t)
	res, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: "ghost", Namespace: "steward"},
	})
	if err != nil {
		t.Errorf("Reconcile of deleted PdfRender returned error: %v", err)
	}
	if res.RequeueAfter != 0 {
		t.Errorf("Reconcile of deleted PdfRender requeued: %+v", res)
	}
}

// TestReconcilePendingJobMissingResetsPhase covers the GC race: if the Job
// vanishes while we're in Pending, the reconciler resets phase so the next
// pass recreates the Job.
func TestReconcilePendingJobMissingResetsPhase(t *testing.T) {
	t.Parallel()

	pdf := newPdfRender("render-5", "steward")
	pdf.Status.Phase = rendersv1alpha1.PdfRenderPhasePending
	r, c := newTestReconciler(t, pdf)

	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: pdf.Name, Namespace: pdf.Namespace},
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var got rendersv1alpha1.PdfRender
	if err := c.Get(context.Background(), types.NamespacedName{Name: pdf.Name, Namespace: pdf.Namespace}, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status.Phase != "" {
		t.Errorf("phase = %q, want empty (reset for recreate)", got.Status.Phase)
	}
}

// TestReconcileRunningJobMissingMarksFailed covers the safety check:
// disappearing Jobs in Running phase must NOT silently re-create, because we
// can't tell what happened to the in-progress render.
func TestReconcileRunningJobMissingMarksFailed(t *testing.T) {
	t.Parallel()

	pdf := newPdfRender("render-6", "steward")
	pdf.Status.Phase = rendersv1alpha1.PdfRenderPhaseRunning
	r, c := newTestReconciler(t, pdf)

	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: pdf.Name, Namespace: pdf.Namespace},
	}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}

	var got rendersv1alpha1.PdfRender
	if err := c.Get(context.Background(), types.NamespacedName{Name: pdf.Name, Namespace: pdf.Namespace}, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Status.Phase != rendersv1alpha1.PdfRenderPhaseFailed {
		t.Errorf("phase = %q, want Failed", got.Status.Phase)
	}
	if got.Status.Error == "" {
		t.Errorf("expected non-empty error on Failed due to missing Job")
	}
}
