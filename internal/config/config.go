// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Package config reads the operator's settings from the environment.
package config

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/Steward-GRC/steward-pdf-renderer/internal/controller"
)

// Config is every setting the operator runs with.
type Config struct {
	Controller controller.Config
	// ProbePort serves /livez and /readyz over plain HTTP.
	ProbePort string
	// MetricsPort serves the controller metrics; "0" turns them off.
	MetricsPort string
	LeaderElect bool
	// WatchNamespace limits the operator to one namespace; empty watches
	// every namespace and needs cluster-wide permissions.
	WatchNamespace string
}

// Load reads the settings through getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	or := func(k, d string) string {
		if v := getenv(k); v != "" {
			return v
		}
		return d
	}
	c := Config{
		Controller: controller.Config{
			RendererImage:     getenv("RENDERER_IMAGE"),
			S3SecretName:      or("S3_SECRET_NAME", "steward-pdf-renderer-s3"),
			JobServiceAccount: or("JOB_SERVICE_ACCOUNT", controller.DefaultJobServiceAccount),
		},
		ProbePort:      or("PROBE_PORT", "8080"),
		MetricsPort:    or("METRICS_PORT", "9090"),
		WatchNamespace: getenv("WATCH_NAMESPACE"),
	}

	var errs []error
	if c.Controller.RendererImage == "" {
		errs = append(errs, errors.New("RENDERER_IMAGE is required"))
	}
	nonNegative := func(key, def string) int32 {
		n, err := strconv.ParseInt(or(key, def), 10, 32)
		if err != nil || n < 0 {
			errs = append(errs, fmt.Errorf("%s must be a whole number, 0 or more", key))
			return 0
		}
		return int32(n)
	}
	c.Controller.JobBackoffLimit = nonNegative("JOB_BACKOFF_LIMIT", "3")
	c.Controller.JobTTLSecondsAfterFinished = nonNegative("JOB_TTL_SECONDS", "600")
	le, err := strconv.ParseBool(or("LEADER_ELECT", "true"))
	if err != nil {
		errs = append(errs, errors.New("LEADER_ELECT must be true or false"))
	}
	c.LeaderElect = le
	return c, errors.Join(errs...)
}
