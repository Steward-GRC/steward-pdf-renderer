// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

package config_test

import (
	"strings"
	"testing"

	"github.com/Steward-GRC/steward-pdf-renderer/internal/config"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	t.Parallel()
	c, err := config.Load(env(map[string]string{"RENDERER_IMAGE": "registry.example.org/steward-pdf-renderer/renderer:v0.1.0"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Controller.RendererImage != "registry.example.org/steward-pdf-renderer/renderer:v0.1.0" {
		t.Errorf("RendererImage = %q", c.Controller.RendererImage)
	}
	if c.Controller.S3SecretName != "steward-pdf-renderer-s3" || c.Controller.JobBackoffLimit != 3 || c.Controller.JobTTLSecondsAfterFinished != 600 {
		t.Errorf("controller defaults = %+v", c.Controller)
	}
	if c.ProbePort != "8080" || c.MetricsPort != "9090" || !c.LeaderElect || c.WatchNamespace != "" {
		t.Errorf("defaults = %+v", c)
	}
}

func TestLoadOverrides(t *testing.T) {
	t.Parallel()
	c, err := config.Load(env(map[string]string{
		"RENDERER_IMAGE":    "img",
		"S3_SECRET_NAME":    "render-store",
		"JOB_BACKOFF_LIMIT": "0",
		"JOB_TTL_SECONDS":   "60",
		"PROBE_PORT":        "8081",
		"METRICS_PORT":      "0",
		"LEADER_ELECT":      "false",
		"WATCH_NAMESPACE":   "steward",
	}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Controller.S3SecretName != "render-store" || c.Controller.JobBackoffLimit != 0 || c.Controller.JobTTLSecondsAfterFinished != 60 {
		t.Errorf("controller = %+v", c.Controller)
	}
	if c.ProbePort != "8081" || c.MetricsPort != "0" || c.LeaderElect || c.WatchNamespace != "steward" {
		t.Errorf("config = %+v", c)
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	t.Parallel()
	_, err := config.Load(env(map[string]string{
		"JOB_BACKOFF_LIMIT": "three",
		"JOB_TTL_SECONDS":   "-1",
		"LEADER_ELECT":      "maybe",
	}))
	if err == nil {
		t.Fatal("Load accepted bad values")
	}
	for _, want := range []string{"RENDERER_IMAGE", "JOB_BACKOFF_LIMIT", "JOB_TTL_SECONDS", "LEADER_ELECT"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q doesn't name %s", err, want)
		}
	}
}

func TestLoadRejectsOutOfRange(t *testing.T) {
	t.Parallel()
	_, err := config.Load(env(map[string]string{"RENDERER_IMAGE": "img", "JOB_TTL_SECONDS": "4294967296"}))
	if err == nil || !strings.Contains(err.Error(), "JOB_TTL_SECONDS") {
		t.Errorf("err = %v, want JOB_TTL_SECONDS rejected", err)
	}
}

func TestLoadJobServiceAccount(t *testing.T) {
	c, err := config.Load(env(map[string]string{"RENDERER_IMAGE": "registry.example.org/renderer:v1"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Controller.JobServiceAccount != "steward-pdf-renderer" {
		t.Errorf("default JobServiceAccount = %q", c.Controller.JobServiceAccount)
	}
	c, err = config.Load(env(map[string]string{"RENDERER_IMAGE": "registry.example.org/renderer:v1", "JOB_SERVICE_ACCOUNT": "renderer-job"}))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Controller.JobServiceAccount != "renderer-job" {
		t.Errorf("JobServiceAccount = %q", c.Controller.JobServiceAccount)
	}
}
