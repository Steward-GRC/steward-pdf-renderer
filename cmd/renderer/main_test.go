// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestLoadConfigMissingRequired confirms the required env vars produce a
// clear error rather than the renderer silently uploading an empty PDF.
func TestLoadConfigMissingRequired(t *testing.T) {
	// Can't t.Parallel() — we mutate process env, which would race with
	// other tests in this package. Restore on cleanup.
	for _, k := range []string{"FETCH_URL", "OUTPUT_BUCKET", "OUTPUT_KEY", "PV_ID", "SENSITIVITY"} {
		t.Setenv(k, "")
	}
	_, err := loadConfig()
	if err == nil {
		t.Fatalf("loadConfig with no env returned nil err")
	}
	for _, want := range []string{"FETCH_URL", "OUTPUT_BUCKET", "OUTPUT_KEY"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing required field %q", err, want)
		}
	}
}

// TestLoadConfigPopulated confirms a fully-populated env yields the expected
// runConfig shape; this is the contract the operator's Job spec relies on.
func TestLoadConfigPopulated(t *testing.T) {
	t.Setenv("PV_ID", "pv-1")
	t.Setenv("FETCH_URL", "http://example.invalid/html")
	t.Setenv("OUTPUT_BUCKET", "bkt")
	t.Setenv("OUTPUT_KEY", "k.pdf")
	t.Setenv("SENSITIVITY", "sensitive")
	t.Setenv("REQUESTED_BY", "u")
	t.Setenv("TRACE_ID", "trace-1")
	t.Setenv("AWS_REGION", "us-east-2")
	t.Setenv("AWS_S3_ENDPOINT", "http://objects.example.org:9000")
	t.Setenv("AWS_S3_FORCE_PATH_STYLE", "true")

	c, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig: %v", err)
	}
	if c.policyVersionID != "pv-1" || c.fetchURL == "" || c.outputBucket != "bkt" || c.outputKey != "k.pdf" {
		t.Errorf("loadConfig surface: %+v", c)
	}
	if c.sensitivity != "sensitive" || c.traceID != "trace-1" || c.requestedBy != "u" {
		t.Errorf("loadConfig metadata: %+v", c)
	}
	if c.s3Region != "us-east-2" || c.s3Endpoint != "http://objects.example.org:9000" || !c.s3ForcePathStyle {
		t.Errorf("loadConfig s3: %+v", c)
	}
}

// TestFetchHTMLHappyPath uses an httptest server to verify the fetch hits
// the URL and returns the body verbatim.
func TestFetchHTMLHappyPath(t *testing.T) {
	t.Parallel()

	const want = "<html><body><h1>policy</h1></body></html>"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(want))
	}))
	t.Cleanup(srv.Close)

	got, err := fetchHTML(context.Background(), srv.URL, 5*time.Second)
	if err != nil {
		t.Fatalf("fetchHTML: %v", err)
	}
	if got != want {
		t.Errorf("body = %q, want %q", got, want)
	}
}

// TestFetchHTMLNon2xx confirms a non-2xx response is surfaced as an error
// (rather than the renderer silently rendering an empty body or an HTML
// error page as a "PDF").
func TestFetchHTMLNon2xx(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	if _, err := fetchHTML(context.Background(), srv.URL, 5*time.Second); err == nil {
		t.Errorf("fetchHTML on 500 returned nil err")
	}
}

// TestFetchHTMLEmptyURL guards the obvious misconfig.
func TestFetchHTMLEmptyURL(t *testing.T) {
	t.Parallel()
	if _, err := fetchHTML(context.Background(), "", time.Second); err == nil {
		t.Errorf("fetchHTML(\"\") returned nil err")
	}
}
