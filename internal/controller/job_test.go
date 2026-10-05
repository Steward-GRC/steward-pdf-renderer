// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package controller_test

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	rendersv1alpha1 "github.com/Steward-GRC/steward-pdf-renderer/api/v1alpha1"
)

func TestReconcileNewJobShape(t *testing.T) {
	t.Parallel()

	pdf := newPdfRender("render-7", "steward")
	pdf.Spec.Sensitivity = "sensitive"
	pdf.Spec.Trace = "trace-7"
	r, c := newTestReconciler(t, pdf)

	key := types.NamespacedName{Name: pdf.Name, Namespace: pdf.Namespace}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	var job batchv1.Job
	if err := c.Get(context.Background(), key, &job); err != nil {
		t.Fatalf("get job: %v", err)
	}

	for k, want := range map[string]string{
		"app.kubernetes.io/managed-by":      "steward-pdf-renderer",
		"app.kubernetes.io/name":            "steward-pdf-renderer",
		"renders.steward-grc.com/pdfrender": "render-7",
	} {
		if got := job.Labels[k]; got != want {
			t.Errorf("job label %s = %q, want %q", k, got, want)
		}
	}
	if got := job.Spec.Template.Labels["renders.steward-grc.com/pdfrender"]; got != "render-7" {
		t.Errorf("pod label = %q", got)
	}
	if job.Spec.BackoffLimit == nil || *job.Spec.BackoffLimit != 3 {
		t.Errorf("backoffLimit = %v, want 3", job.Spec.BackoffLimit)
	}
	if job.Spec.TTLSecondsAfterFinished == nil || *job.Spec.TTLSecondsAfterFinished != 600 {
		t.Errorf("ttl = %v, want 600", job.Spec.TTLSecondsAfterFinished)
	}
	pod := job.Spec.Template.Spec
	if pod.RestartPolicy != corev1.RestartPolicyNever {
		t.Errorf("restartPolicy = %q, want Never", pod.RestartPolicy)
	}
	ctr := pod.Containers[0]
	if len(ctr.EnvFrom) != 1 || ctr.EnvFrom[0].SecretRef == nil || ctr.EnvFrom[0].SecretRef.Name != "steward-pdf-renderer-s3" {
		t.Fatalf("envFrom = %+v, want the S3 secret", ctr.EnvFrom)
	}
	if o := ctr.EnvFrom[0].SecretRef.Optional; o == nil || !*o {
		t.Errorf("S3 secret should be optional")
	}
	env := map[string]string{}
	for _, e := range ctr.Env {
		env[e.Name] = e.Value
	}
	if env["SENSITIVITY"] != "sensitive" || env["TRACE_ID"] != "trace-7" || env["REQUESTED_BY"] != "user-1" {
		t.Errorf("env = %v", env)
	}
}

func TestReconcileConditions(t *testing.T) {
	t.Parallel()

	pdf := newPdfRender("render-8", "steward")
	r, c := newTestReconciler(t, pdf)
	key := types.NamespacedName{Name: pdf.Name, Namespace: pdf.Namespace}
	if _, err := r.Reconcile(context.Background(), ctrl.Request{NamespacedName: key}); err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	var got rendersv1alpha1.PdfRender
	if err := c.Get(context.Background(), key, &got); err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got.Status.Conditions) != 1 {
		t.Fatalf("conditions = %+v", got.Status.Conditions)
	}
	cond := got.Status.Conditions[0]
	if cond.Type != "Progressing" || cond.Status != metav1.ConditionTrue || cond.Reason != "JobCreated" || cond.LastTransitionTime.IsZero() {
		t.Errorf("condition = %+v", cond)
	}
}
