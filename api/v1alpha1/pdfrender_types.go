// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// PdfRenderSpec is the desired state of one PDF render.
//
// The operator builds the Job from the spec when the resource is first
// reconciled; later edits don't reach a Job that already exists. Deleting the
// resource cancels the render: the Job goes with it through its owner
// reference.
type PdfRenderSpec struct {
	// PolicyVersionID identifies the document version being rendered. It is
	// recorded for correlation only; the renderer fetches FetchURL.
	// +kubebuilder:validation:Required
	PolicyVersionID string `json:"policyVersionId"`

	// FetchURL is the URL the renderer GETs for the HTML to print. It must be
	// reachable from the Job's pod.
	// +kubebuilder:validation:Required
	FetchURL string `json:"fetchURL"`

	// OutputBucket is the object-storage bucket the PDF is uploaded to. The
	// operator doesn't check that it exists; a failed upload fails the Job.
	// +kubebuilder:validation:Required
	OutputBucket string `json:"outputBucket"`

	// OutputKey is the object key for the PDF. The caller picks it and keeps
	// it unique.
	// +kubebuilder:validation:Required
	OutputKey string `json:"outputKey"`

	// Sensitivity selects the watermark: "standard" has none, "sensitive"
	// draws the requester and the time diagonally on every page.
	// +kubebuilder:validation:Enum=standard;sensitive
	// +kubebuilder:default=standard
	Sensitivity string `json:"sensitivity,omitempty"`

	// RequestedBy is the requester's user id, printed in the sensitive
	// watermark. Informational only: the operator authenticates nothing
	// against it.
	RequestedBy string `json:"requestedBy,omitempty"`

	// Trace is the caller's trace id, passed to the renderer so its logs
	// correlate with the request that asked for the PDF.
	Trace string `json:"trace,omitempty"`
}

// PdfRenderPhase is the state of a render.
// +kubebuilder:validation:Enum=Pending;Running;Succeeded;Failed
type PdfRenderPhase string

const (
	// PdfRenderPhasePending means the Job exists but its pod isn't active yet.
	PdfRenderPhasePending PdfRenderPhase = "Pending"
	// PdfRenderPhaseRunning means the Job's pod is rendering.
	PdfRenderPhaseRunning PdfRenderPhase = "Running"
	// PdfRenderPhaseSucceeded means the PDF was rendered and uploaded.
	PdfRenderPhaseSucceeded PdfRenderPhase = "Succeeded"
	// PdfRenderPhaseFailed means the Job used up its retries without a PDF.
	PdfRenderPhaseFailed PdfRenderPhase = "Failed"
)

// PdfRenderStatus is the observed state of a PdfRender. Only the operator
// writes it.
type PdfRenderStatus struct {
	// Phase moves Pending, Running, then Succeeded or Failed. Empty means the
	// operator hasn't reconciled the resource yet.
	Phase PdfRenderPhase `json:"phase,omitempty"`

	// OutputURL is the s3:// URI of the PDF once Phase is Succeeded.
	OutputURL string `json:"outputURL,omitempty"`

	// Error is a short reason when Phase is Failed. The detail is in the Job
	// pod's logs, kept until the Job's TTL runs out.
	Error string `json:"error,omitempty"`

	// StartTime is when the operator created the Job.
	StartTime *metav1.Time `json:"startTime,omitempty"`

	// CompleteTime is when the render reached Succeeded or Failed.
	CompleteTime *metav1.Time `json:"completeTime,omitempty"`

	// Conditions are the standard Progressing and Ready conditions.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// PdfRender asks for one document version to be rendered to PDF.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="PolicyVersion",type=string,JSONPath=`.spec.policyVersionId`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`
type PdfRender struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              PdfRenderSpec   `json:"spec,omitempty"`
	Status            PdfRenderStatus `json:"status,omitempty"`
}

// PdfRenderList is a list of PdfRender.
// +kubebuilder:object:root=true
type PdfRenderList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PdfRender `json:"items"`
}
