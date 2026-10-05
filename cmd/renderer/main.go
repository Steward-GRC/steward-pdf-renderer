// Copyright 2026 The Steward Authors
// SPDX-License-Identifier: Apache-2.0

// Command renderer is the one-shot PDF render the operator's Job runs. It
// reads the render from its environment, fetches the HTML with its workload
// token, prints it with
// headless Chromium (watermarked when sensitive), uploads the PDF to object
// storage and exits. Any failure exits non-zero; the Job's backoff retries.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	buildinfo "github.com/Bugs5382/go-buildinfo"
	log "github.com/Bugs5382/go-log"
	"github.com/Bugs5382/go-objectstore"
	"github.com/Bugs5382/go-objectstore/s3store"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"

	"github.com/Steward-GRC/steward-pdf-renderer/internal/render"
)

const (
	defaultHTTPTimeout   = 30 * time.Second
	defaultRenderTimeout = 120 * time.Second
)

func main() {
	logger := log.NewLoggerWithOptions("steward-pdf-renderer")
	if err := run(context.Background(), logger); err != nil {
		logger.Error(err, "render failed")
		os.Exit(1)
	}
}

// runConfig is the render the operator put in the Job's environment.
type runConfig struct {
	policyVersionID string
	fetchURL        string
	outputBucket    string
	outputKey       string
	sensitivity     string
	requestedBy     string
	traceID         string

	// Object storage, from the Secret the operator names.
	s3Endpoint       string
	s3Region         string
	s3AccessKey      string
	s3SecretKey      string
	s3ForcePathStyle bool

	// tokenFile is the Job's projected service-account token, sent on the
	// HTML fetch. Empty only with WORKLOAD_AUTH=disabled.
	tokenFile string

	httpTimeout   time.Duration
	renderTimeout time.Duration
}

// Workload-token settings, the names every Steward service uses.
const (
	envTokenFile = "WORKLOAD_TOKEN_FILE" // #nosec G101 -- an environment variable name, not a credential
	envAuthMode  = "WORKLOAD_AUTH"
	authDisabled = "disabled"
)

// loadConfig reads runConfig from the environment. A missing required value
// is an error, so the Job fails instead of uploading nothing.
func loadConfig() (runConfig, error) {
	c := runConfig{
		policyVersionID: os.Getenv("PV_ID"),
		fetchURL:        os.Getenv("FETCH_URL"),
		outputBucket:    os.Getenv("OUTPUT_BUCKET"),
		outputKey:       os.Getenv("OUTPUT_KEY"),
		sensitivity:     os.Getenv("SENSITIVITY"),
		requestedBy:     os.Getenv("REQUESTED_BY"),
		traceID:         os.Getenv("TRACE_ID"),

		s3Endpoint:       os.Getenv("AWS_S3_ENDPOINT"),
		s3Region:         envOr("AWS_REGION", "us-east-1"),
		s3AccessKey:      os.Getenv("AWS_ACCESS_KEY_ID"),
		s3SecretKey:      os.Getenv("AWS_SECRET_ACCESS_KEY"),
		s3ForcePathStyle: strings.EqualFold(os.Getenv("AWS_S3_FORCE_PATH_STYLE"), "true"),

		tokenFile: strings.TrimSpace(os.Getenv(envTokenFile)),

		httpTimeout:   defaultHTTPTimeout,
		renderTimeout: defaultRenderTimeout,
	}

	var errs []error
	switch mode := os.Getenv(envAuthMode); {
	case mode != "" && mode != authDisabled:
		errs = append(errs, fmt.Errorf("%s=%q: the only accepted value is %q", envAuthMode, mode, authDisabled))
	case c.tokenFile == "" && mode != authDisabled:
		errs = append(errs, fmt.Errorf("%s is required to fetch the HTML; set %s=%s for local runs only", envTokenFile, envAuthMode, authDisabled))
	}

	var missing []string
	if c.fetchURL == "" {
		missing = append(missing, "FETCH_URL")
	}
	if c.outputBucket == "" {
		missing = append(missing, "OUTPUT_BUCKET")
	}
	if c.outputKey == "" {
		missing = append(missing, "OUTPUT_KEY")
	}
	if len(missing) > 0 {
		errs = append(errs, fmt.Errorf("missing required env: %s", strings.Join(missing, ", ")))
	}
	return c, errors.Join(errs...)
}

func envOr(key, dflt string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return dflt
}

