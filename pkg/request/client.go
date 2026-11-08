// Package request provides HTTP request building from OpenAPI operations.
package request

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nomagicln/open-bridge/pkg/config"
)

const (
	// defaultHTTPTimeout bounds requests that do not configure a profile timeout.
	defaultHTTPTimeout = 30 * time.Second

	// defaultRetryInitialDelay is the delay before the first retry.
	defaultRetryInitialDelay = 200 * time.Millisecond

	// defaultRetryMaxDelay caps the exponential retry backoff.
	defaultRetryMaxDelay = 2 * time.Second

	// retryBodyDrainLimit limits how much of a discarded response body is read
	// before closing it, so keep-alive connections can be reused.
	retryBodyDrainLimit = 4 << 10
)

// retryableMethods contains the HTTP methods that may be retried safely. Retries
// are limited to idempotent methods so a retry can never duplicate a write.
var retryableMethods = map[string]bool{
	http.MethodGet:     true,
	http.MethodHead:    true,
	http.MethodPut:     true,
	http.MethodDelete:  true,
	http.MethodOptions: true,
}

// NewHTTPClient builds an HTTP client from a profile's timeout and TLS settings.
// A nil profile or a zero profile timeout yields the defaultHTTPTimeout. TLS
// settings are layered on top of a clone of http.DefaultTransport. Invalid TLS
// configuration (unreadable/invalid CA or client certificate, unsupported
// minimum version, incomplete key pair) returns a wrapped error.
func NewHTTPClient(profile *config.Profile) (*http.Client, error) {
	timeout := defaultHTTPTimeout
	var tlsConfig config.TLSConfig
	if profile != nil {
		if profile.Timeout.Duration > 0 {
			timeout = profile.Timeout.Duration
		}
		tlsConfig = profile.TLSConfig
	}

	clientTLS, err := buildTLSConfig(tlsConfig)
	if err != nil {
		return nil, err
	}

	transport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		transport = &http.Transport{}
	}
	clonedTransport := transport.Clone()
	if clientTLS != nil {
		clonedTransport.TLSClientConfig = clientTLS
	}

	return &http.Client{Timeout: timeout, Transport: clonedTransport}, nil
}

// tlsConfigRequired reports whether the profile asks for any custom TLS behavior.
func tlsConfigRequired(cfg config.TLSConfig) bool {
	return cfg.InsecureSkipVerify ||
		cfg.CAFile != "" ||
		cfg.CertFile != "" ||
		cfg.KeyFile != "" ||
		cfg.ServerName != "" ||
		cfg.MinVersion != ""
}

// buildTLSConfig converts a config.TLSConfig into a *tls.Config. It returns nil
// when no TLS customization was requested.
func buildTLSConfig(cfg config.TLSConfig) (*tls.Config, error) {
	if !tlsConfigRequired(cfg) {
		return nil, nil
	}

	minVersion, err := parseTLSMinVersion(cfg.MinVersion)
	if err != nil {
		return nil, err
	}

	tlsConfig := &tls.Config{
		// InsecureSkipVerify is only set when the user explicitly opted in via
		// the profile's insecure_skip_verify setting; the default is full
		// certificate verification.
		InsecureSkipVerify: cfg.InsecureSkipVerify, //nolint:gosec // explicit user-configured profile setting
		MinVersion:         minVersion,
	}
	if cfg.ServerName != "" {
		tlsConfig.ServerName = cfg.ServerName
	}

	if cfg.CAFile != "" {
		certPool, err := loadCertPool(cfg.CAFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.RootCAs = certPool
	}

	clientCertificate, err := loadClientCertificate(cfg)
	if err != nil {
		return nil, err
	}
	if clientCertificate != nil {
		tlsConfig.Certificates = []tls.Certificate{*clientCertificate}
	}

	return tlsConfig, nil
}

// parseTLSMinVersion maps the profile's textual minimum TLS version to the
// crypto/tls constant. An empty value leaves the Go default in place.
func parseTLSMinVersion(version string) (uint16, error) {
	switch version {
	case "":
		return 0, nil
	case "1.2":
		return tls.VersionTLS12, nil
	case "1.3":
		return tls.VersionTLS13, nil
	default:
		return 0, fmt.Errorf("unsupported TLS min version %q: supported values are %q and %q", version, "1.2", "1.3")
	}
}

// loadCertPool reads a PEM CA bundle from disk.
func loadCertPool(caFile string) (*x509.CertPool, error) {
	caPEM, err := os.ReadFile(caFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read CA file %q: %w", caFile, err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("failed to parse CA file %q: no PEM certificates found", caFile)
	}
	return pool, nil
}

// loadClientCertificate loads the mTLS client key pair. It returns nil when no
// client certificate is configured, and an error when the pair is incomplete.
func loadClientCertificate(cfg config.TLSConfig) (*tls.Certificate, error) {
	if cfg.CertFile == "" && cfg.KeyFile == "" {
		return nil, nil
	}
	if cfg.CertFile == "" || cfg.KeyFile == "" {
		return nil, fmt.Errorf("both client cert and key files must be provided for mTLS (cert_file=%q, key_file=%q)", cfg.CertFile, cfg.KeyFile)
	}

	certificate, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate (cert_file=%q, key_file=%q): %w", cfg.CertFile, cfg.KeyFile, err)
	}
	return &certificate, nil
}

