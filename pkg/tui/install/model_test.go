package install

import (
	"errors"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nomagicln/open-bridge/internal/install"
	"github.com/nomagicln/open-bridge/pkg/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewModel(t *testing.T) {
	tests := []struct {
		name         string
		opts         install.InstallOptions
		wantStep     Step
		wantAuthIdx  int
		wantTLSIdx   int
		wantViewText string
	}{
		{
			name:         "default starts at spec input",
			opts:         install.InstallOptions{},
			wantStep:     StepSpecInput,
			wantViewText: "OpenAPI Specification Source",
		},
		{
			name:         "prefilled spec source starts at base URL",
			opts:         install.InstallOptions{SpecSource: "spec.yaml"},
			wantStep:     StepBaseURL,
			wantViewText: "Base URL",
		},
		{
			name:         "multiple spec sources start at base URL",
			opts:         install.InstallOptions{SpecSources: []string{"a.yaml", "b.yaml"}},
			wantStep:     StepBaseURL,
			wantViewText: "Base URL",
		},
		{
			name:         "auth type selects initial index",
			opts:         install.InstallOptions{AuthType: "basic"},
			wantStep:     StepSpecInput,
			wantAuthIdx:  3,
			wantViewText: "OpenAPI Specification Source",
		},
		{
			name:         "tls skip verify preselects yes",
			opts:         install.InstallOptions{TLSSkipVerify: true},
			wantStep:     StepSpecInput,
			wantTLSIdx:   1,
			wantViewText: "OpenAPI Specification Source",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t, tt.opts, false)

			assert.Equal(t, tt.wantStep, m.step)
			assert.Equal(t, tt.wantAuthIdx, m.authIndex)
			assert.Equal(t, tt.wantTLSIdx, m.tlsSkipVerifyIndex)
			assert.Equal(t, "demo", m.appName)
			assert.NotNil(t, m.collectedHeaders)
			assert.Empty(t, m.history)
			assert.Nil(t, m.Result())
			assert.Contains(t, m.View(), tt.wantViewText)
		})
	}
}

func TestNewModelPrefilledDefaults(t *testing.T) {
	opts := install.InstallOptions{
		SpecSource:  "spec.yaml",
		Description: "preset description",
		BaseURL:     "https://preset.example.com",
	}
	m := newTestModel(t, opts, true)

	assert.True(t, m.appExists)
	assert.Equal(t, "preset description", m.defaultDescription)
	assert.Equal(t, "preset description", m.descInput.Placeholder)
	assert.Equal(t, "https://preset.example.com", m.defaultBaseURL)
	assert.Equal(t, "https://preset.example.com", m.baseUrlInput.Placeholder)
	assert.Equal(t, "spec.yaml", m.specInput.Value())
}

func TestNewModelPrefilledTLSAssets(t *testing.T) {
	opts := install.InstallOptions{
		SpecSource:    "spec.yaml",
		TLSCACert:     "/preset/ca.pem",
		TLSClientCert: "/preset/client.pem",
		TLSClientKey:  "/preset/client-key.pem",
	}
	m := newTestModel(t, opts, false)

	assert.Equal(t, "/preset/ca.pem", m.tlsCACertInput.Value())
	assert.Equal(t, "/preset/client.pem", m.tlsClientCertInput.Value())
	assert.Equal(t, "/preset/client-key.pem", m.tlsClientKeyInput.Value())
}

func TestInit(t *testing.T) {
	assert.NotNil(t, newTestModel(t, install.InstallOptions{}, false).Init())

	loading := newTestModelAtStep(t, StepLoading)
	assert.NotNil(t, loading.Init())
}

func TestUpdateCtrlCQuits(t *testing.T) {
	m := newTestModel(t, install.InstallOptions{}, false)

	next, cmd := pressKey(t, m, keyCtrlC())

	require.NotNil(t, cmd)
	assert.Equal(t, tea.QuitMsg{}, cmd())
	assert.Equal(t, StepSpecInput, next.step)
}

func TestUpdateUnknownStepIsNoop(t *testing.T) {
	m := newTestModelAtStep(t, StepDone)

	next, cmd := applyMsg(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})

	assert.Nil(t, cmd)
	assert.Equal(t, StepDone, next.step)
	assert.Contains(t, next.View(), "Installing demo")
}

func TestSpecInputStep(t *testing.T) {
	t.Run("typed value advances to base URL", func(t *testing.T) {
		m := newTestModel(t, install.InstallOptions{}, false)

		m, _ = typeText(t, m, "spec.yaml")
		require.Equal(t, "spec.yaml", m.specInput.Value())

		m, cmd := pressKey(t, m, keyEnter())

		assert.NotNil(t, cmd)
		assert.Equal(t, StepBaseURL, m.step)
		assert.Equal(t, "spec.yaml", m.options.SpecSource)
		assert.Nil(t, m.err)
		assert.Len(t, m.history, 1)
		assert.True(t, m.baseUrlInput.Focused())
	})

	t.Run("empty input reports validation error", func(t *testing.T) {
		m := newTestModel(t, install.InstallOptions{}, false)
		require.NotEmpty(t, m.specInput.Placeholder, "placeholder hint must still be rendered")

		next, cmd := pressKey(t, m, keyEnter())

		assert.Nil(t, cmd)
		require.Error(t, next.err)
		assert.Contains(t, next.err.Error(), "spec source cannot be empty")
		assert.Equal(t, StepSpecInput, next.step)
		assert.Empty(t, next.options.SpecSource)
	})

	t.Run("non-key message updates input", func(t *testing.T) {
		m := newTestModel(t, install.InstallOptions{}, false)
		next, _ := applyMsg(t, m, tea.WindowSizeMsg{Width: 120, Height: 40})
		assert.Equal(t, StepSpecInput, next.step)
	})
}

