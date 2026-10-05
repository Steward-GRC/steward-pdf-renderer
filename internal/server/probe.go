// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Package server serves the operator's health probes.
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Bugs5382/go-buildinfo/health"
	"github.com/Bugs5382/go-buildinfo/httpbuildinfo"
	"k8s.io/client-go/rest"
)

// HeaderPrefix is the prefix of the build and dependency headers.
const HeaderPrefix = "steward"

// KubernetesDependency is the API server, required: the operator can't
// reconcile without it. The check and the version both read /version.
func KubernetesDependency(c rest.Interface) health.Dependency {
	return health.Dependency{
		Name:     "kubernetes",
		Required: true,
		Check: func(ctx context.Context) error {
			_, err := serverVersion(ctx, c)
			return err
		},
		Version: func(ctx context.Context) (string, error) {
			return serverVersion(ctx, c)
		},
	}
}

func serverVersion(ctx context.Context, c rest.Interface) (string, error) {
	raw, err := c.Get().AbsPath("/version").Do(ctx).Raw()
	if err != nil {
		return "", fmt.Errorf("kubernetes version: %w", err)
	}
	var v struct {
		GitVersion string `json:"gitVersion"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return "", fmt.Errorf("kubernetes version: %w", err)
	}
	return v.GitVersion, nil
}

// Probes serves /livez (the process only) and /readyz (the checker's
// dependencies), both with the build headers.
func Probes(checker *health.Checker) (http.Handler, error) {
	h, err := httpbuildinfo.New(httpbuildinfo.WithPrefix(HeaderPrefix), httpbuildinfo.WithChecker(checker))
	if err != nil {
		return nil, fmt.Errorf("probes: %w", err)
	}
	mux := http.NewServeMux()
	mux.Handle("GET /livez", h.Livez())
	mux.Handle("GET /readyz", h.Readyz())
	return mux, nil
}
