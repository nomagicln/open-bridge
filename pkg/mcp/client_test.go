package mcp

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
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

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/request"
	"github.com/nomagicln/open-bridge/pkg/semantic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- helpers ---

// newClientTestHandler creates a Handler that builds clients per profile
// (nil injected client) for the given default profile.
func newClientTestHandler(t *testing.T, profile config.Profile) *Handler {
	t.Helper()

	handler := NewHandler(semantic.NewMapper(), request.NewBuilder(nil), nil)
	handler.SetSpec(createTestOpenAPISpec())
	handler.SetAppConfig(&config.AppConfig{
		Name:           "testapp",
		DefaultProfile: "default",
		Profiles:       map[string]config.Profile{"default": profile},
	}, "default")

	return handler
}

// callListPets invokes the listPets (GET /pets) tool on a Handler.
func callListPets(t *testing.T, handler *Handler) *mcp.CallToolResult {
	t.Helper()

	result, err := handler.HandleCallTool(context.Background(), createCallToolRequest("listPets", `{}`))
	require.NoError(t, err)
	require.NotNil(t, result)

	return result
}

// newProgressiveClientTestEnv builds a ProgressiveHandler and loads the listPets tool.
func newProgressiveClientTestEnv(t *testing.T, injected *http.Client, profile config.Profile) *ProgressiveHandler {
	t.Helper()

	handler, err := NewProgressiveHandler(request.NewBuilder(nil), injected, SearchEnginePredicate)
	require.NoError(t, err)
	t.Cleanup(func() { _ = handler.Close() })

	handler.SetAppConfig(&config.AppConfig{
		Name:           "testapp",
		DefaultProfile: "default",
		Profiles:       map[string]config.Profile{"default": profile},
	}, "default")
	require.NoError(t, handler.SetSpec(createTestOpenAPISpec(), nil))
	loadProgressiveTool(t, handler, "listPets")

	return handler
}

// invokeListPets invokes the listPets tool through the ProgressiveHandler.
func invokeListPets(t *testing.T, handler *ProgressiveHandler) *mcp.CallToolResult {
	t.Helper()

	return invokeProgressiveTool(t, handler, `{"toolId":"listPets","arguments":{}}`)
}

// clientTestCA is a minimal certificate authority used to issue server
// certificates for per-profile TLS tests.
type clientTestCA struct {
	cert   *x509.Certificate
	key    *ecdsa.PrivateKey
	caPath string
}

// newClientTestCA generates a self-signed CA and writes it to a PEM file.
func newClientTestCA(t *testing.T) *clientTestCA {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "open-bridge-mcp-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)

	caPath := filepath.Join(t.TempDir(), "ca.pem")
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NoError(t, os.WriteFile(caPath, caPEM, 0o600))

	return &clientTestCA{cert: cert, key: key, caPath: caPath}
}

// startTLSAPIServer issues a server certificate for 127.0.0.1 signed by the CA
// and starts an HTTPS API server that counts handled requests.
func (ca *clientTestCA) startTLSAPIServer(t *testing.T, hits *atomic.Int32) *httptest.Server {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "localhost"},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca.cert, &key.PublicKey, ca.key)
	require.NoError(t, err)

	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	server.TLS = &tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}},
	}
	server.StartTLS()
	t.Cleanup(server.Close)

	return server
}

// --- Handler: per-profile TLS ---

func TestHandler_PerProfileTLS_CAFileSucceeds(t *testing.T) {
	ca := newClientTestCA(t)
	var hits atomic.Int32
	server := ca.startTLSAPIServer(t, &hits)

	handler := newClientTestHandler(t, config.Profile{
		Name:      "default",
		BaseURL:   server.URL,
		TLSConfig: config.TLSConfig{CAFile: ca.caPath},
	})

	result := callListPets(t, handler)
	require.False(t, result.IsError, getTextContent(result))
	assert.Equal(t, int32(1), hits.Load())
}

func TestHandler_PerProfileTLS_WrongCAFails(t *testing.T) {
	targetCA := newClientTestCA(t)
	var hits atomic.Int32
	target := targetCA.startTLSAPIServer(t, &hits)

	// Trusting a different CA must not validate this server's certificate.
	wrongCA := newClientTestCA(t)
	handler := newClientTestHandler(t, config.Profile{
		Name:      "default",
		BaseURL:   target.URL,
		TLSConfig: config.TLSConfig{CAFile: wrongCA.caPath},
	})

	result := callListPets(t, handler)
	require.True(t, result.IsError, "request must fail when trusting the wrong CA")
	assert.Contains(t, getTextContent(result), "Failed to execute API call")
	assert.Zero(t, hits.Load())
}

