// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package render_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Steward-GRC/steward-pdf-renderer/internal/render"
)

// fakeBrowser is a script that records its arguments, writes a line to
// stderr and never opens DevTools, like a browser stuck at start-up.
func fakeBrowser(t *testing.T) (bin, argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args")
	bin = filepath.Join(dir, "chrome")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + argsFile + "'\necho 'fake browser: stuck before DevTools' >&2\nsleep 30\n"
	if err := os.WriteFile(bin, []byte(script), 0o700); err != nil { // #nosec G306 -- an executable test fixture
		t.Fatal(err)
	}
	return bin, argsFile
}

// The browser runs with chromedp's defaults, so a fresh profile never waits on
// first-run work or background network calls before opening DevTools.
func TestRenderStartsTheBrowserWithTheHeadlessDefaults(t *testing.T) {
	bin, argsFile := fakeBrowser(t)
	defer render.SetBrowserForTest(bin, 500*time.Millisecond)()

	_, err := render.ChromedpRenderer().Render(context.Background(), "<p>x</p>", "")
	if err == nil {
		t.Fatal("a browser that never opens DevTools must fail the render")
	}
	raw, rerr := os.ReadFile(argsFile)
	if rerr != nil {
		t.Fatalf("the fake browser didn't run: %v", rerr)
	}
	args := string(raw)
	for _, want := range []string{"--headless", "--no-sandbox", "--disable-gpu", "--no-first-run",
		"--no-default-browser-check", "--disable-background-networking", "--disable-sync"} {
		if !strings.Contains(args, want) {
			t.Errorf("browser args lack %s:\n%s", want, args)
		}
	}
}

// A browser that fails to start says why in the error, so a CI log shows the
// cause instead of a bare timeout.
func TestRenderReportsTheBrowserOutputWhenItFailsToStart(t *testing.T) {
	bin, _ := fakeBrowser(t)
	defer render.SetBrowserForTest(bin, 500*time.Millisecond)()

	_, err := render.ChromedpRenderer().Render(context.Background(), "<p>x</p>", "")
	if err == nil || !strings.Contains(err.Error(), "fake browser: stuck before DevTools") {
		t.Fatalf("want the browser's output in the error, got %v", err)
	}
}
