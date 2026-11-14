// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Bugs5382/go-objectstore/memstore"
)

// closedAddr returns a loopback address nothing listens on, so a dial to it
// is refused.
func closedAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	if err := l.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return addr
}

func retryConfig(url string, window time.Duration) runConfig {
	c := testConfig(url, "standard")
	c.fetchRetryWindow = window
	c.fetchRetryInitial = 5 * time.Millisecond
	c.fetchRetryMax = 20 * time.Millisecond
	return c
}

func TestFetchRetriesRefusedDialThenSucceeds(t *testing.T) {
	t.Parallel()
	addr := closedAddr(t)

	// The listener comes up after a few refused dials, as the network admits
	// the new pod.
	go func() {
		time.Sleep(60 * time.Millisecond)
		l, err := net.Listen("tcp", addr)
		if err != nil {
			return
		}
		srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(page))
		}), ReadHeaderTimeout: time.Second}
		go func() { _ = srv.Serve(l) }()
		t.Cleanup(func() { _ = srv.Close() })
	}()

	r := &recordingRenderer{}
	cfg := retryConfig("http://"+addr+"/html", 5*time.Second)
	if err := renderPDF(context.Background(), cfg, r, memstore.New(), testLogger()); err != nil {
		t.Fatalf("renderPDF: %v", err)
	}
	if r.html != page {
		t.Errorf("renderer got %q", r.html)
	}
}

func TestFetchGivesUpAfterTheRetryWindow(t *testing.T) {
	t.Parallel()
	cfg := retryConfig("http://"+closedAddr(t)+"/html", 80*time.Millisecond)

	start := time.Now()
	_, err := fetchHTMLRetry(context.Background(), cfg, testLogger())
	if err == nil {
		t.Fatal("fetch succeeded against a closed port")
	}
	if elapsed := time.Since(start); elapsed < 50*time.Millisecond || elapsed > 3*time.Second {
		t.Errorf("gave up after %v, want about the 80ms window", elapsed)
	}
}

func TestFetchDoesNotRetryAStatusAnswer(t *testing.T) {
	t.Parallel()
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError} {
		var hits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			hits.Add(1)
			w.WriteHeader(status)
		}))
		cfg := retryConfig(srv.URL, 5*time.Second)
		if _, err := fetchHTMLRetry(context.Background(), cfg, testLogger()); err == nil {
			t.Errorf("status %d: fetch succeeded", status)
		}
		if n := hits.Load(); n != 1 {
			t.Errorf("status %d: server hit %d times, want 1", status, n)
		}
		srv.Close()
	}
}

func TestFetchStopsRetryingWhenTheContextEnds(t *testing.T) {
	t.Parallel()
	cfg := retryConfig("http://"+closedAddr(t)+"/html", time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := fetchHTMLRetry(ctx, cfg, testLogger()); err == nil {
		t.Fatal("fetch succeeded against a closed port")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("kept retrying %v after the context ended", elapsed)
	}
}

func TestFetchWithoutARetryWindowDialsOnce(t *testing.T) {
	t.Parallel()
	cfg := retryConfig("http://"+closedAddr(t)+"/html", 0)
	start := time.Now()
	if _, err := fetchHTMLRetry(context.Background(), cfg, testLogger()); err == nil {
		t.Fatal("fetch succeeded against a closed port")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("took %v with no retry window", elapsed)
	}
}
