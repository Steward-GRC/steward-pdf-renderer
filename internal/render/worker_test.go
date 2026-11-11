// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package render_test

import (
	"bytes"
	"context"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Steward-GRC/steward-pdf-renderer/internal/render"
)

// TestChromedpRendersPDF drives a real headless browser. It is skipped where
// none is installed.
func TestChromedpRendersPDF(t *testing.T) {
	found := false
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if _, err := exec.LookPath(name); err == nil {
			found = true
			break
		}
	}
	if !found {
		t.Skip("no Chromium or Chrome on PATH")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*render.DefaultStartTimeout)
	defer cancel()
	css := render.WatermarkCSS(render.WatermarkParams{SensitivityLabel: "SENSITIVE", RecipientIdentity: "erin", Timestamp: time.Now()})
	pdf, err := render.ChromedpRenderer().Render(ctx, "<h1>Desk Booking Policy</h1>", css)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Errorf("output isn't a PDF: %q", pdf[:min(len(pdf), 16)])
	}
}

// browserPath finds a Chromium or Chrome: CHROME_BIN, else the first on PATH.
func browserPath() string {
	if p := os.Getenv("CHROME_BIN"); p != "" {
		return p
	}
	for _, name := range []string{"chromium", "chromium-browser", "google-chrome", "google-chrome-stable"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// TestChromedpRendersPrintFrame renders a multi-page sample at A4 and Letter
// and compares the PDF with the frame: the page size and a multi-page body.
// The header and footer text is checked in the templates (frame_test.go) and by
// eye on a rendered sample; the PDF text is compressed. Skipped where no
// browser is installed.
func TestChromedpRendersPrintFrame(t *testing.T) {
	bin := browserPath()
	if bin == "" {
		t.Skip("no Chromium or Chrome on PATH and CHROME_BIN is unset")
	}
	restore := render.SetBrowserForTest(bin, 0)
	t.Cleanup(restore)

	body := strings.Repeat("<p>Book a shared desk through the booking page within the booking window.</p>", 160)
	css := render.WatermarkCSS(render.WatermarkParams{SensitivityLabel: "SENSITIVE", RecipientIdentity: "erin", Timestamp: time.Now()})
	boxRE := regexp.MustCompile(`/MediaBox\s*\[\s*0\s+0\s+([0-9.]+)\s+([0-9.]+)\s*\]`)

	for _, c := range []struct {
		paper render.PaperSize
		w, h  float64 // points
	}{
		{render.PaperA4, 595, 842},
		{render.PaperLetter, 612, 792},
	} {
		t.Run(string(c.paper), func(t *testing.T) {
			f := frame()
			f.Paper = c.paper
			ctx, cancel := context.WithTimeout(context.Background(), 2*render.DefaultStartTimeout)
			defer cancel()
			pdf, err := render.NewChromedpRenderer(render.Options{Frame: &f}).Render(ctx, body, css)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			m := boxRE.FindSubmatch(pdf)
			if m == nil {
				t.Fatal("no MediaBox in the PDF")
			}
			w, _ := strconv.ParseFloat(string(m[1]), 64)
			h, _ := strconv.ParseFloat(string(m[2]), 64)
			if math.Abs(w-c.w) > 1 || math.Abs(h-c.h) > 1 {
				t.Errorf("page = %.1f x %.1f pt, want about %.0f x %.0f", w, h, c.w, c.h)
			}
			if n := len(regexp.MustCompile(`/Type\s*/Page[^s]`).FindAll(pdf, -1)); n < 2 {
				t.Errorf("pages = %d, want a multi-page sample", n)
			}
		})
	}
}
