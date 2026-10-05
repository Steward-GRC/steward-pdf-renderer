// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package render

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"sync"
	"time"

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

// execPath overrides the browser binary chromedp looks up; empty finds it on
// PATH. wsURLReadTimeout bounds how long the browser has to open DevTools.
var (
	execPath         = ""
	wsURLReadTimeout = 20 * time.Second
)

// ChromedpRenderer is the production Renderer that drives headless Chromium
// via chromedp.
func ChromedpRenderer() Renderer { return RendererFunc(RenderHTMLToPDF) }

// RenderHTMLToPDF boots a fresh headless Chromium process
// (chromedp's defaults plus NoSandbox and DisableGPU — the container ships a Chromium binary
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

	// chromedp's defaults keep a fresh profile from waiting on first-run work
	// or background network calls (component updates, sync, field trials)
	// before it opens DevTools.
	out := &browserOutput{}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.NoSandbox, chromedp.DisableGPU,
		chromedp.WSURLReadTimeout(wsURLReadTimeout), chromedp.CombinedOutput(out))
	if execPath != "" {
		opts = append(opts, chromedp.ExecPath(execPath))
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
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
		if tail := out.String(); tail != "" {
			return nil, fmt.Errorf("chromedp: %w; browser output: %s", err, tail)
		}
		return nil, fmt.Errorf("chromedp: %w", err)
	}
	return buf, nil
}

// browserOutputMax caps what is kept of the browser's output: the last lines
// are the ones that say why it didn't start.
const browserOutputMax = 4 << 10

// browserOutput keeps the tail of the browser's stdout and stderr.
type browserOutput struct {
	mu  sync.Mutex
	buf []byte
}

func (b *browserOutput) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if over := len(b.buf) - browserOutputMax; over > 0 {
		b.buf = b.buf[over:]
	}
	return len(p), nil
}

func (b *browserOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(string(b.buf))
}
