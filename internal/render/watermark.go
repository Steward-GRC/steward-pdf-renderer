// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Package render turns HTML into a PDF with headless Chromium and draws the
// sensitive-document watermark.
package render

import (
	"encoding/xml"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

// WatermarkParams carries the data injected into the watermark overlay.
//
// Every sensitive PDF export must show, on every page, who pulled it and
// when. The watermark is the visible audit trail; the same fields are
// recorded in the audit log by the caller.
type WatermarkParams struct {
	// SensitivityLabel is the policy's sensitivity tier (the stored design prints "SENSITIVE").
	// Rendered verbatim; callers normalize casing before passing it in.
	SensitivityLabel string
	// RecipientIdentity is the requester's user ID or email. Recipient
	// attribution is the deterrent against unauthorized re-sharing.
	RecipientIdentity string
	// Timestamp is the time of the export request, printed as RFC3339 UTC
	// so the banner reads the same in every time zone and matches the audit
	// log.
	Timestamp time.Time
}

// Stored design (R2): "SENSITIVE · name · date" in IBM Plex Mono, at most 28 pt,
// rotated -30 degrees and tiled 3 across by 4 down on every page. It is red,
// not the design's faint purple, so that a photocopy keeps
// it; the opacity is the one the first version of this overlay used.
const (
	watermarkColour  = "rgba(180, 0, 0, 0.18)"
	watermarkMaxSize = 28.0
	// tileW and tileH are one tile in points: an A4 or Letter page split 3 by 4.
	tileW = 200.0
	tileH = 210.0
	// fitWidth is the room a rotated line has inside a tile, in points, divided
	// by the width of one mono glyph at 1 pt rotated by -30 degrees.
	fitWidth = 365.0
)

// WatermarkCSS returns a <style> block that, when injected into the rendered
// HTML before PDF capture, draws a fixed full-page watermark on every page.
// The text reads:
//
//	"<SensitivityLabel> · <RecipientIdentity>" over "<Timestamp RFC3339 UTC>"
//
// Design choices:
//
//   - CSS-only (no post-process). chromedp prints exactly what Chromium
//     renders, so an overlay piggybacks on the same render pass and works for
//     any number of pages without per-page assembly.
//   - body::after with position:fixed repeats across paginated output in
//     Chromium's print pipeline; its background is one SVG tile repeated 3 by
//     4, so there is no markup to inject.
//   - The text is XML-escaped into the SVG and the SVG is URL-encoded into the
//     CSS string, so hostile input can end neither the SVG nor the style block.
//   - It does not touch @page, so the document keeps its own margins.
//   - The text is set at 28 pt at most, in two lines (who, then when), and shrinks
//     to keep its longer line inside its tile.
func WatermarkCSS(p WatermarkParams) string {
	who := fmt.Sprintf("%s · %s", p.SensitivityLabel, p.RecipientIdentity)
	when := p.Timestamp.UTC().Format(time.RFC3339)

	// Two lines, so the longer of them fits the tile at a size that still reads.
	longest := max(utf8.RuneCountInString(who), utf8.RuneCountInString(when))
	size := math.Min(watermarkMaxSize, fitWidth/float64(longest))
	size = math.Floor(size*10) / 10

	line := func(text string) string {
		var b strings.Builder
		if err := xml.EscapeText(&b, []byte(text)); err != nil {
			return ""
		}
		return b.String()
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%[1]g" height="%[2]g" viewBox="0 0 %[1]g %[2]g">`+
		`<text x="%[3]g" y="%[4]g" transform="rotate(-30 %[3]g %[4]g)" text-anchor="middle" `+
		`font-family="IBM Plex Mono, monospace" font-size="%[5]g" fill="%[6]s">`+
		`<tspan x="%[3]g" dy="-0.2em">%[7]s</tspan><tspan x="%[3]g" dy="1.2em">%[8]s</tspan></text></svg>`,
		tileW, tileH, tileW/2, tileH/2, size, watermarkColour, line(who), line(when))

	return fmt.Sprintf(`<style>
body::after {
  content: "";
  position: fixed;
  inset: 0;
  background-image: url("data:image/svg+xml,%s");
  background-size: 33.3333%% 25%%;
  background-repeat: repeat;
  pointer-events: none;
  z-index: 9999;
  -webkit-print-color-adjust: exact;
  print-color-adjust: exact;
}
</style>`, dataURIEscape(svg))
}

// dataURIEscape percent-encodes s for a data: URI. url.QueryEscape writes a
// space as "+", which a data: URI reads as a plus sign, so the spaces are
// written as %20 instead; a literal plus is already %2B.
func dataURIEscape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}
