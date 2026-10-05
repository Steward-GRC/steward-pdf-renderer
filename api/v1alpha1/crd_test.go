// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1_test

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"

	rendersv1alpha1 "github.com/Steward-GRC/steward-pdf-renderer/api/v1alpha1"
)

const crdPath = "../../deployments/crd/renders.steward-grc.com_pdfrenders.yaml"

func TestGroupVersion(t *testing.T) {
	t.Parallel()
	if got, want := rendersv1alpha1.GroupVersion.String(), "renders.steward-grc.com/v1alpha1"; got != want {
		t.Errorf("GroupVersion = %q, want %q", got, want)
	}
}

func loadCRD(t *testing.T) *apiextensionsv1.CustomResourceDefinition {
	t.Helper()
	raw, err := os.ReadFile(crdPath)
	if err != nil {
		t.Fatalf("read CRD: %v", err)
	}
	var crd apiextensionsv1.CustomResourceDefinition
	if err := yaml.UnmarshalStrict(raw, &crd); err != nil {
		t.Fatalf("parse CRD: %v", err)
	}
	return &crd
}

// TestCRDContract pins the names and fields steward-delivery creates and
// reads through its dynamic client.
func TestCRDContract(t *testing.T) {
	t.Parallel()
	crd := loadCRD(t)

	if crd.Name != "pdfrenders.renders.steward-grc.com" {
		t.Errorf("name = %q", crd.Name)
	}
	if crd.Spec.Group != "renders.steward-grc.com" {
		t.Errorf("group = %q", crd.Spec.Group)
	}
	if crd.Spec.Names.Kind != "PdfRender" || crd.Spec.Names.Plural != "pdfrenders" || crd.Spec.Names.ListKind != "PdfRenderList" {
		t.Errorf("names = %+v", crd.Spec.Names)
	}
	if crd.Spec.Scope != apiextensionsv1.NamespaceScoped {
		t.Errorf("scope = %q, want Namespaced", crd.Spec.Scope)
	}
	if len(crd.Spec.Versions) != 1 {
		t.Fatalf("versions = %d, want 1", len(crd.Spec.Versions))
	}
	v := crd.Spec.Versions[0]
	if v.Name != "v1alpha1" || !v.Served || !v.Storage {
		t.Errorf("version = %q served=%v storage=%v", v.Name, v.Served, v.Storage)
	}
	if v.Subresources == nil || v.Subresources.Status == nil {
		t.Errorf("status subresource missing")
	}

	props := v.Schema.OpenAPIV3Schema.Properties
	spec := props["spec"]
	for _, f := range []string{"policyVersionId", "fetchURL", "outputBucket", "outputKey", "sensitivity", "requestedBy", "trace"} {
		if _, ok := spec.Properties[f]; !ok {
			t.Errorf("spec.%s missing", f)
		}
	}
	required := slices.Clone(spec.Required)
	slices.Sort(required)
	if want := []string{"fetchURL", "outputBucket", "outputKey", "policyVersionId"}; !slices.Equal(required, want) {
		t.Errorf("spec required = %v, want %v", required, want)
	}
	var enum []string
	for _, e := range spec.Properties["sensitivity"].Enum {
		var s string
		if err := json.Unmarshal(e.Raw, &s); err != nil {
			t.Fatalf("sensitivity enum: %v", err)
		}
		enum = append(enum, s)
	}
	if want := []string{"standard", "sensitive"}; !slices.Equal(enum, want) {
		t.Errorf("sensitivity enum = %v, want %v", enum, want)
	}
	if d := spec.Properties["sensitivity"].Default; d == nil || string(d.Raw) != `"standard"` {
		t.Errorf("sensitivity default = %v, want standard", d)
	}

	status := props["status"]
	for _, f := range []string{"phase", "outputURL", "error", "startTime", "completeTime", "conditions"} {
		if _, ok := status.Properties[f]; !ok {
			t.Errorf("status.%s missing", f)
		}
	}
	var phases []string
	for _, e := range status.Properties["phase"].Enum {
		var s string
		_ = json.Unmarshal(e.Raw, &s)
		phases = append(phases, s)
	}
	if want := []string{"Pending", "Running", "Succeeded", "Failed"}; !slices.Equal(phases, want) {
		t.Errorf("phase enum = %v, want %v", phases, want)
	}
}

func TestDeepCopyIsIndependent(t *testing.T) {
	t.Parallel()
	start := metav1.NewTime(time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC))
	in := &rendersv1alpha1.PdfRender{
		Spec: rendersv1alpha1.PdfRenderSpec{PolicyVersionID: "pv-42", OutputKey: "a.pdf"},
		Status: rendersv1alpha1.PdfRenderStatus{
			Phase:      rendersv1alpha1.PdfRenderPhaseRunning,
			StartTime:  &start,
			Conditions: []metav1.Condition{{Type: "Progressing", Status: metav1.ConditionTrue}},
		},
	}
	out := in.DeepCopy()
	out.Spec.OutputKey = "b.pdf"
	out.Status.Conditions[0].Status = metav1.ConditionFalse
	out.Status.StartTime.Time = start.Add(time.Hour)

	if in.Spec.OutputKey != "a.pdf" || in.Status.Conditions[0].Status != metav1.ConditionTrue || !in.Status.StartTime.Equal(&start) {
		t.Errorf("DeepCopy shares state with the original: %+v", in)
	}
}

func TestAddToSchemeRegistersKinds(t *testing.T) {
	t.Parallel()
	s := runtime.NewScheme()
	if err := rendersv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	for _, kind := range []string{"PdfRender", "PdfRenderList"} {
		if !s.Recognizes(rendersv1alpha1.GroupVersion.WithKind(kind)) {
			t.Errorf("scheme doesn't recognise %s", kind)
		}
	}
}