func TestHandler_PerProfileTLS_SelfSignedWithoutCAFails(t *testing.T) {
	ca := newClientTestCA(t)
	var hits atomic.Int32
	server := ca.startTLSAPIServer(t, &hits)

	handler := newClientTestHandler(t, config.Profile{Name: "default", BaseURL: server.URL})

	result := callListPets(t, handler)
	require.True(t, result.IsError, "certificate must be rejected without a trusted CA")
	assert.Zero(t, hits.Load())
}

func TestHandler_InvalidTLSConfig_ReturnsError(t *testing.T) {
	handler := newClientTestHandler(t, config.Profile{
		Name:    "default",
		BaseURL: "https://127.0.0.1:1",
		TLSConfig: config.TLSConfig{
			CAFile: filepath.Join(t.TempDir(), "missing-ca.pem"),
		},
	})

	result := callListPets(t, handler)
	require.True(t, result.IsError)
	text := getTextContent(result)
	assert.Contains(t, text, "Failed to create HTTP client")
	assert.Contains(t, text, "failed to read CA file")
}

// TestHandler_ExplicitClientTakesPriority verifies an injected client is used
// even when the profile's TLS configuration would fail to build.
func TestHandler_ExplicitClientTakesPriority(t *testing.T) {
	ca := newClientTestCA(t)
	var hits atomic.Int32
	server := ca.startTLSAPIServer(t, &hits)

	injected := server.Client()
	handler := NewHandler(semantic.NewMapper(), request.NewBuilder(nil), injected)
	handler.SetSpec(createTestOpenAPISpec())
	handler.SetAppConfig(&config.AppConfig{
		Name:           "testapp",
		DefaultProfile: "default",
		Profiles: map[string]config.Profile{
			"default": {
				Name:    "default",
				BaseURL: server.URL,
				TLSConfig: config.TLSConfig{
					CAFile: filepath.Join(t.TempDir(), "missing-ca.pem"),
				},
			},
		},
	}, "default")

	result := callListPets(t, handler)
	require.False(t, result.IsError, getTextContent(result))
	assert.Equal(t, int32(1), hits.Load())
	assert.Same(t, injected, handler.httpClient)
}

func TestHandler_DefaultClientTimeoutPreserved(t *testing.T) {
	handler := NewHandler(semantic.NewMapper(), request.NewBuilder(nil), nil)
	require.NotNil(t, handler.httpClient)
	assert.Equal(t, defaultHTTPTimeout, handler.httpClient.Timeout)
}

// --- Handler: per-profile timeout + retry ---

func TestHandler_PerProfileTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	handler := newClientTestHandler(t, config.Profile{
		Name:    "default",
		BaseURL: server.URL,
		Timeout: config.Duration{Duration: 25 * time.Millisecond},
	})

	start := time.Now()
	result := callListPets(t, handler)
	elapsed := time.Since(start)

	require.True(t, result.IsError, "profile timeout must abort the MCP tool call")
	assert.Less(t, elapsed, time.Second)
	assert.Contains(t, strings.ToLower(getTextContent(result)), "timeout")
}

func TestHandler_PerProfileRetry(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) <= 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"unavailable"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	handler := newClientTestHandler(t, config.Profile{
		Name:    "default",
		BaseURL: server.URL,
		RetryConfig: config.RetryConfig{
			MaxRetries:           3,
			InitialDelay:         config.Duration{Duration: time.Millisecond},
			MaxDelay:             config.Duration{Duration: 2 * time.Millisecond},
			RetryableStatusCodes: []int{http.StatusServiceUnavailable},
		},
	})

	result := callListPets(t, handler)
	require.False(t, result.IsError, getTextContent(result))
	assert.Equal(t, int32(3), attempts.Load(), "503 responses must be retried (2 failures + 1 success)")
}

// --- Handler: per-profile client cache ---