// reachBaseURLStep builds a model positioned at the base URL step.
func reachBaseURLStep(t *testing.T, opts install.InstallOptions) Model {
	t.Helper()
	m := newTestModel(t, opts, false)
	require.Equal(t, StepBaseURL, m.step)
	return m
}

func TestBaseURLStep(t *testing.T) {
	t.Run("generic placeholder is not used as value", func(t *testing.T) {
		m := reachBaseURLStep(t, install.InstallOptions{SpecSource: "spec.yaml"})
		require.NotEmpty(t, m.baseUrlInput.Placeholder, "placeholder hint must still be rendered")

		next, _ := pressKey(t, m, keyEnter())

		require.Error(t, next.err)
		assert.Contains(t, next.err.Error(), "Base URL is required")
		assert.Equal(t, StepBaseURL, next.step)
		assert.Empty(t, next.options.BaseURL)
	})

	t.Run("invalid URL reports error", func(t *testing.T) {
		m := reachBaseURLStep(t, install.InstallOptions{SpecSource: "spec.yaml"})
		m, _ = typeText(t, m, "ftp://files.example.com")

		next, _ := pressKey(t, m, keyEnter())

		require.Error(t, next.err)
		assert.Contains(t, next.err.Error(), "invalid Base URL")
		assert.Equal(t, StepBaseURL, next.step)
	})

	t.Run("http URL skips TLS steps", func(t *testing.T) {
		m := reachBaseURLStep(t, install.InstallOptions{SpecSource: "spec.yaml"})
		m, _ = typeText(t, m, "http://api.example.com")

		next, _ := pressKey(t, m, keyEnter())

		assert.Nil(t, next.err)
		assert.Equal(t, StepAuthType, next.step)
		assert.Equal(t, "http://api.example.com", next.options.BaseURL)
		assert.Len(t, next.history, 1)
	})

	t.Run("https URL goes to TLS steps", func(t *testing.T) {
		m := reachBaseURLStep(t, install.InstallOptions{SpecSource: "spec.yaml"})
		m, _ = typeText(t, m, "https://api.example.com")

		next, _ := pressKey(t, m, keyEnter())

		assert.Nil(t, next.err)
		assert.Equal(t, StepTLSSkipVerify, next.step)
	})

	t.Run("prefilled default is used for empty input", func(t *testing.T) {
		m := reachBaseURLStep(t, install.InstallOptions{
			SpecSource: "spec.yaml",
			BaseURL:    "http://default.example.com",
		})

		next, _ := pressKey(t, m, keyEnter())

		assert.Nil(t, next.err)
		assert.Equal(t, "http://default.example.com", next.options.BaseURL)
		assert.Equal(t, StepAuthType, next.step)
	})
}

func TestNeedsTLSConfiguration(t *testing.T) {
	tests := []struct {
		name string
		opts install.InstallOptions
		want bool
	}{
		{name: "no URLs", opts: install.InstallOptions{}, want: false},
		{name: "http spec and http base", opts: install.InstallOptions{SpecSource: "http://x/spec", BaseURL: "http://x"}, want: false},
		{name: "https spec", opts: install.InstallOptions{SpecSource: "https://x/spec", BaseURL: "http://x"}, want: true},
		{name: "https base", opts: install.InstallOptions{SpecSource: "spec.yaml", BaseURL: "https://x"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t, tt.opts, false)
			assert.Equal(t, tt.want, m.needsTLSConfiguration())
		})
	}
}