func run(ctx context.Context, logger log.Logger) error {
	info := buildinfo.Get()
	logger.Info("starting", log.F("version", info.Version), log.F("commit", info.Commit))

	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	logger = logger.With(log.F("policy_version_id", cfg.policyVersionID), log.F("trace_id", cfg.traceID))

	sc := storeConfig(cfg)
	if cfg.s3AccessKey == "" && cfg.s3SecretKey == "" {
		// No static keys: use the SDK's default chain (workload identity,
		// instance roles).
		aws, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.s3Region))
		if err != nil {
			return fmt.Errorf("object storage credentials: %w", err)
		}
		sc.Credentials = aws.Credentials
	}
	store, err := s3store.New(sc)
	if err != nil {
		return fmt.Errorf("object storage: %w", err)
	}
	return renderPDF(ctx, cfg, render.ChromedpRenderer(), store, logger)
}

func storeConfig(c runConfig) s3store.Config {
	return s3store.Config{
		Endpoint:        c.s3Endpoint,
		Region:          c.s3Region,
		Bucket:          c.outputBucket,
		AccessKeyID:     c.s3AccessKey,
		SecretAccessKey: c.s3SecretKey,
		PathStyle:       c.s3ForcePathStyle,
	}
}

// renderPDF fetches, renders and uploads one PDF.
func renderPDF(ctx context.Context, cfg runConfig, r render.Renderer, store objectstore.Store, logger log.Logger) error {
	logger.Info("fetching html", log.F("workload_token", cfg.tokenFile != ""))
	start := time.Now()
	htmlContent, err := fetchHTMLAs(ctx, cfg.fetchURL, cfg.httpTimeout, cfg.tokenFile)
	if err != nil {
		return fmt.Errorf("fetch html: %w", err)
	}
	logger.Info("fetched html", log.F("html_bytes", len(htmlContent)), log.F("duration_ms", time.Since(start).Milliseconds()))

	watermarkCSS := ""
	if strings.EqualFold(cfg.sensitivity, "sensitive") {
		watermarkCSS = render.WatermarkCSS(render.WatermarkParams{
			SensitivityLabel:  "CONFIDENTIAL",
			RecipientIdentity: cfg.requestedBy,
			Timestamp:         time.Now().UTC(),
		})
	}
	logger.Debug("rendering", log.F("watermark", watermarkCSS != ""))

	renderCtx, cancel := context.WithTimeout(ctx, cfg.renderTimeout)
	defer cancel()
	start = time.Now()
	pdfBytes, err := r.Render(renderCtx, htmlContent, watermarkCSS)
	if err != nil {
		return fmt.Errorf("render pdf: %w", err)
	}
	logger.Info("rendered pdf", log.F("pdf_bytes", len(pdfBytes)), log.F("duration_ms", time.Since(start).Milliseconds()))

	start = time.Now()
	if _, err := store.Put(ctx, cfg.outputKey, bytes.NewReader(pdfBytes), objectstore.PutOptions{
		ContentType: "application/pdf",
		Size:        int64(len(pdfBytes)),
	}); err != nil {
		return fmt.Errorf("upload pdf: %w", err)
	}
	logger.Info("uploaded pdf", log.F("bucket", cfg.outputBucket), log.F("key", cfg.outputKey), log.F("duration_ms", time.Since(start).Milliseconds()))
	return nil
}

// fetchHTML GETs the URL and returns the response body. Non-2xx is an error;
// the Job's backoff retries. The body is capped at 8 MiB so a runaway page
// can't exhaust the pod's memory.
func fetchHTML(ctx context.Context, url string, timeout time.Duration) (string, error) {
	return fetchHTMLAs(ctx, url, timeout, "")
}

// fetchHTMLAs is fetchHTML sending "Authorization: Bearer <token>" read from
// tokenFile, which is read on every fetch so a rotated token is picked up. A
// file that can't be read, or is empty, fails the fetch before any request
// goes out. An empty tokenFile sends no token.
func fetchHTMLAs(ctx context.Context, url string, timeout time.Duration, tokenFile string) (string, error) {
	if url == "" {
		return "", errors.New("empty fetch url")
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	if tokenFile != "" {
		tok, err := readToken(tokenFile)
		if err != nil {
			return "", err
		}
		req.Header.Set("Authorization", "Bearer "+tok)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http get: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("fetch returned status %d", resp.StatusCode)
	}

	const maxBytes = 8 << 20 // 8 MiB
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}
	if len(body) > maxBytes {
		return "", fmt.Errorf("fetch body exceeds %d bytes", maxBytes)
	}
	return string(body), nil
}

func readToken(path string) (string, error) {
	b, err := os.ReadFile(path) // #nosec G304 -- operator-configured path
	if err != nil {
		return "", fmt.Errorf("read %s: %w", envTokenFile, err)
	}
	tok := strings.TrimSpace(string(b))
	if tok == "" {
		return "", fmt.Errorf("%s %q is empty", envTokenFile, path)
	}
	return tok, nil
}
