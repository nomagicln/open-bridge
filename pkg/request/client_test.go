package request

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"io/fs"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nomagicln/open-bridge/pkg/config"
)

// ---------- NewHTTPClient ----------

func TestNewHTTPClientDefaults(t *testing.T) {
	t.Run("nil profile", func(t *testing.T) {
		client, err := NewHTTPClient(nil)
		if err != nil {
			t.Fatalf("NewHTTPClient(nil) error: %v", err)
		}
		if got, want := client.Timeout, defaultHTTPTimeout; got != want {
			t.Errorf("Timeout = %v, want %v", got, want)
		}
		transport, ok := client.Transport.(*http.Transport)
		if !ok {
			t.Fatalf("Transport = %T, want *http.Transport", client.Transport)
		}
		if transport == http.DefaultTransport {
			t.Error("Transport must be a clone of http.DefaultTransport, not the shared instance")
		}
	})

	t.Run("zero profile uses default timeout", func(t *testing.T) {
		client, err := NewHTTPClient(&config.Profile{Name: "default"})
		if err != nil {
			t.Fatalf("NewHTTPClient() error: %v", err)
		}
		if got, want := client.Timeout, defaultHTTPTimeout; got != want {
			t.Errorf("Timeout = %v, want %v", got, want)
		}
	})

	t.Run("profile timeout overrides default", func(t *testing.T) {
		client, err := NewHTTPClient(&config.Profile{
			Timeout: config.Duration{Duration: 5 * time.Second},
		})
		if err != nil {
			t.Fatalf("NewHTTPClient() error: %v", err)
		}
		if got, want := client.Timeout, 5*time.Second; got != want {
			t.Errorf("Timeout = %v, want %v", got, want)
		}
	})
}

func TestNewHTTPClientInvalidTLSConfig(t *testing.T) {
	tests := []struct {
		name      string
		tlsConfig config.TLSConfig
		wantErr   string
		wantIs    error
	}{
		{
			name:      "unsupported min version",
			tlsConfig: config.TLSConfig{MinVersion: "1.0"},
			wantErr:   "unsupported TLS min version",
		},
		{
			name:      "missing CA file",
			tlsConfig: config.TLSConfig{CAFile: filepath.Join(t.TempDir(), "missing-ca.pem")},
			wantErr:   "failed to read CA file",
			wantIs:    fs.ErrNotExist,
		},
		{
			name: "invalid CA content",
			tlsConfig: config.TLSConfig{
				CAFile: writeTempPEM(t, "invalid-ca.pem", &pem.Block{Type: "CERTIFICATE", Bytes: []byte("not a certificate")}),
			},
			wantErr: "failed to parse CA file",
		},
		{
			name:      "client cert without key",
			tlsConfig: config.TLSConfig{CertFile: filepath.Join(t.TempDir(), "client.pem")},
			wantErr:   "both client cert and key files must be provided",
		},
		{
			name: "unparsable client key pair",
			tlsConfig: config.TLSConfig{
				CertFile: writeTempPEM(t, "client.pem", &pem.Block{Type: "CERTIFICATE", Bytes: []byte("nope")}),
				KeyFile:  writeTempPEM(t, "client.key", &pem.Block{Type: "PRIVATE KEY", Bytes: []byte("nope")}),
			},
			wantErr: "failed to load client certificate",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewHTTPClient(&config.Profile{TLSConfig: tt.tlsConfig})
			if err == nil {
				t.Fatalf("NewHTTPClient() expected error containing %q, got nil", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErr)
			}
			if tt.wantIs != nil && !errors.Is(err, tt.wantIs) {
				t.Errorf("error = %v, want it to wrap %v", err, tt.wantIs)
			}
		})
	}
}

