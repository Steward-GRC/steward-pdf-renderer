// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	log "github.com/Bugs5382/go-log"
	"github.com/Bugs5382/go-objectstore"
	"github.com/Bugs5382/go-objectstore/memstore"

	"github.com/Steward-GRC/steward-pdf-renderer/internal/render"
)

const page = "<html><body><h1>Desk Booking Policy</h1></body></html>"

type recordingRenderer struct {
	mu        sync.Mutex
	html      string
	watermark string
}

func (r *recordingRenderer) Render(_ context.Context, html, watermarkCSS string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.html, r.watermark = html, watermarkCSS
	return []byte("%PDF-1.7 rendered"), nil
}

func htmlServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(page))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func testConfig(fetchURL, sensitivity string) runConfig {
	return runConfig{
		policyVersionID: "pv-42",
		fetchURL:        fetchURL,
		outputBucket:    "steward-artifacts",
		outputKey:       "artifacts/pv-42/render-0001.pdf",
		sensitivity:     sensitivity,
		requestedBy:     "erin",
		httpTimeout:     defaultHTTPTimeout,
		renderTimeout:   defaultRenderTimeout,
	}
}

func readObject(t *testing.T, store objectstore.Store, key string) (string, string) {
	t.Helper()
	obj, err := store.Get(context.Background(), key, objectstore.GetOptions{})
	if err != nil {
		t.Fatalf("get %s: %v", key, err)
	}
	defer func() { _ = obj.Body.Close() }()
	body, err := io.ReadAll(obj.Body)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	st, err := store.Stat(context.Background(), key)
	if err != nil {
		t.Fatalf("stat %s: %v", key, err)
	}
	return string(body), st.ContentType
}

func TestRenderStandardUploadsWithoutWatermark(t *testing.T) {
	t.Parallel()
	srv := htmlServer(t, http.StatusOK)
	store := memstore.New()
	r := &recordingRenderer{}

	if err := renderPDF(context.Background(), testConfig(srv.URL, "standard"), r, store, testLogger()); err != nil {
		t.Fatalf("renderPDF: %v", err)
	}
	if r.html != page || r.watermark != "" {
		t.Errorf("renderer got html=%q watermark=%q", r.html, r.watermark)
	}
	body, ct := readObject(t, store, "artifacts/pv-42/render-0001.pdf")
	if body != "%PDF-1.7 rendered" || ct != "application/pdf" {
		t.Errorf("stored %q as %q", body, ct)
	}
}

func TestRenderSensitiveAddsWatermark(t *testing.T) {
	t.Parallel()
	srv := htmlServer(t, http.StatusOK)
	r := &recordingRenderer{}

	if err := renderPDF(context.Background(), testConfig(srv.URL, "sensitive"), r, memstore.New(), testLogger()); err != nil {
		t.Fatalf("renderPDF: %v", err)
	}
	for _, want := range []string{"CONFIDENTIAL", "erin", "body::after"} {
		if !strings.Contains(r.watermark, want) {
			t.Errorf("watermark missing %q: %s", want, r.watermark)
		}
	}
}

func TestRenderFetchFailureStoresNothing(t *testing.T) {
	t.Parallel()
	srv := htmlServer(t, http.StatusInternalServerError)
	store := memstore.New()
	r := &recordingRenderer{}

	if err := renderPDF(context.Background(), testConfig(srv.URL, "standard"), r, store, testLogger()); err == nil {
		t.Fatal("renderPDF succeeded on a failed fetch")
	}
	if _, err := store.Stat(context.Background(), "artifacts/pv-42/render-0001.pdf"); err == nil {
		t.Error("an object was stored after a failed fetch")
	}
	if r.html != "" {
		t.Error("the renderer ran after a failed fetch")
	}
}

func TestRenderFailureStoresNothing(t *testing.T) {
	t.Parallel()
	srv := htmlServer(t, http.StatusOK)
	store := memstore.New()
	failing := render.RendererFunc(func(context.Context, string, string) ([]byte, error) {
		return nil, io.ErrUnexpectedEOF
	})

	if err := renderPDF(context.Background(), testConfig(srv.URL, "standard"), failing, store, testLogger()); err == nil {
		t.Fatal("renderPDF succeeded on a failed render")
	}
	if _, err := store.Stat(context.Background(), "artifacts/pv-42/render-0001.pdf"); err == nil {
		t.Error("an object was stored after a failed render")
	}
}

func TestStoreConfigFromEnvironment(t *testing.T) {
	t.Parallel()
	c := testConfig("http://delivery.example.org/x", "standard")
	c.s3Endpoint, c.s3Region, c.s3AccessKey, c.s3SecretKey, c.s3ForcePathStyle = "http://objects.example.org:9000", "us-east-2", "ak", "sk", true
	sc := storeConfig(c)
	if sc.Endpoint != c.s3Endpoint || sc.Region != "us-east-2" || sc.Bucket != "steward-artifacts" ||
		sc.AccessKeyID != "ak" || sc.SecretAccessKey != "sk" || !sc.PathStyle {
		t.Errorf("store config = %+v", sc)
	}
}

func testLogger() log.Logger { return log.Nop() }
