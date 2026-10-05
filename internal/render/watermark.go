// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Package render turns HTML into a PDF with headless Chromium and draws the
// sensitive-document watermark.
package render

import (
	"fmt"
	"time"
)

// WatermarkParams carries the data injected into the watermark overlay.
//
// Every sensitive PDF export must show, on every page, who pulled it and
// when. The watermark is the visible audit trail; the same fields are
// recorded in the audit log by the caller.
type WatermarkParams struct {
	// SensitivityLabel is the policy's sensitivity tier (e.g. "CONFIDENTIAL").
	// Rendered verbatim; callers normalize casing before passing it in.
	SensitivityLabel string
	// RecipientIdentity is the requester's user ID or email. Recipient
	// attribution is the deterrent against unauthorized re-sharing.
	RecipientIdentity string
	// Timestamp is when the export was requested. Always rendered as RFC3339
	// UTC so the banner is unambiguous across time zones and matches the
	// audit log.
	Timestamp time.Time
}

// WatermarkCSS returns a <style> block that, when injected into the rendered
// HTML before PDF capture, draws a fixed-position diagonal watermark on every
// page. The text reads:
//
//	"<SensitivityLabel> | <RecipientIdentity> | <Timestamp RFC3339>"
//
// Design choices:
//
//   - CSS-only (no SVG, no post-process). chromedp prints exactly what
//     Chromium renders, so a pure-CSS overlay piggybacks on the same render
//     pass and works for any number of pages without per-page assembly.
//   - body::after with position:fixed reliably repeats across paginated
//     output in Chromium's print pipeline.
//   - rgba(180,0,0,0.18) is light enough to remain readable behind the policy
//     text but unmistakable in a screenshot or photograph.
//   - %q on the content string escapes embedded quotes / backslashes so we
//     cannot break out of the CSS string even on hostile input.
func WatermarkCSS(p WatermarkParams) string {
	text := fmt.Sprintf("%s | %s | %s",
		p.SensitivityLabel,
		p.RecipientIdentity,
		p.Timestamp.UTC().Format(time.RFC3339),
	)

	return fmt.Sprintf(`<style>
@page { margin: 0; }
body::after {
  content: %q;
  position: fixed;
  top: 50%%;
  left: 50%%;
  transform: translate(-50%%, -50%%) rotate(-40deg);
  font-size: 36px;
  color: rgba(180, 0, 0, 0.18);
  white-space: nowrap;
  pointer-events: none;
  z-index: 9999;
}
</style>`, text)
}