func TestNewHTTPClientCAFile(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(okHandler))
	t.Cleanup(server.Close)

	caPath := writeTempPEM(t, "ca.pem", &pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	client, err := NewHTTPClient(&config.Profile{TLSConfig: config.TLSConfig{CAFile: caPath}})
	if err != nil {
		t.Fatalf("NewHTTPClient() error: %v", err)
	}

	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("client.Get() with CAFile failed: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestNewHTTPClientInsecureSkipVerify(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(okHandler))
	t.Cleanup(server.Close)

	t.Run("verification on by default and fails for self-signed", func(t *testing.T) {
		client, err := NewHTTPClient(&config.Profile{})
		if err != nil {
			t.Fatalf("NewHTTPClient() error: %v", err)
		}
		assertGetFails(t, client, server.URL, "client.Get() expected certificate error for self-signed server, got nil")
	})

	t.Run("insecure skip verify accepts self-signed", func(t *testing.T) {
		client, err := NewHTTPClient(&config.Profile{
			TLSConfig: config.TLSConfig{InsecureSkipVerify: true},
		})
		if err != nil {
			t.Fatalf("NewHTTPClient() error: %v", err)
		}
		resp, err := client.Get(server.URL)
		if err != nil {
			t.Fatalf("client.Get() with InsecureSkipVerify failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})
}

func TestNewHTTPClientWrongCA(t *testing.T) {
	ca := newTestCA(t)
	serverCert, _, _ := ca.issue(t, "localhost", nil, []net.IP{net.ParseIP("127.0.0.1")}, x509.ExtKeyUsageServerAuth)
	server := startTLSServer(t, &tls.Config{Certificates: []tls.Certificate{serverCert}})

	// Trusting a different CA must not validate this server's certificate.
	otherCA := newTestCA(t)
	client, err := NewHTTPClient(&config.Profile{TLSConfig: config.TLSConfig{CAFile: otherCA.caPath}})
	if err != nil {
		t.Fatalf("NewHTTPClient() error: %v", err)
	}
	assertGetFails(t, client, server.URL, "client.Get() expected unknown authority error, got nil")
}

func TestNewHTTPClientServerName(t *testing.T) {
	ca := newTestCA(t)
	serverCert, _, _ := ca.issue(t, "svc.openbridge.test", []string{"svc.openbridge.test"}, nil, x509.ExtKeyUsageServerAuth)
	server := startTLSServer(t, &tls.Config{Certificates: []tls.Certificate{serverCert}})

	t.Run("server name overrides url host", func(t *testing.T) {
		client, err := NewHTTPClient(&config.Profile{TLSConfig: config.TLSConfig{
			CAFile:     ca.caPath,
			ServerName: "svc.openbridge.test",
		}})
		if err != nil {
			t.Fatalf("NewHTTPClient() error: %v", err)
		}
		resp, err := client.Get(server.URL)
		if err != nil {
			t.Fatalf("client.Get() with ServerName failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
	})

	t.Run("missing server name fails hostname verification", func(t *testing.T) {
		client, err := NewHTTPClient(&config.Profile{TLSConfig: config.TLSConfig{CAFile: ca.caPath}})
		if err != nil {
			t.Fatalf("NewHTTPClient() error: %v", err)
		}
		assertGetFails(t, client, server.URL, "client.Get() expected hostname verification error, got nil")
	})
}

func TestNewHTTPClientClientCertificate(t *testing.T) {
	ca := newTestCA(t)
	serverCert, _, _ := ca.issue(t, "localhost", nil, []net.IP{net.ParseIP("127.0.0.1")}, x509.ExtKeyUsageServerAuth)
	clientCert, clientCertPath, clientKeyPath := ca.issue(t, "cli-client", nil, nil, x509.ExtKeyUsageClientAuth)

	server := startTLSServer(t, &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    ca.pool,
	})

	t.Run("mTLS with client certificate succeeds", func(t *testing.T) {
		client, err := NewHTTPClient(&config.Profile{TLSConfig: config.TLSConfig{
			CAFile:   ca.caPath,
			CertFile: clientCertPath,
			KeyFile:  clientKeyPath,
		}})
		if err != nil {
			t.Fatalf("NewHTTPClient() error: %v", err)
		}
		resp, err := client.Get(server.URL)
		if err != nil {
			t.Fatalf("client.Get() with client cert failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
		}
		_ = clientCert
	})

	t.Run("missing client certificate is rejected", func(t *testing.T) {
		client, err := NewHTTPClient(&config.Profile{TLSConfig: config.TLSConfig{CAFile: ca.caPath}})
		if err != nil {
			t.Fatalf("NewHTTPClient() error: %v", err)
		}
		assertGetFails(t, client, server.URL, "client.Get() expected TLS handshake error without client cert, got nil")
	})
}

func TestNewHTTPClientMinVersion(t *testing.T) {
	ca := newTestCA(t)
	serverCert, _, _ := ca.issue(t, "localhost", nil, []net.IP{net.ParseIP("127.0.0.1")}, x509.ExtKeyUsageServerAuth)
	server := startTLSServer(t, &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		MaxVersion:   tls.VersionTLS12,
	})

	t.Run("TLS 1.2 client works", func(t *testing.T) {
		client, err := NewHTTPClient(&config.Profile{TLSConfig: config.TLSConfig{
			CAFile:     ca.caPath,
			MinVersion: "1.2",
		}})
		if err != nil {
			t.Fatalf("NewHTTPClient() error: %v", err)
		}
		resp, err := client.Get(server.URL)
		if err != nil {
			t.Fatalf("client.Get() with MinVersion 1.2 failed: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
	})

	t.Run("TLS 1.3 client fails against TLS 1.2 server", func(t *testing.T) {
		client, err := NewHTTPClient(&config.Profile{TLSConfig: config.TLSConfig{
			CAFile:     ca.caPath,
			MinVersion: "1.3",
		}})
		if err != nil {
			t.Fatalf("NewHTTPClient() error: %v", err)
		}
		resp, err := client.Get(server.URL)
		if err == nil {
			_ = resp.Body.Close()
			t.Fatal("client.Get() expected protocol version error, got nil")
		}
	})
}

func TestNewHTTPClientTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		_, _ = w.Write([]byte("slow"))
	}))
	t.Cleanup(server.Close)

	client, err := NewHTTPClient(&config.Profile{Timeout: config.Duration{Duration: 50 * time.Millisecond}})
	if err != nil {
		t.Fatalf("NewHTTPClient() error: %v", err)
	}
	start := time.Now()
	resp, err := client.Get(server.URL)
	elapsed := time.Since(start)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("client.Get() expected timeout error, got nil")
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Errorf("client.Get() error = %v, want a net.Error timeout", err)
	}
	if elapsed > time.Second {
		t.Errorf("client.Get() took %v, timeout was not applied", elapsed)
	}
}

// ---------- DoWithRetry ----------

func TestRetryBackoff(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{attempt: 0, want: defaultRetryInitialDelay},
		{attempt: 1, want: 400 * time.Millisecond},
		{attempt: 2, want: 800 * time.Millisecond},
		{attempt: 3, want: 1600 * time.Millisecond},
		{attempt: 4, want: defaultRetryMaxDelay},
		{attempt: 5, want: defaultRetryMaxDelay},
	}
	for _, tt := range tests {
		if got := retryBackoff(tt.attempt, defaultRetryInitialDelay, defaultRetryMaxDelay); got != tt.want {
			t.Errorf("retryBackoff(%d) = %v, want %v", tt.attempt, got, tt.want)
		}
	}
}

func TestDoWithRetryRetryableStatus(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n := attempts.Add(1); n <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("retry me"))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	t.Cleanup(server.Close)

	req, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("http.NewRequest() error: %v", err)
	}
	resp, err := DoWithRetry(http.DefaultClient, req, fastRetryConfig(3, http.StatusServiceUnavailable))
	if err != nil {
		t.Fatalf("DoWithRetry() error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "ok" {
		t.Errorf("body = %q, want ok", body)
	}
}

func TestDoWithRetryStopsOnClientError(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(server.Close)

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := DoWithRetry(http.DefaultClient, req, fastRetryConfig(3, http.StatusServiceUnavailable))
	if err != nil {
		t.Fatalf("DoWithRetry() error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 (4xx must not be retried)", got)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestDoWithRetryMaxRetriesZero(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
	resp, err := DoWithRetry(http.DefaultClient, req, config.RetryConfig{})
	if err != nil {
		t.Fatalf("DoWithRetry() error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 (MaxRetries=0 disables retries)", got)
	}
}

func TestDoWithRetryNetworkError(t *testing.T) {
	transport := &flakyTransport{failures: 2}
	client := &http.Client{Transport: transport}
	req, _ := http.NewRequest(http.MethodGet, "http://example.invalid/pets", nil)

	resp, err := DoWithRetry(client, req, fastRetryConfig(2))
	if err != nil {
		t.Fatalf("DoWithRetry() error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := transport.attempts; got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestDoWithRetryContextCanceled(t *testing.T) {
	transport := &errorTransport{err: context.Canceled}
	client := &http.Client{Transport: transport}
	req, _ := http.NewRequest(http.MethodGet, "http://example.invalid/pets", nil)

	_, err := DoWithRetry(client, req, fastRetryConfig(3))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("DoWithRetry() error = %v, want context.Canceled", err)
	}
	if got := transport.attempts; got != 1 {
		t.Errorf("attempts = %d, want 1 (context cancellation must not be retried)", got)
	}
}

func TestDoWithRetryReplaysRequestBody(t *testing.T) {
	var attempts atomic.Int32
	bodies := make([]string, 0, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		if attempts.Add(1) == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	// Wrap the body so http.NewRequest cannot derive a GetBody automatically;
	// DoWithRetry must buffer and replay it.
	payload := `{"name":"fluffy"}`
	req, err := http.NewRequest(http.MethodPut, server.URL, io.NopCloser(strings.NewReader(payload)))
	if err != nil {
		t.Fatalf("http.NewRequest() error: %v", err)
	}
	resp, err := DoWithRetry(http.DefaultClient, req, fastRetryConfig(2, http.StatusServiceUnavailable))
	if err != nil {
		t.Fatalf("DoWithRetry() error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := attempts.Load(); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
	if len(bodies) != 2 {
		t.Fatalf("server observed %d bodies, want 2", len(bodies))
	}
	for i, body := range bodies {
		if body != payload {
			t.Errorf("body[%d] = %q, want %q (request body must be replayed)", i, body, payload)
		}
	}
}

func TestDoWithRetryNonIdempotentMethod(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	req, _ := http.NewRequest(http.MethodPost, server.URL, strings.NewReader(`{}`))
	resp, err := DoWithRetry(http.DefaultClient, req, fastRetryConfig(3, http.StatusServiceUnavailable))
	if err != nil {
		t.Fatalf("DoWithRetry() error: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1 (POST must not be retried)", got)
	}
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusServiceUnavailable)
	}
}

// ---------- helpers ----------

var okHandler = func(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func fastRetryConfig(maxRetries int, codes ...int) config.RetryConfig {
	return config.RetryConfig{
		MaxRetries:           maxRetries,
		InitialDelay:         config.Duration{Duration: time.Millisecond},
		MaxDelay:             config.Duration{Duration: 2 * time.Millisecond},
		RetryableStatusCodes: codes,
	}
}

func writeTempPEM(t *testing.T, name string, block *pem.Block) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func startTLSServer(t *testing.T, tlsConfig *tls.Config) *httptest.Server {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(okHandler))
	server.TLS = tlsConfig
	server.StartTLS()
	t.Cleanup(server.Close)
	return server
}

// testCA is a minimal certificate authority used to issue server/client
// certificates for the TLS tests.
type testCA struct {
	cert   *x509.Certificate
	key    *ecdsa.PrivateKey
	caPath string
	pool   *x509.CertPool
}

func newTestCA(t *testing.T) *testCA {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "open-bridge-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA certificate: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA certificate: %v", err)
	}

	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, caPEM, 0o600); err != nil {
		t.Fatalf("write CA file: %v", err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)

	return &testCA{cert: cert, key: key, caPath: caPath, pool: pool}
}

// issue creates a leaf certificate signed by the CA and writes it (with its
// key) to temporary files. It returns the tls.Certificate plus file paths.
func (ca *testCA) issue(t *testing.T, commonName string, dnsNames []string, ips []net.IP, usage x509.ExtKeyUsage) (tls.Certificate, string, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: commonName},
		DNSNames:     dnsNames,
		IPAddresses:  ips,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatalf("create leaf certificate: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal leaf key: %v", err)
	}

	dir := t.TempDir()
	certPath := filepath.Join(dir, commonName+"-cert.pem")
	keyPath := filepath.Join(dir, commonName+"-key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatalf("write leaf cert: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatalf("write leaf key: %v", err)
	}

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, certPath, keyPath
}

// assertGetFails asserts that the client cannot complete a GET to url.
func assertGetFails(t *testing.T, client *http.Client, url, message string) {
	t.Helper()
	resp, err := client.Get(url)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal(message)
	}
}

