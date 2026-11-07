package install

import (
	"errors"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/stretchr/testify/assert"
)

func TestViewPendingStepContent(t *testing.T) {
	tests := []struct {
		step Step
		want string
	}{
		{step: StepSpecInput, want: "OpenAPI Specification Source"},
		{step: StepBaseURL, want: "Base URL"},
		{step: StepTLSSkipVerify, want: "Skip TLS Certificate Verification"},
		{step: StepTLSCACert, want: "Custom CA Certificate"},
		{step: StepTLSClientCert, want: "TLS Client Certificate"},
		{step: StepAuthType, want: "Authentication Type"},
		{step: StepAuthDetails, want: "Authentication Details"},
		{step: StepLoading, want: "Loading specification"},
		{step: StepDescription, want: "Description"},
		{step: StepShim, want: "Create Shim Executable"},
		{step: StepAddHeadersConfirm, want: "Add Custom HTTP Headers"},
		{step: StepHeaderInput, want: "Custom HTTP Headers"},
		{step: StepMCPAdvancedConfirm, want: "Configure MCP Advanced Options"},
		{step: StepMCPProgressiveDisclosure, want: "Progressive Disclosure"},
		{step: StepMCPSearchEngine, want: "Search Engine"},
		{step: StepMCPReadOnlyMode, want: "Read-Only Mode"},
		{step: StepProtectSensitiveInfo, want: "Protect Sensitive Information"},
		{step: StepOverwriteConfirm, want: "Overwrite?"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			m := newTestModel(t, config.InstallOptions{}, false)
			m.step = tt.step
			if tt.step == StepAuthDetails {
				m.options.AuthType = "basic"
				m.prepareAuthDetails()
			}

			view := m.View()

			assert.Contains(t, view, "Installing demo")
			assert.Contains(t, view, tt.want)
		})
	}
}

func TestViewDoneStepRendersOnlyHeader(t *testing.T) {
	m := newTestModelAtStep(t, StepDone)

	view := m.View()

	assert.Contains(t, view, "Installing demo")
	assert.NotContains(t, view, "OpenAPI Specification Source")
}

func TestViewShowsErrorAndHistory(t *testing.T) {
	m := newTestModel(t, config.InstallOptions{}, false)
	m.err = errors.New("boom")
	m.addHistory("Spec Source", "spec.yaml")

	view := m.View()

	assert.Contains(t, view, "Error: boom")
	assert.Contains(t, view, "Spec Source")
	assert.Contains(t, view, "✔")
}

func TestViewSuppressedWhenFinished(t *testing.T) {
	m := newTestModelAtStep(t, StepProtectSensitiveInfo)
	updated, _ := m.handleProtectSensitiveInfoEnter()
	finished, ok := updated.(Model)
	assert.True(t, ok, "handler must return install.Model")
	assert.NotNil(t, finished.Result())
	assert.Empty(t, finished.View())
}

func TestViewAuthDetailsForEachAuthType(t *testing.T) {
	tests := []struct {
		authType   string
		wantInputs int
	}{
		{authType: "bearer", wantInputs: 1},
		{authType: "basic", wantInputs: 2},
		{authType: "api_key", wantInputs: 2},
		{authType: "none", wantInputs: 0},
	}

	for _, tt := range tests {
		t.Run(tt.authType, func(t *testing.T) {
			m := newTestModelAtStep(t, StepAuthDetails)
			m.options.AuthType = tt.authType
			m.prepareAuthDetails()

			assertions := assert.New(t)
			assertions.Len(m.authInputs, tt.wantInputs)
			assertions.Contains(m.View(), "Authentication Details")
		})
	}
}

func TestMCPAdvancedViewShowsRecommendation(t *testing.T) {
	m := newTestModelAtStep(t, StepMCPAdvancedConfirm)
	m.progressiveRecommendation = "Recommended: 42 operations detected"

	view := m.View()

	assert.Contains(t, view, "Recommended: 42 operations detected")
	assert.Contains(t, view, "Configure MCP Advanced Options")
}

func TestRenderChoice(t *testing.T) {
	m := newTestModel(t, config.InstallOptions{}, false)

	withLabel := m.renderChoice("Pick one:", []string{"No", "Yes"}, 1)
	assert.Contains(t, withLabel, "Pick one:")
	assert.Contains(t, withLabel, "[Yes]")
	assert.Contains(t, withLabel, "No")

	withoutLabel := m.renderChoice("", []string{"First", "Second"}, 0)
	assert.Contains(t, withoutLabel, "[First]")
	assert.Contains(t, withoutLabel, "Second")
	assert.NotContains(t, withoutLabel, "Pick one:")
}

func TestRenderInputFocus(t *testing.T) {
	m := newTestModel(t, config.InstallOptions{}, false)
	input := textinput.New()
	input.SetValue("typed")
	focusedInput := textinput.New()
	focusedInput.SetValue("typed")
	focusedInput.Focus()

	focused := m.renderInput("Label", focusedInput, true)
	unfocused := m.renderInput("Label", input, false)

	assert.True(t, focusedInput.Focused())
	assert.False(t, input.Focused())
	assert.Contains(t, focused, "Label")
	assert.Contains(t, focused, "typed")
	assert.Contains(t, unfocused, "Label")
	assert.Contains(t, unfocused, "typed")
}

func TestResultBeforeFinish(t *testing.T) {
	m := newTestModel(t, config.InstallOptions{}, false)

	assert.Nil(t, m.Result())

	finished, cmd := m.finishInstall()
	assert.NotNil(t, finished.Result())
	assert.NotNil(t, cmd)
}
