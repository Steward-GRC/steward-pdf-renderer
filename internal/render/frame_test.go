// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package render_test

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Steward-GRC/steward-pdf-renderer/internal/render"
)

// frame is the sample from the stored print design (R1).
func frame() render.Frame {
	return render.Frame{
		Organisation: "Example Organisation",
		Number:       "POL-FACILITIES-000001",
		Version:      "v1.0.0",
		Status:       "Published",
		Owner:        "Dave",
		PrintedAt:    time.Date(2026, 10, 3, 9, 30, 0, 0, time.UTC),
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.005 }

// TestFramePrintMargins pins R1: 20 mm top and bottom, 18 mm left and right.
func TestFramePrintMargins(t *testing.T) {
	t.Parallel()
	p := frame().PrintParams()
	const mm = 1 / 25.4
	for name, got := range map[string]float64{"top": p.MarginTop, "bottom": p.MarginBottom} {
		if !near(got, 20*mm) {
			t.Errorf("%s margin = %.3f in, want 20 mm", name, got)
		}
	}
	for name, got := range map[string]float64{"left": p.MarginLeft, "right": p.MarginRight} {
		if !near(got, 18*mm) {
			t.Errorf("%s margin = %.3f in, want 18 mm", name, got)
		}
	}
	if !p.DisplayHeaderFooter {
		t.Error("header and footer are not displayed")
	}
	if !p.PrintBackground {
		t.Error("backgrounds are not printed, so the watermark would vanish")
	}
}

// TestFramePaperSizes covers A4 (the default) and Letter.
func TestFramePaperSizes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		paper render.PaperSize
		w, h  float64
	}{
		{"", 210 / 25.4, 297 / 25.4},
		{render.PaperA4, 210 / 25.4, 297 / 25.4},
		{render.PaperLetter, 8.5, 11},
	}
	for _, c := range cases {
		f := frame()
		f.Paper = c.paper
		p := f.PrintParams()
		if !near(p.PaperWidth, c.w) || !near(p.PaperHeight, c.h) {
			t.Errorf("paper %q = %.2f x %.2f in, want %.2f x %.2f", c.paper, p.PaperWidth, p.PaperHeight, c.w, c.h)
		}
	}
	if !render.PaperLetter.Valid() || !render.PaperA4.Valid() || render.PaperSize("A3").Valid() {
		t.Error("PaperSize.Valid accepts the wrong sizes")
	}
}

// TestFrameHeader pins the header: organisation on the left, then
// "number · version · status" on the right.
func TestFrameHeader(t *testing.T) {
	t.Parallel()
	h := frame().PrintParams().HeaderTemplate
	for _, want := range []string{"Example Organisation", "POL-FACILITIES-000001 · v1.0.0 · Published", "EO"} {
		if !strings.Contains(h, want) {
			t.Errorf("header missing %q\n%s", want, h)
		}
	}
	if strings.Contains(h, "pageNumber") {
		t.Error("page numbers belong in the footer")
	}
}

// TestFrameFooter pins the footer: "owner · printed date" on the left and
// "Steward · Page n of N" on the right, with Chromium's page fields.
func TestFrameFooter(t *testing.T) {
	t.Parallel()
	ft := frame().PrintParams().FooterTemplate
	for _, want := range []string{
		"Dave · Printed 3 Oct 2026 09:30",
		`Steward · Page <span class="pageNumber"></span> of <span class="totalPages"></span>`,
	} {
		if !strings.Contains(ft, want) {
			t.Errorf("footer missing %q\n%s", want, ft)
		}
	}
}

// TestFrameTemplatesEscape keeps document fields from breaking the templates.
func TestFrameTemplatesEscape(t *testing.T) {
	t.Parallel()
	f := frame()
	f.Organisation = `<script>alert(1)</script> & Co`
	f.Owner = `"><img src=x>`
	p := f.PrintParams()
	if strings.Contains(p.HeaderTemplate, "<script>") || strings.Contains(p.FooterTemplate, "<img") {
		t.Errorf("fields are not escaped\n%s\n%s", p.HeaderTemplate, p.FooterTemplate)
	}
	if !strings.Contains(p.HeaderTemplate, "&amp; Co") {
		t.Errorf("header lost the escaped text\n%s", p.HeaderTemplate)
	}
}

// TestFrameEmptyFields leaves out what is not set instead of printing stray
// separators.
func TestFrameEmptyFields(t *testing.T) {
	t.Parallel()
	p := render.Frame{PrintedAt: time.Date(2026, 10, 3, 9, 30, 0, 0, time.UTC)}.PrintParams()
	if strings.Contains(p.HeaderTemplate, "·") {
		t.Errorf("header has a stray separator\n%s", p.HeaderTemplate)
	}
	if !strings.Contains(p.FooterTemplate, "Printed 3 Oct 2026 09:30") || strings.Contains(p.FooterTemplate, " · Printed") {
		t.Errorf("footer left side wrong\n%s", p.FooterTemplate)
	}
	if !strings.Contains(p.FooterTemplate, "totalPages") {
		t.Error("footer has no page count")
	}
}
