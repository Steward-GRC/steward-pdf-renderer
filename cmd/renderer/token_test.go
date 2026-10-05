// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Bugs5382/go-objectstore/memstore"
)

// authSink answers 200 and records each request's Authorization header.
func authSink(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get("Authorization"))
		mu.Unlock()
		_, _ = w.Write([]byte("<p>ok</p>"))
	}))
	t.Cleanup(srv.Close)
	return srv, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), seen...)
	}
}

func TestFetchHTMLSendsTheTokenReReadOnEveryFetch(t *testing.T) {
	srv, seen := authSink(t)
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("first\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fetchHTMLAs(context.Background(), srv.URL, 5*time.Second, file); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if err := os.WriteFile(file, []byte("rotated"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fetchHTMLAs(context.Background(), srv.URL, 5*time.Second, file); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	got := seen()
	if len(got) != 2 || got[0] != "Bearer first" || got[1] != "Bearer rotated" {
		t.Errorf("Authorization headers = %q", got)
	}
}

func TestFetchHTMLFailsClosedOnAMissingOrEmptyToken(t *testing.T) {
	srv, seen := authSink(t)
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, file := range map[string]string{"missing": filepath.Join(dir, "absent"), "empty": empty} {
		_, err := fetchHTMLAs(context.Background(), srv.URL, 5*time.Second, file)
		if err == nil || !strings.Contains(err.Error(), "WORKLOAD_TOKEN_FILE") {
			t.Errorf("%s: want an error naming WORKLOAD_TOKEN_FILE, got %v", name, err)
		}
	}
	if got := seen(); len(got) != 0 {
		t.Errorf("no request may go out without the token, got %q", got)
	}
}

func TestFetchHTMLWithNoTokenFileSendsNone(t *testing.T) {
	srv, seen := authSink(t)
	if _, err := fetchHTMLAs(context.Background(), srv.URL, 5*time.Second, ""); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got := seen(); len(got) != 1 || got[0] != "" {
		t.Errorf("Authorization headers = %q, want none", got)
	}
}

func TestLoadConfigNeedsATokenFileUnlessDisabled(t *testing.T) {
	t.Setenv("FETCH_URL", "http://example.invalid/html")
	t.Setenv("OUTPUT_BUCKET", "bkt")
	t.Setenv("OUTPUT_KEY", "k.pdf")
	t.Setenv("WORKLOAD_TOKEN_FILE", "")
	t.Setenv("WORKLOAD_AUTH", "")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "WORKLOAD_TOKEN_FILE") {
		t.Fatalf("want an error naming WORKLOAD_TOKEN_FILE, got %v", err)
	}
	t.Setenv("WORKLOAD_AUTH", "disabled")
	c, err := loadConfig()
	if err != nil || c.tokenFile != "" {
		t.Fatalf("disabled: %+v %v", c, err)
	}
	t.Setenv("WORKLOAD_AUTH", "off")
	if _, err := loadConfig(); err == nil || !strings.Contains(err.Error(), "WORKLOAD_AUTH") {
		t.Fatalf("want an error naming WORKLOAD_AUTH, got %v", err)
	}
	t.Setenv("WORKLOAD_AUTH", "")
	t.Setenv("WORKLOAD_TOKEN_FILE", "/var/run/secrets/steward/token")
	c, err = loadConfig()
	if err != nil || c.tokenFile != "/var/run/secrets/steward/token" {
		t.Fatalf("token file: %+v %v", c, err)
	}
}

func TestRenderSendsTheTokenOnTheFetch(t *testing.T) {
	srv, seen := authSink(t)
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("tok"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(srv.URL, "")
	cfg.tokenFile = file
	if err := renderPDF(context.Background(), cfg, &recordingRenderer{}, memstore.New(), testLogger()); err != nil {
		t.Fatalf("renderPDF: %v", err)
	}
	if got := seen(); len(got) != 1 || got[0] != "Bearer tok" {
		t.Errorf("Authorization headers = %q", got)
	}
}
