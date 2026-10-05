// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package render_test

import (
	"strings"
	"testing"
	"time"

	"github.com/Steward-GRC/steward-pdf-renderer/internal/render"
)

// TestWatermarkCSSContainsLabel mirrors the delivery package's coverage: the
// rendered CSS must carry the sensitivity label, recipient identity, and
// RFC3339 UTC timestamp verbatim so the audit trail visible on every page
// matches the audit row we persist to the database.
func TestWatermarkCSSContainsLabel(t *testing.T) {
	t.Parallel()

	params := render.WatermarkParams{
		SensitivityLabel:  "CONFIDENTIAL",
		RecipientIdentity: "erin@example.org",
		Timestamp:         time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
	}
	css := render.WatermarkCSS(params)
	for _, want := range []string{"CONFIDENTIAL", "erin@example.org", "2026-05-30T12:00:00Z"} {
		if !strings.Contains(css, want) {
			t.Errorf("watermark CSS missing %q\nGot:\n%s", want, css)
		}
	}
	for _, want := range []string{"<style>", "body::after", "position: fixed"} {
		if !strings.Contains(css, want) {
			t.Errorf("watermark CSS missing structural piece %q", want)
		}
	}
}

// TestWatermarkCSSTimestampIsUTC guards against a regression where a local
// timezone leaks into the rendered banner. The audit story requires UTC.
func TestWatermarkCSSTimestampIsUTC(t *testing.T) {
	t.Parallel()

	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	params := render.WatermarkParams{
		SensitivityLabel:  "CONFIDENTIAL",
		RecipientIdentity: "erin@example.org",
		Timestamp:         time.Date(2026, 5, 30, 8, 0, 0, 0, loc), // 12:00 UTC
	}
	css := render.WatermarkCSS(params)
	if !strings.Contains(css, "2026-05-30T12:00:00Z") {
		t.Errorf("expected UTC-formatted timestamp in CSS; got:\n%s", css)
	}
}