// flakyTransport fails the first N round trips and then returns a 200 response.
type flakyTransport struct {
	attempts int
	failures int
}

func (f *flakyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	f.attempts++
	if f.attempts <= f.failures {
		return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("ok")),
		Request:    req,
	}, nil
}

// errorTransport always returns the configured error.
type errorTransport struct {
	err      error
	attempts int
}

func (e *errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	e.attempts++
	return nil, e.err
}

// blockingBody blocks reads until it is released.
type blockingBody struct {
	release chan struct{}
}

func (b *blockingBody) Read([]byte) (int, error) {
	<-b.release
	return 0, io.EOF
}

func (b *blockingBody) Close() error { return nil }

// statusTransport returns a fixed response once and reports the call.
type statusTransport struct {
	status int
	body   io.ReadCloser
	called chan struct{}
	once   sync.Once
}

func (t *statusTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.once.Do(func() { close(t.called) })
	return &http.Response{
		StatusCode: t.status,
		Body:       t.body,
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

// TestDoWithRetryStopsWhenContextCancelledDuringBackoff proves the retry
// backoff is cancellable: a cancelled context aborts the wait instead of
// sleeping through it and issuing another request.
func TestDoWithRetryStopsWhenContextCancelledDuringBackoff(t *testing.T) {
	release := make(chan struct{})
	transport := &statusTransport{
		status: http.StatusServiceUnavailable,
		body:   &blockingBody{release: release},
		called: make(chan struct{}),
	}
	client := &http.Client{Transport: transport}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.invalid/pets", nil)
	if err != nil {
		t.Fatalf("http.NewRequestWithContext() error: %v", err)
	}

	retry := config.RetryConfig{
		MaxRetries:           3,
		InitialDelay:         config.Duration{Duration: 30 * time.Second},
		MaxDelay:             config.Duration{Duration: 30 * time.Second},
		RetryableStatusCodes: []int{http.StatusServiceUnavailable},
	}

	done := make(chan error, 1)
	go func() {
		_, err := DoWithRetry(client, req, retry)
		done <- err
	}()

	<-transport.called

	// Cancel while the response body is still being drained, then let the
	// drain finish so the retry backoff is reached with a cancelled context.
	cancel()
	close(release)

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("DoWithRetry() error = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DoWithRetry() did not return after the context was cancelled")
	}
}
