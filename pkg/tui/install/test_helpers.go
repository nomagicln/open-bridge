package install

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/stretchr/testify/require"
)

const testSpecYAML = `openapi: "3.0.0"
info:
  title: TUI Test API
  version: "1.0.0"
servers:
  - url: https://api.example.com/v1
paths:
  /pets:
    get:
      operationId: listPets
      responses:
        "200":
          description: ok
`

// newTestModel builds a fresh wizard model for the given options.
func newTestModel(t *testing.T, opts config.InstallOptions, appExists bool) Model {
	t.Helper()
	return NewModel("demo", opts, appExists)
}

// newTestModelAtStep builds a model positioned directly on the given step.
func newTestModelAtStep(t *testing.T, step Step) Model {
	t.Helper()
	m := NewModel("demo", config.InstallOptions{}, false)
	m.step = step
	return m
}

// applyMsg drives the model through Update and asserts the returned tea.Model
// is still an install.Model.
func applyMsg(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	updated, cmd := m.Update(msg)
	next, ok := updated.(Model)
	require.True(t, ok, "Update must return install.Model")
	return next, cmd
}

// pressKey drives a single bubbletea key event through the model.
func pressKey(t *testing.T, m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	t.Helper()
	return applyMsg(t, m, msg)
}

// typeText feeds a rune sequence to the currently focused text input.
func typeText(t *testing.T, m Model, text string) (Model, tea.Cmd) {
	t.Helper()
	return applyMsg(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text)})
}

func keyEnter() tea.KeyMsg    { return tea.KeyMsg{Type: tea.KeyEnter} }
func keyTab() tea.KeyMsg      { return tea.KeyMsg{Type: tea.KeyTab} }
func keyShiftTab() tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyShiftTab} }
func keyLeft() tea.KeyMsg     { return tea.KeyMsg{Type: tea.KeyLeft} }
func keyRight() tea.KeyMsg    { return tea.KeyMsg{Type: tea.KeyRight} }
func keyUp() tea.KeyMsg       { return tea.KeyMsg{Type: tea.KeyUp} }
func keyDown() tea.KeyMsg     { return tea.KeyMsg{Type: tea.KeyDown} }
func keyCtrlC() tea.KeyMsg    { return tea.KeyMsg{Type: tea.KeyCtrlC} }

// writeTestSpec creates a valid OpenAPI spec file and returns its path.
func writeTestSpec(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "openapi.yaml")
	require.NoError(t, os.WriteFile(path, []byte(testSpecYAML), 0o600))
	return path
}

// writeTLSAssets generates a self-signed CA certificate plus a matching
// client certificate/key pair, returning their file paths.
func writeTLSAssets(t *testing.T) (caCertPath, clientCertPath, clientKeyPath string) {
	t.Helper()

	dir := t.TempDir()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	require.NoError(t, err)

	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "openbridge-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(cryptorand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	caPath := filepath.Join(dir, "ca.pem")
	require.NoError(t, os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), 0o600))

	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	require.NoError(t, err)
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "openbridge-test-client"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	clientDER, err := x509.CreateCertificate(cryptorand.Reader, clientTemplate, caTemplate, &clientKey.PublicKey, caKey)
	require.NoError(t, err)

	certPath := filepath.Join(dir, "client.pem")
	require.NoError(t, os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: clientDER}), 0o600))

	keyDER, err := x509.MarshalECPrivateKey(clientKey)
	require.NoError(t, err)
	keyPath := filepath.Join(dir, "client-key.pem")
	require.NoError(t, os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600))

	return caPath, certPath, keyPath
}

// writeInvalidPEM writes a file that is not valid PEM data.
func writeInvalidPEM(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "invalid.pem")
	require.NoError(t, os.WriteFile(path, []byte("not a pem file"), 0o600))
	return path
}