func TestHandler_PerProfileClientCache(t *testing.T) {
	handler := NewHandler(semantic.NewMapper(), request.NewBuilder(nil), nil)

	profileA := &config.Profile{Name: "a", Timeout: config.Duration{Duration: 5 * time.Second}}
	profileB := &config.Profile{Name: "b", Timeout: config.Duration{Duration: 10 * time.Second}}

	clientA1, err := handler.httpClientForProfile("a", profileA)
	require.NoError(t, err)
	clientA2, err := handler.httpClientForProfile("a", profileA)
	require.NoError(t, err)
	clientB, err := handler.httpClientForProfile("b", profileB)
	require.NoError(t, err)

	assert.Same(t, clientA1, clientA2, "same profile must reuse the cached client")
	assert.NotSame(t, clientA1, clientB, "different profiles must use different clients")
	assert.Equal(t, 5*time.Second, clientA1.Timeout)
	assert.Equal(t, 10*time.Second, clientB.Timeout)

	defaultClient, err := handler.httpClientForProfile("default", nil)
	require.NoError(t, err)
	assert.Equal(t, defaultHTTPTimeout, defaultClient.Timeout)
}

// TestHandler_PerProfileClientCacheConcurrent verifies cache access is race-free
// and returns one shared client per profile under concurrent tool calls.
func TestHandler_PerProfileClientCacheConcurrent(t *testing.T) {
	handler := NewHandler(semantic.NewMapper(), request.NewBuilder(nil), nil)
	profile := &config.Profile{Name: "default", Timeout: config.Duration{Duration: 5 * time.Second}}

	const workers = 20
	clients := make([]*http.Client, workers)
	var wg sync.WaitGroup
	for i := range workers {
		wg.Go(func() {
			client, err := handler.httpClientForProfile("default", profile)
			if err != nil {
				t.Errorf("httpClientForProfile() error: %v", err)
				return
			}
			clients[i] = client
		})
	}
	wg.Wait()

	for i := 1; i < workers; i++ {
		assert.Same(t, clients[0], clients[i], "all goroutines must observe the same cached client")
	}
}

// --- ProgressiveHandler: per-profile TLS/timeout/retry ---

func TestProgressiveHandler_PerProfileTLS_CAFileSucceeds(t *testing.T) {
	ca := newClientTestCA(t)
	var hits atomic.Int32
	server := ca.startTLSAPIServer(t, &hits)

	handler := newProgressiveClientTestEnv(t, nil, config.Profile{
		Name:      "default",
		BaseURL:   server.URL,
		TLSConfig: config.TLSConfig{CAFile: ca.caPath},
	})

	result := invokeListPets(t, handler)
	require.False(t, result.IsError, getTextContent(result))
	assert.Equal(t, int32(1), hits.Load())
}

func TestProgressiveHandler_InvalidTLSConfig_ReturnsError(t *testing.T) {
	handler := newProgressiveClientTestEnv(t, nil, config.Profile{
		Name:    "default",
		BaseURL: "https://127.0.0.1:1",
		TLSConfig: config.TLSConfig{
			CAFile: filepath.Join(t.TempDir(), "missing-ca.pem"),
		},
	})

	result := invokeListPets(t, handler)
	require.True(t, result.IsError)
	assert.Contains(t, getTextContent(result), "Failed to create HTTP client")
}

func TestProgressiveHandler_ExplicitClientTakesPriority(t *testing.T) {
	ca := newClientTestCA(t)
	var hits atomic.Int32
	server := ca.startTLSAPIServer(t, &hits)

	handler := newProgressiveClientTestEnv(t, server.Client(), config.Profile{
		Name:    "default",
		BaseURL: server.URL,
		TLSConfig: config.TLSConfig{
			CAFile: filepath.Join(t.TempDir(), "missing-ca.pem"),
		},
	})

	result := invokeListPets(t, handler)
	require.False(t, result.IsError, getTextContent(result))
	assert.Equal(t, int32(1), hits.Load())
}

func TestProgressiveHandler_PerProfileRetry(t *testing.T) {
	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if attempts.Add(1) <= 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(server.Close)

	handler := newProgressiveClientTestEnv(t, nil, config.Profile{
		Name:    "default",
		BaseURL: server.URL,
		RetryConfig: config.RetryConfig{
			MaxRetries:           2,
			InitialDelay:         config.Duration{Duration: time.Millisecond},
			MaxDelay:             config.Duration{Duration: 2 * time.Millisecond},
			RetryableStatusCodes: []int{http.StatusServiceUnavailable},
		},
	})

	result := invokeListPets(t, handler)
	require.False(t, result.IsError, getTextContent(result))
	assert.Equal(t, int32(2), attempts.Load())
}