// DoWithRetry sends the request, retrying network errors and configured
// retryable status codes for idempotent methods. RetryConfig.MaxRetries is the
// number of retries after the initial attempt; MaxRetries <= 0 disables
// retrying entirely. The request body is buffered so it can be replayed.
//
// A cancelled request context aborts the retry: no further request is sent and
// the context error is returned.
func DoWithRetry(client *http.Client, req *http.Request, retry config.RetryConfig) (*http.Response, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if req == nil {
		return nil, errors.New("cannot retry a nil request")
	}
	if retry.MaxRetries <= 0 || !isRetryableMethod(req.Method) {
		return client.Do(req)
	}

	rewindBody, err := prepareBodyReplay(req)
	if err != nil {
		return nil, err
	}

	policy := newRetryPolicy(retry)
	for attempt := 0; ; attempt++ {
		resp, final, err := policy.attempt(client, req, rewindBody, attempt)
		if err != nil {
			return nil, err
		}
		if final {
			return resp, nil
		}
	}
}

// retryPolicy holds the resolved retry settings of a request.
type retryPolicy struct {
	maxRetries      int
	retryableStatus map[int]bool
	initialDelay    time.Duration
	maxDelay        time.Duration
}

// newRetryPolicy resolves the retry settings of a configuration.
func newRetryPolicy(retry config.RetryConfig) retryPolicy {
	statuses := make(map[int]bool, len(retry.RetryableStatusCodes))
	for _, statusCode := range retry.RetryableStatusCodes {
		statuses[statusCode] = true
	}

	initialDelay, maxDelay := retryDelays(retry)

	return retryPolicy{
		maxRetries:      retry.MaxRetries,
		retryableStatus: statuses,
		initialDelay:    initialDelay,
		maxDelay:        maxDelay,
	}
}

// attempt performs a single request attempt.
//
// It returns the response when the attempt is final. When another attempt is
// needed, it waits for the backoff first and reports the attempt as not final.
func (p *retryPolicy) attempt(
	client *http.Client,
	req *http.Request,
	rewindBody func() error,
	attempt int,
) (*http.Response, bool, error) {
	if attempt > 0 {
		if err := rewindBody(); err != nil {
			return nil, false, err
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		if attempt >= p.maxRetries || !isRetryableNetworkError(req, err) {
			return nil, false, err
		}
		if err := p.waitBeforeRetry(req, attempt); err != nil {
			return nil, false, err
		}
		return nil, false, nil
	}

	if attempt >= p.maxRetries || !p.retryableStatus[resp.StatusCode] {
		return resp, true, nil
	}

	drainAndCloseBody(resp.Body)
	if err := p.waitBeforeRetry(req, attempt); err != nil {
		return nil, false, err
	}
	return nil, false, nil
}

// waitBeforeRetry waits for the backoff before the next attempt. It returns the
// context error when the request context is cancelled while waiting.
func (p *retryPolicy) waitBeforeRetry(req *http.Request, attempt int) error {
	return waitForRetry(req.Context(), retryBackoff(attempt, p.initialDelay, p.maxDelay))
}

// waitForRetry waits for the given delay before the next attempt. It returns
// the context error when the request context is cancelled while waiting, so a
// cancelled request never sleeps through the whole backoff.
func waitForRetry(ctx context.Context, delay time.Duration) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// retryDelays resolves the effective backoff bounds for a retry configuration.
func retryDelays(retry config.RetryConfig) (initial, maxDelay time.Duration) {
	initial = retry.InitialDelay.Duration
	if initial <= 0 {
		initial = defaultRetryInitialDelay
	}
	maxDelay = retry.MaxDelay.Duration
	if maxDelay <= 0 {
		maxDelay = defaultRetryMaxDelay
	}
	return initial, maxDelay
}

// isRetryableMethod reports whether requests with this method can be retried.
func isRetryableMethod(method string) bool {
	return retryableMethods[strings.ToUpper(method)]
}

// isRetryableNetworkError reports whether a transport error is retryable.
// Caller-cancelled requests are never retried.
func isRetryableNetworkError(req *http.Request, err error) bool {
	if req.Context() != nil && req.Context().Err() != nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return false
	}
	return true
}

// prepareBodyReplay returns a function that resets the request body for the next
// attempt. Existing GetBody functions are reused; otherwise the body is buffered
// into memory once.
func prepareBodyReplay(req *http.Request) (func() error, error) {
	if req.Body == nil {
		return func() error { return nil }, nil
	}
	if req.GetBody != nil {
		return func() error {
			body, err := req.GetBody()
			if err != nil {
				return fmt.Errorf("failed to rewind request body for retry: %w", err)
			}
			req.Body = body
			return nil
		}, nil
	}

	bodyBytes, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to buffer request body for retry: %w", err)
	}
	_ = req.Body.Close()
	req.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	return func() error {
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		return nil
	}, nil
}

// retryBackoff returns the delay before the given retry attempt (0-based),
// doubling from initial and capped at maxDelay.
func retryBackoff(attempt int, initial, maxDelay time.Duration) time.Duration {
	delay := initial
	for range attempt {
		if delay >= maxDelay {
			return maxDelay
		}
		delay *= 2
	}
	return min(delay, maxDelay)
}

// drainAndCloseBody discards a bounded amount of a response body before closing
// it so the underlying connection can be reused.
func drainAndCloseBody(body io.ReadCloser) {
	if body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(body, retryBodyDrainLimit))
	_ = body.Close()
}
