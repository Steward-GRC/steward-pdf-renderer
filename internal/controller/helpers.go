// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	rendersv1alpha1 "github.com/Steward-GRC/steward-pdf-renderer/api/v1alpha1"
)

// isJobSucceeded returns true when a Job has at least one successful pod.
// JobComplete condition is the canonical signal but Status.Succeeded is set
// in the same write, so checking Succeeded directly is equivalent and avoids
// iterating conditions.
func isJobSucceeded(job *batchv1.Job) bool {
	if job == nil {
		return false
	}
	if job.Status.Succeeded > 0 {
		return true
	}
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// isJobFailed returns true when a Job has exhausted its backoff. We check
// both the JobFailed condition (set by the controller when backoff is hit)
// and Status.Failed >= backoffLimit (the underlying mechanism), so a Job
// that's failing-but-not-yet-reconciled-by-k8s is still caught.
func isJobFailed(job *batchv1.Job, backoffLimit int32) bool {
	if job == nil {
		return false
	}
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	// Defensive: if Status.Failed exceeds the limit (race: controller hasn't
	// set the condition yet), treat it as failed so we don't sit in Running.
	if backoffLimit >= 0 && job.Status.Failed > backoffLimit {
		return true
	}
	return false
}

// jobFailureReason extracts a brief human-readable failure description from
// the Job's terminal condition. Falls back to a generic message if no
// condition carries one — which can happen if the Job was force-deleted.
func jobFailureReason(job *batchv1.Job) string {
	if job == nil {
		return "unknown Job state"
	}
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			if c.Message != "" {
				return c.Message
			}
			if c.Reason != "" {
				return c.Reason
			}
		}
	}
	return fmt.Sprintf("Job exhausted backoffLimit (failed=%d)", job.Status.Failed)
}

// setCondition upserts a condition, keeping LastTransitionTime when its
// status doesn't change.
func setCondition(status *rendersv1alpha1.PdfRenderStatus, condType string, condStatus metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    condType,
		Status:  condStatus,
		Reason:  reason,
		Message: message,
	})
}
