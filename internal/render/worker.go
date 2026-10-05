// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// Renderer turns HTML, with an optional watermark style block, into PDF
// bytes.
type Renderer interface {
	Render(ctx context.Context, htmlContent string, watermarkCSS string) ([]byte, error)
}

// RendererFunc adapts a plain function to the Renderer interface.
type RendererFunc func(ctx context.Context, htmlContent, watermarkCSS string) ([]byte, error)

// Render implements Renderer.
func (f RendererFunc) Render(ctx context.Context, htmlContent, watermarkCSS string) ([]byte, error) {
	return f(ctx, htmlContent, watermarkCSS)
}

// ChromedpRenderer is the production Renderer that drives headless Chromium
// via chromedp.
func ChromedpRenderer() Renderer { return RendererFunc(RenderHTMLToPDF) }

// RenderHTMLToPDF boots a fresh headless Chromium process
// (NoSandbox/Headless/DisableGPU — the container ships a Chromium binary
// with these flags as the supported mode), navigates to an in-memory data:
// URL containing htmlContent (optionally prefixed with watermarkCSS), and
// captures the rendered page as PDF bytes.
//
// The data: URL is base64-encoded so quotes, ampersands and non-ASCII text
// travel intact.
func RenderHTMLToPDF(ctx context.Context, htmlContent string, watermarkCSS string) ([]byte, error) {
	if watermarkCSS != "" {
		// Prepend the watermark <style> so it overrides any conflicting rules
		// in the policy HTML's own <head>.
		htmlContent = fmt.Sprintf("<!doctype html><head>%s</head>%s", watermarkCSS, htmlContent)
	}

	dataURL := "data:text/html;base64," + base64.StdEncoding.EncodeToString([]byte(htmlContent))

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx,
		chromedp.NoSandbox,
		chromedp.Headless,
		chromedp.DisableGPU,
	)
	defer cancelAlloc()

	browserCtx, cancelBrowser := chromedp.NewContext(allocCtx)
	defer cancelBrowser()

	var buf []byte
	if err := chromedp.Run(browserCtx,
		chromedp.Navigate(dataURL),
		chromedp.ActionFunc(func(ctx context.Context) error {
			var err error
			buf, _, err = page.PrintToPDF().
				WithPrintBackground(true).
				Do(ctx)
			return err
		}),
	); err != nil {
		return nil, fmt.Errorf("chromedp: %w", err)
	}
	return buf, nil
}
