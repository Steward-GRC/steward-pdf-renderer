// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package render_test

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Steward-GRC/steward-pdf-renderer/internal/render"
)

func sample() render.WatermarkParams {
	return render.WatermarkParams{
		SensitivityLabel:  "SENSITIVE",
		RecipientIdentity: "erin@example.org",
		Timestamp:         time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC),
	}
}

// tile returns the SVG the watermark tiles, decoded from the CSS data URI.
func tile(t *testing.T, css string) string {
	t.Helper()
	m := regexp.MustCompile(`url\("data:image/svg\+xml,([^"]+)"\)`).FindStringSubmatch(css)
	if m == nil {
		t.Fatalf("watermark CSS has no SVG tile:\n%s", css)
	}
	svg, err := url.QueryUnescape(m[1])
	if err != nil {
		t.Fatalf("tile isn't URL-encoded SVG: %v", err)
	}
	return svg
}

// TestWatermarkCarriesLabelIdentityAndUTCTime: the visible audit trail must
// match the audit row, so the label, the requester and the RFC3339 UTC time
// are printed verbatim, in the stored "LABEL · name · date" order.
func TestWatermarkCarriesLabelIdentityAndUTCTime(t *testing.T) {
	t.Parallel()

	svg := tile(t, render.WatermarkCSS(sample()))
	for _, want := range []string{"SENSITIVE · erin@example.org", "2026-05-30T12:00:00Z"} {
		if !strings.Contains(svg, want) {
			t.Errorf("tile text missing %q:\n%s", want, svg)
		}
	}
}

func TestWatermarkTimestampIsUTC(t *testing.T) {
	t.Parallel()

	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	p := sample()
	p.Timestamp = time.Date(2026, 5, 30, 8, 0, 0, 0, loc) // 12:00 UTC
	if !strings.Contains(tile(t, render.WatermarkCSS(p)), "2026-05-30T12:00:00Z") {
		t.Error("expected the UTC time in the tile")
	}
}

// TestWatermarkFollowsStoredDesign: red so a photocopy keeps it, Plex Mono,
// -30 degrees, tiled 3 across and 4 down on every page.
func TestWatermarkFollowsStoredDesign(t *testing.T) {
	t.Parallel()

	css := render.WatermarkCSS(sample())
	for _, want := range []string{
		"<style>", "body::after", "position: fixed",
		"background-size: 33.3333% 25%",
	} {
		if !strings.Contains(css, want) {
			t.Errorf("CSS missing %q:\n%s", want, css)
		}
	}
	svg := tile(t, css)
	for _, want := range []string{"IBM Plex Mono", "rotate(-30", "rgba(180, 0, 0, 0.18)"} {
		if !strings.Contains(svg, want) {
			t.Errorf("tile missing %q:\n%s", want, svg)
		}
	}
}

// TestWatermarkLeavesThePageMargins: the old overlay set @page margin to 0,
// which threw away the document's own margins.
func TestWatermarkLeavesThePageMargins(t *testing.T) {
	t.Parallel()

	if strings.Contains(render.WatermarkCSS(sample()), "@page") {
		t.Error("the watermark must not touch @page")
	}
}

// TestWatermarkTextCannotBreakOut: hostile text must not end the SVG or the
// style block.
func TestWatermarkTextCannotBreakOut(t *testing.T) {
	t.Parallel()

	p := sample()
	p.RecipientIdentity = `</style><script>"x"&'`
	css := render.WatermarkCSS(p)
	if strings.Count(css, "</style>") != 1 || strings.Contains(css, "<script>") {
		t.Errorf("text broke out of the style block:\n%s", css)
	}
	svg := tile(t, css)
	if strings.Contains(svg, "<script>") || strings.Contains(svg, "</style>") {
		t.Errorf("text broke out of the SVG:\n%s", svg)
	}
}

// TestWatermarkFontFitsTheTile: 28 pt is the cap; the two lines are set
// smaller so the longer stays inside its tile, but never below a readable size.
func TestWatermarkFontFitsTheTile(t *testing.T) {
	t.Parallel()

	m := regexp.MustCompile(`font-size="([0-9.]+)"`).FindStringSubmatch(tile(t, render.WatermarkCSS(sample())))
	if m == nil {
		t.Fatal("no font size in the tile")
	}
	size, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatal(err)
	}
	if size > 28 || size < 12 {
		t.Errorf("font size %v pt, want 12 to 28", size)
	}
}

// TestWatermarkTileIsValidDataURI guards the encoding of the tile in the CSS:
// in a data: URI a "+" is a literal plus, not a space, so a form-encoded tile
// ("<svg+xmlns=...") is not SVG and Chromium draws no watermark at all.
func TestWatermarkTileIsValidDataURI(t *testing.T) {
	t.Parallel()

	m := regexp.MustCompile(`url\("data:image/svg\+xml,([^"]+)"\)`).FindStringSubmatch(render.WatermarkCSS(sample()))
	if m == nil {
		t.Fatal("watermark CSS has no SVG tile")
	}
	if strings.Contains(m[1], "+") {
		t.Errorf("tile carries a literal %q, which a data: URI reads as a plus sign:\n%s", "+", m[1])
	}
	svg, err := url.PathUnescape(m[1])
	if err != nil {
		t.Fatalf("tile isn't percent-encoded SVG: %v", err)
	}
	if !strings.HasPrefix(svg, "<svg xmlns=") {
		t.Errorf("decoded tile does not start with an SVG element:\n%s", svg)
	}
}
