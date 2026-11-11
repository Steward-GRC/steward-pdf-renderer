// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"fmt"
	"html"
	"strings"
	"time"
	"unicode"

	"github.com/chromedp/cdproto/page"
)

// PaperSize is a page size the print frame supports.
type PaperSize string

// The page sizes of the stored print design (R1: "A4 and Letter safe").
const (
	PaperA4     PaperSize = "A4"
	PaperLetter PaperSize = "Letter"
)

// Valid reports whether p is A4 or Letter.
func (p PaperSize) Valid() bool { return p == PaperA4 || p == PaperLetter }

// Print margins of the stored design (R1: 20 / 18 mm): top and bottom, then
// left and right.
const (
	marginVerticalMM   = 20.0
	marginHorizontalMM = 18.0
	mmPerInch          = 25.4
)

// printedAtLayout is how the footer writes the print time: "3 Oct 2026 09:30".
const printedAtLayout = "2 Jan 2006 15:04"

// Frame is what the stored print design (R1) draws around a document on
// every page: the margins, a running header and a running footer.
//
// The header carries the organisation on the left and "number · version ·
// status" on the right. The footer carries "owner · Printed date" on the left
// and "Steward · Page n of N" on the right. A field left empty is left out.
// Every field is HTML-escaped into the templates.
type Frame struct {
	// Organisation is the organisation's name, with its initials as a mark.
	Organisation string
	// Number is the document number, e.g. "POL-FACILITIES-000001".
	Number string
	// Version is the document version, e.g. "v1.0.0".
	Version string
	// Status is the document status, e.g. "Published".
	Status string
	// Owner is the document owner, printed in the footer.
	Owner string
	// PrintedAt is when the PDF was produced, printed in UTC.
	PrintedAt time.Time
	// Paper is the page size; empty means A4.
	Paper PaperSize
}

// PrintParams returns the Chromium print options for the frame: paper size,
// margins, and the header and footer templates, with backgrounds on so the
// watermark prints.
func (f Frame) PrintParams() *page.PrintToPDFParams {
	w, h := f.paperInches()
	return page.PrintToPDF().
		WithPrintBackground(true).
		WithPaperWidth(w).
		WithPaperHeight(h).
		WithMarginTop(marginVerticalMM / mmPerInch).
		WithMarginBottom(marginVerticalMM / mmPerInch).
		WithMarginLeft(marginHorizontalMM / mmPerInch).
		WithMarginRight(marginHorizontalMM / mmPerInch).
		WithDisplayHeaderFooter(true).
		WithHeaderTemplate(f.headerTemplate()).
		WithFooterTemplate(f.footerTemplate())
}

// paperInches is the page size in inches.
func (f Frame) paperInches() (width, height float64) {
	if f.Paper == PaperLetter {
		return 8.5, 11
	}
	return 210 / mmPerInch, 297 / mmPerInch
}

// runningStyle is shared by both templates. Chromium gives a template no
// styles of its own (the font size is 0), so everything is set here. The
// padding keeps the text inside the 18 mm side margins.
const runningStyle = "box-sizing:border-box;width:100%;padding:0 18mm;display:flex;" +
	"justify-content:space-between;align-items:center;gap:12px;" +
	"font-family:'Public Sans','Helvetica Neue',Arial,sans-serif;color:#5E525F;" +
	"-webkit-print-color-adjust:exact;"

func (f Frame) headerTemplate() string {
	var left string
	if f.Organisation != "" {
		left = fmt.Sprintf(`<span style="display:flex;gap:6px;align-items:center">`+
			`<span style="width:22px;height:14px;border-radius:2px;background:#E4DACC;font-size:6pt;font-weight:600;`+
			`display:flex;align-items:center;justify-content:center">%s</span>%s</span>`,
			html.EscapeString(initials(f.Organisation)), html.EscapeString(f.Organisation))
	}
	right := joinFields(" · ", f.Number, f.Version, f.Status)
	return fmt.Sprintf(`<div style="%sfont-size:8.5pt;font-weight:500">`+
		`<div style="width:100%%;display:flex;justify-content:space-between;align-items:center;`+
		`border-bottom:.5pt solid #261B25;padding-bottom:6px">`+
		`<span>%s</span><span style="font-family:'IBM Plex Mono',monospace">%s</span></div></div>`,
		runningStyle, left, html.EscapeString(right))
}

func (f Frame) footerTemplate() string {
	left := joinFields(" · ", f.Owner, "Printed "+f.PrintedAt.UTC().Format(printedAtLayout))
	return fmt.Sprintf(`<div style="%sfont-size:8pt">`+
		`<div style="width:100%%;display:flex;justify-content:space-between;gap:12px;`+
		`border-top:.5pt solid #B9AAB7;padding-top:6px">`+
		`<span>%s</span><span style="white-space:nowrap">Steward · Page <span class="pageNumber"></span> of <span class="totalPages"></span></span>`+
		`</div></div>`,
		runningStyle, html.EscapeString(left))
}

// joinFields joins the fields that are set.
func joinFields(sep string, fields ...string) string {
	set := fields[:0:0]
	for _, v := range fields {
		if strings.TrimSpace(v) != "" {
			set = append(set, v)
		}
	}
	return strings.Join(set, sep)
}

// initials is the first letter of up to the first two words, upper-cased.
func initials(name string) string {
	var b strings.Builder
	for _, w := range strings.Fields(name) {
		if b.Len() >= 2 {
			break
		}
		for _, r := range w {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				b.WriteRune(unicode.ToUpper(r))
				break
			}
		}
	}
	return b.String()
}
