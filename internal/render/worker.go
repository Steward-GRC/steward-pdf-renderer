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

// DefaultStartTimeout is how long the browser has to open DevTools. A cold
// start on a busy shared machine can take well over chromedp's own 20s.
const DefaultStartTimeout = 60 * time.Second

// execPath overrides the browser binary chromedp looks up; empty finds it on
// PATH. testStartTimeout, when set, replaces every renderer's start timeout.
var (
	execPath         = ""
	testStartTimeout time.Duration
)

// Options tune the Chromium renderer.
type Options struct {
	// StartTimeout bounds the browser's start-up; zero means
	// DefaultStartTimeout.
	StartTimeout time.Duration
}

// ChromedpRenderer is the production Renderer that drives headless Chromium
// via chromedp, with the default options.
func ChromedpRenderer() Renderer { return NewChromedpRenderer(Options{}) }

// NewChromedpRenderer is ChromedpRenderer with opts.
func NewChromedpRenderer(opts Options) Renderer {
	return RendererFunc(func(ctx context.Context, htmlContent, watermarkCSS string) ([]byte, error) {
		return renderHTMLToPDF(ctx, htmlContent, watermarkCSS, opts)
	})
}

// RenderHTMLToPDF boots a fresh headless Chromium process
// (chromedp's defaults plus NoSandbox and DisableGPU — the container ships a Chromium binary
// with these flags as the supported mode), navigates to an in-memory data:
// URL containing htmlContent (optionally prefixed with watermarkCSS), and
// captures the rendered page as PDF bytes.
//
// The data: URL is base64-encoded so quotes, ampersands and non-ASCII text
// travel intact.
func RenderHTMLToPDF(ctx context.Context, htmlContent string, watermarkCSS string) ([]byte, error) {
	return renderHTMLToPDF(ctx, htmlContent, watermarkCSS, Options{})
}

func renderHTMLToPDF(ctx context.Context, htmlContent, watermarkCSS string, o Options) ([]byte, error) {
	startTimeout := o.StartTimeout
	if startTimeout <= 0 {
		startTimeout = DefaultStartTimeout
	}
	if testStartTimeout > 0 {
		startTimeout = testStartTimeout
	}
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
		chromedp.WSURLReadTimeout(startTimeout), chromedp.CombinedOutput(out))
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
