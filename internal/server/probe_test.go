// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Bugs5382/go-buildinfo/health"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"

	"github.com/Steward-GRC/steward-pdf-renderer/internal/server"
)

// apiServer answers the discovery /version call the way a Kubernetes API
// server does, on a fixed address so it can be stopped and started again.
type apiServer struct {
	t    *testing.T
	addr string
	srv  *http.Server
}

func startAPIServer(t *testing.T, addr string) *apiServer {
	t.Helper()
	var l net.Listener
	var err error
	for i := 0; i < 50; i++ {
		if l, err = net.Listen("tcp", addr); err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("listen %s: %v", addr, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/version", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"major":"1","minor":"34","gitVersion":"v1.34.2"}`))
	})
	s := &apiServer{t: t, addr: l.Addr().String(), srv: &http.Server{Handler: mux, ReadHeaderTimeout: time.Second}}
	go func() { _ = s.srv.Serve(l) }()
	t.Cleanup(func() { _ = s.srv.Close() })
	return s
}

func (s *apiServer) stop() {
	if err := s.srv.Close(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		s.t.Fatalf("stop: %v", err)
	}
}

func newProbes(t *testing.T, host string) http.Handler {
	t.Helper()
	dc, err := discovery.NewDiscoveryClientForConfig(&rest.Config{Host: "http://" + host})
	if err != nil {
		t.Fatalf("discovery client: %v", err)
	}
	checker := health.New(health.WithTTL(50*time.Millisecond), health.WithTimeout(500*time.Millisecond))
	if err := checker.Register(server.KubernetesDependency(dc.RESTClient())); err != nil {
		t.Fatalf("register: %v", err)
	}
	h, err := server.Probes(checker)
	if err != nil {
		t.Fatalf("Probes: %v", err)
	}
	return h
}

type readyBody struct {
	Ready        bool `json:"ready"`
	Dependencies []struct {
		Name     string `json:"name"`
		State    string `json:"state"`
		Required bool   `json:"required"`
		Version  string `json:"version"`
		Error    string `json:"error"`
	} `json:"dependencies"`
}

func get(t *testing.T, h http.Handler, path string) (*httptest.ResponseRecorder, readyBody) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil))
	var b readyBody
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	return rec, b
}

func TestProbesCarryBuildHeaders(t *testing.T) {
	t.Parallel()
	api := startAPIServer(t, "127.0.0.1:0")
	h := newProbes(t, api.addr)

	for _, path := range []string{"/livez", "/readyz"} {
		rec, _ := get(t, h, path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", path, rec.Code)
		}
		if rec.Header().Get("Steward-Version") == "" || rec.Header().Get("Steward-Commit") == "" {
			t.Errorf("%s headers = %v, want Steward-Version and Steward-Commit", path, rec.Header())
		}
	}
	rec, _ := get(t, h, "/health")
	if rec.Code != http.StatusNotFound {
		t.Errorf("/health = %d, want 404", rec.Code)
	}
}

func TestReadinessFollowsTheAPIServer(t *testing.T) {
	t.Parallel()
	api := startAPIServer(t, "127.0.0.1:0")
	addr := api.addr
	h := newProbes(t, addr)

	rec, body := get(t, h, "/readyz")
	if rec.Code != http.StatusOK || !body.Ready {
		t.Fatalf("readyz with the API server up = %d %s", rec.Code, rec.Body)
	}
	if len(body.Dependencies) != 1 {
		t.Fatalf("dependencies = %+v", body.Dependencies)
	}
	dep := body.Dependencies[0]
	if dep.Name != "kubernetes" || !dep.Required || dep.State != "ok" || dep.Version != "v1.34.2" {
		t.Errorf("dependency = %+v", dep)
	}
	if got := rec.Header().Get("Steward-Dep-Kubernetes"); got != "v1.34.2" {
		t.Errorf("Steward-Dep-Kubernetes = %q, want v1.34.2", got)
	}
	if got := rec.Header().Get("Steward-Depstate-Kubernetes"); got != "ok" {
		t.Errorf("Steward-Depstate-Kubernetes = %q, want ok", got)
	}

	api.stop()
	waitFor(t, func() bool {
		rec, _ := get(t, h, "/readyz")
		return rec.Code == http.StatusServiceUnavailable
	})
	rec, body = get(t, h, "/readyz")
	if body.Ready || body.Dependencies[0].State != "down" || body.Dependencies[0].Error == "" {
		t.Errorf("readyz with the API server down = %s", rec.Body)
	}
	if rec, _ := get(t, h, "/livez"); rec.Code != http.StatusOK {
		t.Errorf("livez with the API server down = %d, want 200", rec.Code)
	}

	startAPIServer(t, addr)
	waitFor(t, func() bool {
		rec, _ := get(t, h, "/readyz")
		return rec.Code == http.StatusOK
	})
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatal("condition not reached within 5s")
}
