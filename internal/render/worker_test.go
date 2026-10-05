// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package render_test

import (
	"bytes"
	"context"
	"os/exec"
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

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	css := render.WatermarkCSS(render.WatermarkParams{SensitivityLabel: "CONFIDENTIAL", RecipientIdentity: "erin", Timestamp: time.Now()})
	pdf, err := render.ChromedpRenderer().Render(ctx, "<h1>Desk Booking Policy</h1>", css)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Errorf("output isn't a PDF: %q", pdf[:min(len(pdf), 16)])
	}
}