func TestTLSSkipVerifyStep(t *testing.T) {
	m := newTestModelAtStep(t, StepTLSSkipVerify)

	m, _ = pressKey(t, m, keyRight())
	assert.Equal(t, 1, m.tlsSkipVerifyIndex)
	m, _ = pressKey(t, m, keyLeft())
	assert.Equal(t, 0, m.tlsSkipVerifyIndex)
	m, _ = typeText(t, m, "l")
	assert.Equal(t, 1, m.tlsSkipVerifyIndex)
	m, _ = typeText(t, m, "h")
	assert.Equal(t, 0, m.tlsSkipVerifyIndex)

	m, _ = applyMsg(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	next, cmd := pressKey(t, m, keyEnter())

	assert.Equal(t, StepTLSCACert, next.step)
	assert.False(t, next.options.TLSSkipVerify)
	assert.NotNil(t, cmd)
	assert.True(t, next.tlsCACertInput.Focused())

	enabled := newTestModelAtStep(t, StepTLSSkipVerify)
	enabled, _ = pressKey(t, enabled, keyRight())
	enabled, _ = pressKey(t, enabled, keyEnter())
	assert.True(t, enabled.options.TLSSkipVerify)
	assert.Len(t, enabled.history, 1)
}

func TestTLSCACertStep(t *testing.T) {
	t.Run("invalid path reports error", func(t *testing.T) {
		m := newTestModelAtStep(t, StepTLSCACert)
		m.tlsCACertInput.Focus()
		m, _ = typeText(t, m, "does-not-exist.pem")

		next, _ := pressKey(t, m, keyEnter())

		require.Error(t, next.err)
		assert.Contains(t, next.err.Error(), "invalid CA certificate")
		assert.Equal(t, StepTLSCACert, next.step)
	})

	t.Run("leave empty skips validation", func(t *testing.T) {
		m := newTestModelAtStep(t, StepTLSCACert)

		next, cmd := pressKey(t, m, keyEnter())

		assert.Nil(t, next.err)
		assert.Equal(t, StepTLSClientCert, next.step)
		assert.Equal(t, 0, next.focusIndex)
		assert.NotNil(t, cmd)
	})

	t.Run("valid CA certificate is stored", func(t *testing.T) {
		caCert, _, _ := writeTLSAssets(t)
		m := newTestModelAtStep(t, StepTLSCACert)
		m.tlsCACertInput.Focus()
		m, _ = typeText(t, m, caCert)

		next, _ := pressKey(t, m, keyEnter())

		assert.Nil(t, next.err)
		assert.Equal(t, caCert, next.options.TLSCACert)
		assert.Equal(t, StepTLSClientCert, next.step)
		assert.Len(t, next.history, 1)
	})

	t.Run("non-key message is forwarded to input", func(t *testing.T) {
		m := newTestModelAtStep(t, StepTLSCACert)
		next, _ := applyMsg(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
		assert.Equal(t, StepTLSCACert, next.step)
	})
}

func TestTLSClientCertStep(t *testing.T) {
	t.Run("both empty skips to auth type", func(t *testing.T) {
		m := newTestModelAtStep(t, StepTLSClientCert)

		next, _ := pressKey(t, m, keyEnter())

		assert.Nil(t, next.err)
		assert.Equal(t, StepAuthType, next.step)
	})

	t.Run("cert without key reports error", func(t *testing.T) {
		m := newTestModelAtStep(t, StepTLSClientCert)
		m.tlsClientCertInput.SetValue("/tmp/client.pem")

		next, _ := pressKey(t, m, keyEnter())

		require.Error(t, next.err)
		assert.Contains(t, next.err.Error(), "must be provided together")
		assert.Equal(t, StepTLSClientCert, next.step)
	})

	t.Run("valid cert and key are stored", func(t *testing.T) {
		_, clientCert, clientKey := writeTLSAssets(t)
		m := newTestModelAtStep(t, StepTLSClientCert)
		m.tlsClientCertInput.SetValue(clientCert)
		m.tlsClientKeyInput.SetValue(clientKey)

		next, _ := pressKey(t, m, keyEnter())

		assert.Nil(t, next.err)
		assert.Equal(t, clientCert, next.options.TLSClientCert)
		assert.Equal(t, clientKey, next.options.TLSClientKey)
		assert.Equal(t, StepAuthType, next.step)
		assert.Len(t, next.history, 2)
	})

	t.Run("invalid certificate reports error", func(t *testing.T) {
		_, _, clientKey := writeTLSAssets(t)
		m := newTestModelAtStep(t, StepTLSClientCert)
		m.tlsClientCertInput.SetValue(writeInvalidPEM(t))
		m.tlsClientKeyInput.SetValue(clientKey)

		next, _ := pressKey(t, m, keyEnter())

		require.Error(t, next.err)
		assert.Contains(t, next.err.Error(), "invalid client certificate")
	})

	t.Run("invalid key reports error", func(t *testing.T) {
		_, clientCert, _ := writeTLSAssets(t)
		m := newTestModelAtStep(t, StepTLSClientCert)
		m.tlsClientCertInput.SetValue(clientCert)
		m.tlsClientKeyInput.SetValue(writeInvalidPEM(t))

		next, _ := pressKey(t, m, keyEnter())

		require.Error(t, next.err)
		assert.Contains(t, next.err.Error(), "invalid client key")
	})

	t.Run("tab toggles focus and typing targets focused input", func(t *testing.T) {
		m := newTestModelAtStep(t, StepTLSClientCert)
		m.tlsClientCertInput.Focus()

		m, _ = pressKey(t, m, keyTab())
		assert.Equal(t, 1, m.focusIndex)
		assert.True(t, m.tlsClientKeyInput.Focused())

		m, _ = typeText(t, m, "key-material")
		assert.Equal(t, "key-material", m.tlsClientKeyInput.Value())

		m, _ = pressKey(t, m, keyShiftTab())
		assert.Equal(t, 0, m.focusIndex)
		assert.True(t, m.tlsClientCertInput.Focused())

		m, _ = typeText(t, m, "cert-material")
		assert.Equal(t, "cert-material", m.tlsClientCertInput.Value())
	})

	t.Run("non-key message updates focused input", func(t *testing.T) {
		m := newTestModelAtStep(t, StepTLSClientCert)
		next, _ := applyMsg(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
		assert.Equal(t, StepTLSClientCert, next.step)
	})
}

// reachAuthTypeStep drives the model to the auth type step via the http base
// URL path (which skips the TLS steps).
func reachAuthTypeStep(t *testing.T) Model {
	t.Helper()
	m := reachBaseURLStep(t, install.InstallOptions{SpecSource: "spec.yaml"})
	m, _ = typeText(t, m, "http://api.example.com")
	m, _ = pressKey(t, m, keyEnter())
	require.Equal(t, StepAuthType, m.step)
	return m
}

func TestAuthTypeStep(t *testing.T) {
	t.Run("cycles options with wrap-around", func(t *testing.T) {
		m := reachAuthTypeStep(t)

		m, _ = pressKey(t, m, keyLeft())
		assert.Equal(t, len(m.authOptions)-1, m.authIndex)
		m, _ = pressKey(t, m, keyRight())
		assert.Equal(t, 0, m.authIndex)
		m, _ = typeText(t, m, "l")
		assert.Equal(t, 1, m.authIndex)
		m, _ = typeText(t, m, "h")
		assert.Equal(t, 0, m.authIndex)
	})

	t.Run("none skips auth details and loads spec", func(t *testing.T) {
		m := reachAuthTypeStep(t)

		next, cmd := pressKey(t, m, keyEnter())

		assert.Equal(t, StepLoading, next.step)
		assert.Equal(t, "none", next.options.AuthType)
		assert.NotNil(t, cmd)
		assert.Contains(t, next.history[1], "Auth Type")
	})

	t.Run("bearer prepares one input", func(t *testing.T) {
		m := reachAuthTypeStep(t)
		m, _ = pressKey(t, m, keyRight())

		next, _ := pressKey(t, m, keyEnter())

		assert.Equal(t, StepAuthDetails, next.step)
		require.Len(t, next.authInputs, 1)
		assert.Equal(t, []string{"Token"}, next.authInputLabels)
	})

	t.Run("api_key prepares name and value inputs", func(t *testing.T) {
		m := reachAuthTypeStep(t)
		m, _ = pressKey(t, m, keyRight())
		m, _ = pressKey(t, m, keyRight())

		next, _ := pressKey(t, m, keyEnter())

		assert.Equal(t, StepAuthDetails, next.step)
		require.Len(t, next.authInputs, 2)
		assert.Equal(t, "X-API-Key", next.authInputs[0].Value())
		assert.Equal(t, []string{"Header Name", "Key Value"}, next.authInputLabels)
	})

	t.Run("basic prepares username and password inputs", func(t *testing.T) {
		m := reachAuthTypeStep(t)
		for range 3 {
			m, _ = pressKey(t, m, keyRight())
		}

		next, _ := pressKey(t, m, keyEnter())

		assert.Equal(t, StepAuthDetails, next.step)
		require.Len(t, next.authInputs, 2)
		assert.Equal(t, []string{"Username", "Password"}, next.authInputLabels)
	})

	t.Run("non-key message is ignored", func(t *testing.T) {
		m := reachAuthTypeStep(t)
		next, cmd := applyMsg(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
		assert.Nil(t, cmd)
		assert.Equal(t, StepAuthType, next.step)
	})
}

func TestAuthDetailsStep(t *testing.T) {
	t.Run("bearer token is collected", func(t *testing.T) {
		m := reachAuthTypeStep(t)
		m, _ = pressKey(t, m, keyRight())
		m, _ = pressKey(t, m, keyEnter())

		m, _ = typeText(t, m, "secret-token")
		next, cmd := pressKey(t, m, keyEnter())

		assert.Equal(t, StepLoading, next.step)
		assert.NotNil(t, cmd)
		assert.Equal(t, "secret-token", next.options.AuthParams["token"])
	})

	t.Run("basic credentials are collected", func(t *testing.T) {
		m := reachAuthTypeStep(t)
		for range 3 {
			m, _ = pressKey(t, m, keyRight())
		}
		m, _ = pressKey(t, m, keyEnter())

		m, _ = typeText(t, m, "alice")
		m, _ = pressKey(t, m, keyTab())
		assert.Equal(t, 1, m.focusIndex)
		m, _ = typeText(t, m, "hunter2")
		next, _ := pressKey(t, m, keyEnter())

		assert.Equal(t, StepLoading, next.step)
		assert.Equal(t, "alice", next.options.AuthParams["username"])
		assert.Equal(t, "hunter2", next.options.AuthParams["password"])
	})

	t.Run("api_key credentials are collected", func(t *testing.T) {
		m := reachAuthTypeStep(t)
		m, _ = pressKey(t, m, keyRight())
		m, _ = pressKey(t, m, keyRight())
		m, _ = pressKey(t, m, keyEnter())
		require.Len(t, m.authInputs, 2)

		m, _ = pressKey(t, m, keyTab())
		m, _ = typeText(t, m, "secret-key")
		next, _ := pressKey(t, m, keyEnter())

		assert.Equal(t, StepLoading, next.step)
		assert.Equal(t, "X-API-Key", next.options.AuthParams["key_name"])
		assert.Equal(t, "secret-key", next.options.AuthParams["token"])
	})

	t.Run("navigation keys cycle focus", func(t *testing.T) {
		m := reachAuthTypeStep(t)
		for range 3 {
			m, _ = pressKey(t, m, keyRight())
		}
		m, _ = pressKey(t, m, keyEnter())
		require.Len(t, m.authInputs, 2)

		m, _ = pressKey(t, m, keyUp())
		assert.Equal(t, 1, m.focusIndex)
		m, _ = pressKey(t, m, keyDown())
		assert.Equal(t, 0, m.focusIndex)
		m, _ = pressKey(t, m, keyShiftTab())
		assert.Equal(t, 1, m.focusIndex)
		m, _ = pressKey(t, m, keyTab())
		assert.Equal(t, 0, m.focusIndex)

		next, _ := applyMsg(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
		assert.Equal(t, StepAuthDetails, next.step)

		outOfRange := m
		outOfRange.focusIndex = len(m.authInputs)
		_, cmd := applyMsg(t, outOfRange, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
		assert.Nil(t, cmd)
	})
}

func TestLoadingStep(t *testing.T) {
	t.Run("spinner tick is forwarded", func(t *testing.T) {
		m := newTestModelAtStep(t, StepLoading)
		next, _ := applyMsg(t, m, m.spinner.Tick())
		assert.Equal(t, StepLoading, next.step)
	})

	t.Run("spec load error returns to spec input", func(t *testing.T) {
		m := newTestModelAtStep(t, StepLoading)

		next, cmd := applyMsg(t, m, specLoadedMsg{err: errors.New("boom")})

		assert.Nil(t, cmd)
		require.Error(t, next.err)
		assert.Equal(t, StepSpecInput, next.step)
	})

	t.Run("successful load moves to description", func(t *testing.T) {
		m := newTestModelAtStep(t, StepLoading)
		info := &spec.SpecInfo{Title: "Pets API", Operations: 5}

		next, cmd := applyMsg(t, m, specLoadedMsg{info: info})

		assert.Equal(t, StepDescription, next.step)
		assert.NotNil(t, cmd)
		require.NotNil(t, next.specDoc)
		assert.Equal(t, "Pets API", next.defaultDescription)
		assert.Equal(t, "Pets API", next.descInput.Placeholder)
		assert.Contains(t, next.progressiveRecommendation, "5 operations")
		assert.Contains(t, next.View(), "Pets API")
	})

	t.Run("large spec recommends progressive disclosure", func(t *testing.T) {
		m := newTestModelAtStep(t, StepLoading)
		info := &spec.SpecInfo{Title: "Big API", Operations: install.DefaultProgressiveThreshold + 5}

		next, _ := applyMsg(t, m, specLoadedMsg{info: info})

		assert.Contains(t, next.progressiveRecommendation, "Recommended")
		assert.Equal(t, 1, next.mcpProgressiveIndex)
		assert.Equal(t, 1, next.mcpAdvancedIndex)
	})

	t.Run("custom progressive threshold is honoured", func(t *testing.T) {
		m := newTestModelAtStep(t, StepLoading)
		m.options.ProgressiveThreshold = 2
		info := &spec.SpecInfo{Title: "", Operations: 3}

		next, _ := applyMsg(t, m, specLoadedMsg{info: info})

		assert.Contains(t, next.progressiveRecommendation, "Recommended")
		assert.Empty(t, next.defaultDescription)
	})

	t.Run("non-key message is ignored", func(t *testing.T) {
		m := newTestModelAtStep(t, StepLoading)
		next, cmd := applyMsg(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
		assert.Nil(t, cmd)
		assert.Equal(t, StepLoading, next.step)
	})
}

func TestDescriptionStep(t *testing.T) {
	m := newTestModelAtStep(t, StepDescription)
	m.descInput.Focus()
	m, _ = typeText(t, m, "my application")

	next, _ := pressKey(t, m, keyEnter())

	assert.Equal(t, StepShim, next.step)
	assert.Equal(t, "my application", next.options.Description)
	assert.Len(t, next.history, 1)

	defaulted := newTestModelAtStep(t, StepDescription)
	defaulted.defaultDescription = "default description"
	nextDefault, _ := pressKey(t, defaulted, keyEnter())
	assert.Equal(t, "default description", nextDefault.options.Description)

	placeholder := newTestModelAtStep(t, StepDescription)
	placeholder.descInput.Placeholder = "placeholder description"
	nextPlaceholder, _ := pressKey(t, placeholder, keyEnter())
	assert.Equal(t, StepShim, nextPlaceholder.step)
	assert.Empty(t, nextPlaceholder.options.Description)

	nonKey := newTestModelAtStep(t, StepDescription)
	nextNonKey, _ := applyMsg(t, nonKey, tea.WindowSizeMsg{Width: 80, Height: 24})
	assert.Equal(t, StepDescription, nextNonKey.step)
}

func TestShimStep(t *testing.T) {
	no := newTestModelAtStep(t, StepShim)
	no, _ = pressKey(t, no, keyRight())
	require.Equal(t, 1, no.shimIndex)
	no, _ = pressKey(t, no, keyEnter())
	assert.False(t, no.options.CreateShim)
	assert.Equal(t, StepAddHeadersConfirm, no.step)

	yes := newTestModelAtStep(t, StepShim)
	yes, _ = pressKey(t, yes, keyLeft())
	require.Equal(t, 1, yes.shimIndex)
	yes, _ = pressKey(t, yes, keyLeft())
	require.Equal(t, 0, yes.shimIndex)
	yes, _ = pressKey(t, yes, keyEnter())
	assert.True(t, yes.options.CreateShim)
	assert.Equal(t, StepAddHeadersConfirm, yes.step)
	assert.Len(t, yes.history, 1)
}

func TestAddHeadersConfirmStep(t *testing.T) {
	tests := []struct {
		name      string
		appExists bool
		force     bool
		wantStep  Step
	}{
		{name: "no headers new app goes to MCP", appExists: false, wantStep: StepMCPAdvancedConfirm},
		{name: "no headers existing app asks overwrite", appExists: true, wantStep: StepOverwriteConfirm},
		{name: "no headers existing app forced goes to MCP", appExists: true, force: true, wantStep: StepMCPAdvancedConfirm},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t, install.InstallOptions{Force: tt.force}, tt.appExists)
			m.step = StepAddHeadersConfirm

			next, _ := pressKey(t, m, keyLeft())
			assert.Equal(t, 0, next.addHeadersIndex)
			next, _ = pressKey(t, next, keyRight())
			assert.Equal(t, 1, next.addHeadersIndex)
			next, _ = pressKey(t, next, keyLeft())
			next, _ = pressKey(t, next, keyEnter())

			assert.Equal(t, tt.wantStep, next.step)
		})
	}

	t.Run("yes opens header input", func(t *testing.T) {
		m := newTestModelAtStep(t, StepAddHeadersConfirm)
		m, _ = pressKey(t, m, keyRight())

		next, cmd := pressKey(t, m, keyEnter())

		assert.Equal(t, StepHeaderInput, next.step)
		assert.Equal(t, 0, next.focusIndex)
		assert.NotNil(t, cmd)
	})
}

func TestHeaderInputStep(t *testing.T) {
	m := newTestModelAtStep(t, StepHeaderInput)
	m.headerNameInput.Focus()

	m, _ = typeText(t, m, "X-Test-Header")
	m, _ = pressKey(t, m, keyTab())
	assert.Equal(t, 1, m.focusIndex)
	m, _ = typeText(t, m, "value-1")
	next, _ := pressKey(t, m, keyEnter())

	assert.Equal(t, "value-1", next.collectedHeaders["X-Test-Header"])
	assert.Equal(t, 0, next.focusIndex)
	assert.Empty(t, next.headerNameInput.Value())
	assert.Empty(t, next.headerValueInput.Value())
	assert.Len(t, next.history, 1)

	next, _ = pressKey(t, next, keyEnter())
	assert.Equal(t, StepMCPAdvancedConfirm, next.step)

	orphan := newTestModelAtStep(t, StepHeaderInput)
	orphan, _ = pressKey(t, orphan, keyTab())
	orphan, _ = typeText(t, orphan, "orphan-value")
	nextOrphan, _ := pressKey(t, orphan, keyEnter())
	assert.Empty(t, nextOrphan.collectedHeaders)
	assert.Equal(t, 0, nextOrphan.focusIndex)

	spacey := newTestModelAtStep(t, StepHeaderInput)
	spacey.headerNameInput.Focus()
	spacey, _ = typeText(t, spacey, " X-Spaced ")
	spacey, _ = pressKey(t, spacey, keyTab())
	spacey, _ = typeText(t, spacey, " v ")
	spacey, _ = pressKey(t, spacey, keyEnter())
	assert.Equal(t, "v", spacey.collectedHeaders["X-Spaced"])

	toggle := newTestModelAtStep(t, StepHeaderInput)
	toggle, _ = pressKey(t, toggle, keyTab())
	assert.Equal(t, 1, toggle.focusIndex)
	toggle, _ = pressKey(t, toggle, keyShiftTab())
	assert.Equal(t, 0, toggle.focusIndex)

	nonKey := newTestModelAtStep(t, StepHeaderInput)
	nextNonKey, _ := applyMsg(t, nonKey, tea.WindowSizeMsg{Width: 80, Height: 24})
	assert.Equal(t, StepHeaderInput, nextNonKey.step)
}

func TestMCPAdvancedConfirmStep(t *testing.T) {
	t.Run("skip finishes installation", func(t *testing.T) {
		m := newTestModelAtStep(t, StepMCPAdvancedConfirm)
		m.collectedHeaders["X-Test"] = "value"

		next, cmd := pressKey(t, m, keyEnter())

		assert.NotNil(t, next.Result())
		assert.NotNil(t, cmd)
		assert.Equal(t, "value", next.Result().Headers["X-Test"])
		assert.Contains(t, next.View(), "")
	})

	t.Run("configure opens progressive disclosure", func(t *testing.T) {
		m := newTestModelAtStep(t, StepMCPAdvancedConfirm)

		next, _ := pressKey(t, m, keyLeft())
		assert.Equal(t, 0, next.mcpAdvancedIndex)
		next, _ = pressKey(t, next, keyRight())
		assert.Equal(t, 1, next.mcpAdvancedIndex)
		next, _ = pressKey(t, next, keyEnter())

		assert.Equal(t, StepMCPProgressiveDisclosure, next.step)
	})

	t.Run("non-key message is ignored", func(t *testing.T) {
		m := newTestModelAtStep(t, StepMCPAdvancedConfirm)
		next, cmd := applyMsg(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
		assert.Nil(t, cmd)
		assert.Equal(t, StepMCPAdvancedConfirm, next.step)
	})
}

func TestMCPProgressiveSteps(t *testing.T) {
	t.Run("default config auto-selects single engine", func(t *testing.T) {
		m := newTestModelAtStep(t, StepMCPProgressiveDisclosure)

		next, _ := pressKey(t, m, keyEnter())

		require.NotNil(t, next.options.ProgressiveDisclosure)
		assert.False(t, *next.options.ProgressiveDisclosure)
		assert.Equal(t, "predicate", next.options.SearchEngine)
		assert.Equal(t, StepMCPReadOnlyMode, next.step)
	})

	t.Run("enabled progressive disclosure is recorded", func(t *testing.T) {
		m := newTestModelAtStep(t, StepMCPProgressiveDisclosure)

		m, _ = pressKey(t, m, keyLeft())
		assert.Equal(t, 0, m.mcpProgressiveIndex)
		m, _ = pressKey(t, m, keyRight())
		next, _ := pressKey(t, m, keyEnter())

		require.NotNil(t, next.options.ProgressiveDisclosure)
		assert.True(t, *next.options.ProgressiveDisclosure)
		assert.Contains(t, next.history[0], "Enabled")
	})

	t.Run("multiple search engines open engine selection", func(t *testing.T) {
		m := newTestModelAtStep(t, StepMCPProgressiveDisclosure)
		m.mcpSearchEngineOptions = []string{"predicate", "sql"}

		next, _ := pressKey(t, m, keyEnter())
		assert.Equal(t, StepMCPSearchEngine, next.step)

		next, _ = pressKey(t, next, keyLeft())
		assert.Equal(t, 1, next.mcpSearchEngineIndex)
		next, _ = pressKey(t, next, keyRight())
		assert.Equal(t, 0, next.mcpSearchEngineIndex)
		next, _ = pressKey(t, next, keyRight())
		assert.Equal(t, 1, next.mcpSearchEngineIndex)
		next, _ = pressKey(t, next, keyEnter())
		assert.Equal(t, "sql", next.options.SearchEngine)
		assert.Equal(t, StepMCPReadOnlyMode, next.step)
		assert.Contains(t, next.View(), "Read-Only")
	})

	t.Run("recommendation hint is rendered", func(t *testing.T) {
		m := newTestModelAtStep(t, StepMCPProgressiveDisclosure)
		m.progressiveRecommendation = "Recommended: 100 operations"

		assert.Contains(t, m.View(), "Recommended: 100 operations")
	})

	t.Run("non-key message is ignored", func(t *testing.T) {
		m := newTestModelAtStep(t, StepMCPProgressiveDisclosure)
		next, _ := applyMsg(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
		assert.Equal(t, StepMCPProgressiveDisclosure, next.step)
	})
}

func TestMCPReadOnlyAndProtectSensitiveSteps(t *testing.T) {
	readOnly := newTestModelAtStep(t, StepMCPReadOnlyMode)
	readOnly, _ = pressKey(t, readOnly, keyLeft())
	readOnly, _ = pressKey(t, readOnly, keyRight())
	next, _ := pressKey(t, readOnly, keyEnter())
	assert.True(t, next.options.ReadOnlyMode)
	assert.Equal(t, StepProtectSensitiveInfo, next.step)
	assert.Contains(t, next.View(), "Protect Sensitive Information")

	disabled := newTestModelAtStep(t, StepMCPReadOnlyMode)
	disabled, _ = applyMsg(t, disabled, tea.WindowSizeMsg{Width: 80, Height: 24})
	disabled, _ = pressKey(t, disabled, keyEnter())
	assert.False(t, disabled.options.ReadOnlyMode)

	protect := newTestModelAtStep(t, StepProtectSensitiveInfo)
	protect, _ = pressKey(t, protect, keyLeft())
	protect, _ = pressKey(t, protect, keyRight())
	finished, cmd := pressKey(t, protect, keyEnter())
	assert.True(t, finished.options.ProtectSensitiveInfo)
	assert.NotNil(t, finished.Result())
	assert.NotNil(t, cmd)

	keepRaw := newTestModelAtStep(t, StepProtectSensitiveInfo)
	keepRaw, _ = applyMsg(t, keepRaw, tea.WindowSizeMsg{Width: 80, Height: 24})
	finishedRaw, _ := pressKey(t, keepRaw, keyEnter())
	assert.False(t, finishedRaw.options.ProtectSensitiveInfo)
}

func TestOverwriteConfirmStep(t *testing.T) {
	t.Run("declining aborts", func(t *testing.T) {
		m := newTestModel(t, install.InstallOptions{}, true)
		m.step = StepOverwriteConfirm

		next, _ := pressKey(t, m, keyLeft())
		next, cmd := pressKey(t, next, keyEnter())

		require.Error(t, next.err)
		assert.Contains(t, next.err.Error(), "aborted")
		assert.NotNil(t, cmd)
		assert.Nil(t, next.Result())
	})

	t.Run("accepting forces overwrite", func(t *testing.T) {
		m := newTestModel(t, install.InstallOptions{}, true)
		m.step = StepOverwriteConfirm

		next, _ := pressKey(t, m, keyRight())
		next, _ = pressKey(t, next, keyEnter())

		assert.True(t, next.options.Force)
		assert.Equal(t, StepMCPAdvancedConfirm, next.step)
		assert.Len(t, next.history, 1)
	})

	t.Run("non-key message is ignored", func(t *testing.T) {
		m := newTestModel(t, install.InstallOptions{}, true)
		m.step = StepOverwriteConfirm
		next, _ := applyMsg(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
		assert.Equal(t, StepOverwriteConfirm, next.step)
	})
}

func TestHappyPath(t *testing.T) {
	m := newTestModel(t, install.InstallOptions{}, false)

	m, _ = typeText(t, m, "spec.yaml")
	m, _ = pressKey(t, m, keyEnter())
	require.Equal(t, StepBaseURL, m.step)

	m, _ = typeText(t, m, "http://api.example.com")
	m, _ = pressKey(t, m, keyEnter())
	require.Equal(t, StepAuthType, m.step)

	m, _ = pressKey(t, m, keyEnter())
	require.Equal(t, StepLoading, m.step)

	m, _ = applyMsg(t, m, specLoadedMsg{info: &spec.SpecInfo{Title: "Pets", Operations: 3}})
	require.Equal(t, StepDescription, m.step)

	m, _ = typeText(t, m, "petstore")
	m, _ = pressKey(t, m, keyEnter())
	require.Equal(t, StepShim, m.step)

	m, _ = pressKey(t, m, keyEnter())
	require.Equal(t, StepAddHeadersConfirm, m.step)

	m, _ = pressKey(t, m, keyEnter())
	require.Equal(t, StepMCPAdvancedConfirm, m.step)

	m, _ = pressKey(t, m, keyEnter())

	result := m.Result()
	require.NotNil(t, result)
	assert.Equal(t, "spec.yaml", result.SpecSource)
	assert.Equal(t, "http://api.example.com", result.BaseURL)
	assert.Equal(t, "petstore", result.Description)
	assert.Equal(t, "none", result.AuthType)
	assert.True(t, result.CreateShim)
	assert.Empty(t, m.View())
}

func TestLoadSpecCmd(t *testing.T) {
	t.Run("valid spec returns info and server URL", func(t *testing.T) {
		m := newTestModel(t, install.InstallOptions{SpecSource: writeTestSpec(t)}, false)

		msg := m.loadSpecCmd()()
		loaded, ok := msg.(specLoadedMsg)
		require.True(t, ok)
		require.NoError(t, loaded.err)
		require.NotNil(t, loaded.info)
		assert.Equal(t, "TUI Test API", loaded.info.Title)
		assert.Equal(t, 1, loaded.info.Operations)
		assert.Equal(t, "https://api.example.com/v1", loaded.baseURL)
	})

	t.Run("missing spec returns error", func(t *testing.T) {
		m := newTestModel(t, install.InstallOptions{
			SpecSource: filepath.Join(t.TempDir(), "missing.yaml"),
		}, false)

		msg := m.loadSpecCmd()()
		loaded, ok := msg.(specLoadedMsg)
		require.True(t, ok)
		require.Error(t, loaded.err)
		assert.Nil(t, loaded.info)
	})
}

func TestCycleOptionIndex(t *testing.T) {
	tests := []struct {
		name     string
		newIndex int
		maxLen   int
		want     int
	}{
		{name: "negative wraps to last", newIndex: -1, maxLen: 4, want: 3},
		{name: "overflow wraps to first", newIndex: 4, maxLen: 4, want: 0},
		{name: "in range is kept", newIndex: 2, maxLen: 4, want: 2},
		{name: "zero is kept", newIndex: 0, maxLen: 4, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, cycleOptionIndex(tt.newIndex, tt.maxLen))
		})
	}
}

func TestValidateCertAndKeyPair(t *testing.T) {
	tests := []struct {
		name    string
		cert    string
		key     string
		wantErr bool
	}{
		{name: "both empty", wantErr: false},
		{name: "both provided", cert: "cert.pem", key: "key.pem", wantErr: false},
		{name: "cert only", cert: "cert.pem", wantErr: true},
		{name: "key only", key: "key.pem", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCertAndKeyPair(tt.cert, tt.key)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

// TestOptionSelectionIgnoresUnrelatedInput covers the default branches of the
// shared option-selection helper and the empty search-engine list fallback.
func TestOptionSelectionIgnoresUnrelatedInput(t *testing.T) {
	shim := newTestModelAtStep(t, StepShim)
	next, cmd := pressKey(t, shim, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	assert.Nil(t, cmd)
	assert.Equal(t, 0, next.shimIndex)

	engine := newTestModelAtStep(t, StepMCPSearchEngine)
	nextEngine, _ := applyMsg(t, engine, tea.WindowSizeMsg{Width: 80, Height: 24})
	assert.Equal(t, 0, nextEngine.mcpSearchEngineIndex)

	noEngines := newTestModelAtStep(t, StepMCPProgressiveDisclosure)
	noEngines.mcpSearchEngineOptions = nil
	nextNoEngines, _ := pressKey(t, noEngines, keyEnter())
	assert.Equal(t, StepMCPReadOnlyMode, nextNoEngines.step)
	assert.Empty(t, nextNoEngines.options.SearchEngine)
}

// TestRegressionOptionSelectionPersists locks B2: Left/Right selection changes
// must survive across Update calls and Enter must use the newly selected value.
func TestRegressionOptionSelectionPersists(t *testing.T) {
	shim := newTestModelAtStep(t, StepShim)
	shim, _ = pressKey(t, shim, keyRight())
	require.Equal(t, 1, shim.shimIndex)
	shim, _ = pressKey(t, shim, keyEnter())
	assert.False(t, shim.options.CreateShim)

	engine := newTestModelAtStep(t, StepMCPSearchEngine)
	engine.mcpSearchEngineOptions = []string{"predicate", "sql"}
	engine, _ = pressKey(t, engine, keyRight())
	require.Equal(t, 1, engine.mcpSearchEngineIndex)
	engine, _ = pressKey(t, engine, keyEnter())
	assert.Equal(t, "sql", engine.options.SearchEngine)
}

// TestRegressionAutoSelectSingleSearchEngine locks B3: with a single engine the
// value must be persisted on the model, not dropped with a receiver copy.
func TestRegressionAutoSelectSingleSearchEngine(t *testing.T) {
	m := newTestModelAtStep(t, StepMCPProgressiveDisclosure)

	next, _ := pressKey(t, m, keyEnter())

	require.NotNil(t, next.options.ProgressiveDisclosure)
	assert.Equal(t, "predicate", next.options.SearchEngine)
	assert.Equal(t, StepMCPReadOnlyMode, next.step)
}

// TestRegressionEmptyInputDoesNotUsePlaceholder locks B1: pressing Enter on an
// empty field must not copy the rendered placeholder hint into the options.
func TestRegressionEmptyInputDoesNotUsePlaceholder(t *testing.T) {
	specModel := newTestModel(t, install.InstallOptions{}, false)
	require.NotEmpty(t, specModel.specInput.Placeholder)
	nextSpec, _ := pressKey(t, specModel, keyEnter())
	require.Error(t, nextSpec.err)
	assert.Empty(t, nextSpec.options.SpecSource)

	baseModel := reachBaseURLStep(t, install.InstallOptions{SpecSource: "spec.yaml"})
	require.NotEmpty(t, baseModel.baseUrlInput.Placeholder)
	nextBase, _ := pressKey(t, baseModel, keyEnter())
	require.Error(t, nextBase.err)
	assert.Empty(t, nextBase.options.BaseURL)

	descModel := newTestModelAtStep(t, StepDescription)
	descModel.descInput.Placeholder = "hint only"
	nextDesc, _ := pressKey(t, descModel, keyEnter())
	assert.Equal(t, StepShim, nextDesc.step)
	assert.Empty(t, nextDesc.options.Description)
}
