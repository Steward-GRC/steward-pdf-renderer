// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Command operator watches PdfRender resources and runs one renderer Job for
// each. Replicas share the work through leader election; only the leader
// reconciles, and every replica serves its probes.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"time"

	buildinfo "github.com/Bugs5382/go-buildinfo"
	"github.com/Bugs5382/go-buildinfo/health"
	log "github.com/Bugs5382/go-log"
	"github.com/go-logr/zerologr"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	rendersv1alpha1 "github.com/Steward-GRC/steward-pdf-renderer/api/v1alpha1"
	"github.com/Steward-GRC/steward-pdf-renderer/internal/config"
	"github.com/Steward-GRC/steward-pdf-renderer/internal/controller"
	"github.com/Steward-GRC/steward-pdf-renderer/internal/server"
)

const leaderElectionID = "steward-pdf-renderer.renders.steward-grc.com"

func main() {
	logger := log.NewLoggerWithOptions("steward-pdf-renderer-operator")
	if err := run(ctrl.SetupSignalHandler(), logger); err != nil {
		logger.Error(err, "operator stopped")
		os.Exit(1)
	}
}

func run(ctx context.Context, logger log.TraceLogger) error {
	info := buildinfo.Get()
	logger.Info("starting", log.F("version", info.Version), log.F("commit", info.Commit))

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}

	zl := log.New("steward-pdf-renderer-operator")
	ctrl.SetLogger(zerologr.New(&zl))

	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(rendersv1alpha1.AddToScheme(scheme))

	restCfg, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("kubernetes config: %w", err)
	}
	metricsAddr := "0"
	if cfg.MetricsPort != "0" {
		metricsAddr = ":" + cfg.MetricsPort
	}
	opts := ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: "0",
		LeaderElection:         cfg.LeaderElect,
		LeaderElectionID:       leaderElectionID,
	}
	if cfg.WatchNamespace != "" {
		opts.Cache = cache.Options{DefaultNamespaces: map[string]cache.Config{cfg.WatchNamespace: {}}}
	}
	mgr, err := ctrl.NewManager(restCfg, opts)
	if err != nil {
		return fmt.Errorf("manager: %w", err)
	}

	if err := (&controller.PdfRenderReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Config: cfg.Controller,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("reconciler: %w", err)
	}

	dc, err := discovery.NewDiscoveryClientForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("discovery client: %w", err)
	}
	checker := health.New(health.WithTTL(5*time.Second), health.WithTimeout(2*time.Second), health.WithLogger(logger))
	if err := checker.Register(server.KubernetesDependency(dc.RESTClient())); err != nil {
		return fmt.Errorf("health: %w", err)
	}
	probes, err := server.Probes(checker)
	if err != nil {
		return err
	}
	if err := mgr.Add(probeServer(":"+cfg.ProbePort, probes, logger)); err != nil {
		return fmt.Errorf("probe server: %w", err)
	}

	logger.Info("manager starting",
		log.F("renderer_image", cfg.Controller.RendererImage),
		log.F("watch_namespace", cfg.WatchNamespace),
		log.F("leader_elect", cfg.LeaderElect),
		log.F("probe_port", cfg.ProbePort),
		log.F("metrics_port", cfg.MetricsPort))
	if err := mgr.Start(ctx); err != nil {
		return fmt.Errorf("manager: %w", err)
	}
	logger.Info("stopped")
	return nil
}

// probeRunnable serves the probes on every replica, leader or not, for the
// manager's lifetime.
type probeRunnable struct {
	srv    *http.Server
	logger log.Logger
}

func probeServer(addr string, h http.Handler, logger log.Logger) manager.Runnable {
	return &probeRunnable{srv: &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}, logger: logger}
}

func (p *probeRunnable) NeedLeaderElection() bool { return false }

func (p *probeRunnable) Start(ctx context.Context) error {
	l, err := net.Listen("tcp", p.srv.Addr)
	if err != nil {
		return fmt.Errorf("probe listen: %w", err)
	}
	p.logger.Info("probes listening", log.F("addr", l.Addr().String()))
	errCh := make(chan error, 1)
	go func() { errCh <- p.srv.Serve(l) }()
	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		return p.srv.Shutdown(shutCtx)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("probe server: %w", err)
	}
}
