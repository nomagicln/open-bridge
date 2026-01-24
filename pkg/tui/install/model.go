package install

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nomagicln/open-bridge/pkg/config"
	"github.com/nomagicln/open-bridge/pkg/spec"
)

// Steps in the wizard
type Step int

const (
	StepSpecInput Step = iota
	StepBaseURL
	StepTLSSkipVerify
	StepTLSCACert
	StepTLSClientCert
	StepAuthType
	StepAuthDetails
	StepLoading
	StepDescription
	StepShim
	StepAddHeadersConfirm
	StepHeaderInput
	StepMCPAdvancedConfirm
	StepMCPProgressiveDisclosure
	StepMCPSearchEngine
	StepMCPReadOnlyMode
	StepProtectSensitiveInfo
	StepOverwriteConfirm
	StepDone
)

// Styles
var (
	focusedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	blurredStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	noStyle      = lipgloss.NewStyle()
	helpStyle    = blurredStyle

	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFF7DB")).
			Background(lipgloss.Color("63")).
			Padding(0, 1)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF0000")).
			Padding(0, 0)

	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("42")) // Green
	questionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
)

type Model struct {
	// State
	step    Step
	appName string
	options config.InstallOptions
	result  *config.InstallOptions
	err     error

	// Flags
	appExists bool

	// History
	history []string

	// Inputs
	specInput    textinput.Model
	descInput    textinput.Model
	baseUrlInput textinput.Model

	// TLS Inputs
	tlsCACertInput     textinput.Model
	tlsClientCertInput textinput.Model
	tlsClientKeyInput  textinput.Model

	// Auth Inputs
	authInputs      []textinput.Model
	authInputLabels []string

	// Headers Input
	headerNameInput  textinput.Model
	headerValueInput textinput.Model
	collectedHeaders map[string]string

	// Choices
	authOptions []string
	authIndex   int
	shimOptions []string // Yes, No
	shimIndex   int

	// Confirm Choice
	confirmOptions    []string
	confirmIndex      int
	addHeadersOptions []string
	addHeadersIndex   int

	// TLS Choices
	tlsSkipVerifyOptions []string
	tlsSkipVerifyIndex   int

	// MCP Choices
	mcpAdvancedOptions        []string
	mcpAdvancedIndex          int
	mcpProgressiveOptions     []string
	mcpProgressiveIndex       int
	mcpSearchEngineOptions    []string
	mcpSearchEngineIndex      int
	mcpReadOnlyOptions        []string
	mcpReadOnlyIndex          int
	progressiveRecommendation string

	// Protect Sensitive Info Choice
	protectSensitiveInfoOptions []string
	protectSensitiveInfoIndex   int

	// Focus index for forms
	focusIndex int

	// Spinner
	spinner spinner.Model

	// Spec Context
	specDoc            *spec.SpecInfo
	defaultBaseURL     string
	defaultDescription string
}

// initializeBasicInputs sets up the basic text input fields (spec, description, baseURL).
func (m *Model) initializeBasicInputs(opts config.InstallOptions) {
	m.specInput = textinput.New()
	m.specInput.Placeholder = "https://example.com/openapi.json or ./spec.yaml"
	m.specInput.Focus()
	m.specInput.CharLimit = 200
	m.specInput.Width = 50
	if opts.SpecSource != "" {
		m.specInput.SetValue(opts.SpecSource)
	}

	m.descInput = textinput.New()
	m.descInput.Placeholder = "Description of the application"
	m.descInput.CharLimit = 100
	m.descInput.Width = 50
	if opts.Description != "" {
		m.defaultDescription = opts.Description
		m.descInput.Placeholder = opts.Description
	}

	m.baseUrlInput = textinput.New()
	m.baseUrlInput.Placeholder = "https://api.example.com"
	m.baseUrlInput.CharLimit = 100
	m.baseUrlInput.Width = 50
	if opts.BaseURL != "" {
		m.defaultBaseURL = opts.BaseURL
		m.baseUrlInput.Placeholder = opts.BaseURL
	}
}

// initializeTLSInputs sets up the TLS certificate input fields.
func (m *Model) initializeTLSInputs(opts config.InstallOptions) {
	m.tlsCACertInput = textinput.New()
	m.tlsCACertInput.Placeholder = "/path/to/ca.pem (optional)"
	m.tlsCACertInput.CharLimit = 200
	m.tlsCACertInput.Width = 50
	if opts.TLSCACert != "" {
		m.tlsCACertInput.SetValue(opts.TLSCACert)
	}

	m.tlsClientCertInput = textinput.New()
	m.tlsClientCertInput.Placeholder = "/path/to/client.pem (optional)"
	m.tlsClientCertInput.CharLimit = 200
	m.tlsClientCertInput.Width = 50
	if opts.TLSClientCert != "" {
		m.tlsClientCertInput.SetValue(opts.TLSClientCert)
	}

	m.tlsClientKeyInput = textinput.New()
	m.tlsClientKeyInput.Placeholder = "/path/to/client-key.pem (optional)"
	m.tlsClientKeyInput.CharLimit = 200
	m.tlsClientKeyInput.Width = 50
	if opts.TLSClientKey != "" {
		m.tlsClientKeyInput.SetValue(opts.TLSClientKey)
	}
}

// initializeHeaderInputs sets up the custom header input fields.
func (m *Model) initializeHeaderInputs() {
	m.headerNameInput = textinput.New()
	m.headerNameInput.Placeholder = "Header Name"
	m.headerNameInput.Width = 30
	m.headerNameInput.CharLimit = 100

	m.headerValueInput = textinput.New()
	m.headerValueInput.Placeholder = "Value"
	m.headerValueInput.Width = 30
	m.headerValueInput.CharLimit = 500
}

// initializeTextInputs sets up all text input fields for the model.
func (m *Model) initializeTextInputs(opts config.InstallOptions) {
	m.initializeBasicInputs(opts)
	m.initializeTLSInputs(opts)
	m.initializeHeaderInputs()
}

// initializeSpinner sets up the loading spinner.
func (m *Model) initializeSpinner() {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	m.spinner = s
}

func NewModel(appName string, opts config.InstallOptions, appExists bool) Model {
	m := Model{
		step:                        StepSpecInput,
		appName:                     appName,
		options:                     opts,
		appExists:                   appExists,
		collectedHeaders:            make(map[string]string),
		authOptions:                 []string{"none", "bearer", "api_key", "basic"},
		shimOptions:                 []string{"Yes", "No"},
		confirmOptions:              []string{"No", "Yes"},
		addHeadersOptions:           []string{"No", "Yes"},
		tlsSkipVerifyOptions:        []string{"No", "Yes"},
		mcpAdvancedOptions:          []string{"Skip", "Configure"},
		mcpProgressiveOptions:       []string{"No", "Yes"},
		mcpSearchEngineOptions:      []string{"predicate"},
		mcpReadOnlyOptions:          []string{"No", "Yes"},
		protectSensitiveInfoOptions: []string{"No", "Yes"},
		history:                     []string{},
	}

	m.initializeTextInputs(opts)
	m.initializeSpinner()

	if opts.SpecSource != "" || len(opts.SpecSources) > 0 {
		m.step = StepBaseURL
		m.specInput.Blur()
		m.baseUrlInput.Focus()
	}

	if opts.AuthType != "" {
		for i, opt := range m.authOptions {
			if opt == opts.AuthType {
				m.authIndex = i
				break
			}
		}
	}

	// Set TLS skip verify from options
	if opts.TLSSkipVerify {
		m.tlsSkipVerifyIndex = 1 // Yes
	}

	return m
}

func (m Model) Init() tea.Cmd {
	if m.step == StepLoading {
		return tea.Batch(m.spinner.Tick, m.loadSpecCmd())
	}
	return textinput.Blink
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if keyMsg.String() == "ctrl+c" {
			return m, tea.Quit
		}
	}

	return m.handleStepUpdate(msg)
}

func (m Model) handleStepUpdate(msg tea.Msg) (tea.Model, tea.Cmd) { //nolint:funlen // Step routing requires many cases
	switch m.step {
	case StepSpecInput:
		return m.updateSpecInput(msg)
	case StepBaseURL:
		return m.updateBaseURL(msg)
	case StepTLSSkipVerify:
		return m.updateTLSSkipVerify(msg)
	case StepTLSCACert:
		return m.updateTLSCACert(msg)
	case StepTLSClientCert:
		return m.updateTLSClientCert(msg)
	case StepAuthType:
		return m.updateAuthType(msg)
	case StepAuthDetails:
		return m.updateAuthDetails(msg)
	case StepLoading:
		return m.updateLoading(msg)
	case StepDescription:
		return m.updateDescription(msg)
	case StepShim:
		return m.updateShim(msg)
	case StepAddHeadersConfirm:
		return m.updateAddHeadersConfirm(msg)
	case StepHeaderInput:
		return m.updateHeaderInput(msg)
	case StepMCPAdvancedConfirm:
		return m.updateMCPAdvancedConfirm(msg)
	case StepMCPProgressiveDisclosure:
		return m.updateMCPProgressiveDisclosure(msg)
	case StepMCPSearchEngine:
		return m.updateMCPSearchEngine(msg)
	case StepMCPReadOnlyMode:
		return m.updateMCPReadOnlyMode(msg)
	case StepProtectSensitiveInfo:
		return m.updateProtectSensitiveInfo(msg)
	case StepOverwriteConfirm:
		return m.updateOverwriteConfirm(msg)
	}

	return m, nil
}

// renderTextInputStep renders a text input step with question and hint.
func renderTextInputStep(s *strings.Builder, question, inputView string) {
	s.WriteString(questionStyle.Render(question))
	s.WriteString("\n")
	s.WriteString(inputView)
	s.WriteString("\n\n(Press Enter to continue)")
}

// renderTextInputStepWithHint renders a text input step with custom hint.
func renderTextInputStepWithHint(s *strings.Builder, question, inputView, hint string) {
	s.WriteString(questionStyle.Render(question))
	s.WriteString("\n")
	s.WriteString(inputView)
	s.WriteString("\n\n")
	s.WriteString(helpStyle.Render(hint))
}

// renderChoiceStep renders a choice selection step.
func (m Model) renderChoiceStep(s *strings.Builder, question string, choices []string, selectedIdx int) {
	s.WriteString(questionStyle.Render(question))
	s.WriteString("\n")
	s.WriteString(m.renderChoice("", choices, selectedIdx))
	s.WriteString("\n\n(Left/Right to select, Enter to confirm)")
}

// renderStepContent renders the content for the current step.
func (m Model) renderStepContent(s *strings.Builder) {
	switch m.step {
	case StepSpecInput:
		renderTextInputStep(s, "? OpenAPI Specification Source:", m.specInput.View())
	case StepBaseURL:
		renderTextInputStep(s, "? Base URL:", m.baseUrlInput.View())
	case StepTLSSkipVerify:
		m.renderChoiceStep(s, "? Skip TLS Certificate Verification (insecure):", m.tlsSkipVerifyOptions, m.tlsSkipVerifyIndex)
	case StepTLSCACert:
		renderTextInputStepWithHint(s, "? Custom CA Certificate (optional):", m.tlsCACertInput.View(), "(Leave empty to skip, Enter to continue)")
	case StepTLSClientCert:
		m.renderTLSClientCertStep(s)
	case StepAuthType:
		m.renderChoiceStep(s, "? Authentication Type:", m.authOptions, m.authIndex)
	case StepAuthDetails:
		m.renderAuthDetailsStep(s)
	case StepLoading:
		fmt.Fprintf(s, "%s Loading specification...", m.spinner.View())
	case StepDescription:
		renderTextInputStep(s, "? Description:", m.descInput.View())
	case StepShim:
		m.renderChoiceStep(s, "? Create Shim Executable:", m.shimOptions, m.shimIndex)
	case StepAddHeadersConfirm:
		m.renderAddHeadersConfirmStep(s)
	case StepHeaderInput:
		m.renderHeaderInputStep(s)
	case StepMCPAdvancedConfirm:
		m.renderMCPAdvancedConfirmStep(s)
	case StepMCPProgressiveDisclosure:
		m.renderMCPProgressiveStep(s)
	case StepMCPSearchEngine:
		m.renderChoiceStep(s, "? Search Engine for Progressive Disclosure:", m.mcpSearchEngineOptions, m.mcpSearchEngineIndex)
	case StepMCPReadOnlyMode:
		m.renderChoiceStep(s, "? Enable Read-Only Mode (GET operations only):", m.mcpReadOnlyOptions, m.mcpReadOnlyIndex)
	case StepProtectSensitiveInfo:
		m.renderChoiceStep(s, "? Protect Sensitive Information (mask API keys in generated code):", m.protectSensitiveInfoOptions, m.protectSensitiveInfoIndex)
	case StepOverwriteConfirm:
		m.renderOverwriteConfirmStep(s)
	default:
		// StepDone or unknown step - nothing to render
	}
}

// renderAddHeadersConfirmStep renders the add headers confirm step.
func (m Model) renderAddHeadersConfirmStep(s *strings.Builder) {
	s.WriteString(questionStyle.Render("? Add Custom HTTP Headers:"))
	s.WriteString("\n")
	s.WriteString(m.renderChoice("", m.addHeadersOptions, m.addHeadersIndex))
	s.WriteString("\n\n")
}

// renderOverwriteConfirmStep renders the overwrite confirm step.
func (m Model) renderOverwriteConfirmStep(s *strings.Builder) {
	fmt.Fprintf(s, "App '%s' already exists.\n\n", m.appName)
	s.WriteString(m.renderChoice("Overwrite?", m.confirmOptions, m.confirmIndex))
	s.WriteString("\n\n")
}

// renderDualInputStep renders a step with two input fields.
func (m Model) renderDualInputStep(s *strings.Builder, title, label1, label2, help string, input1, input2 textinput.Model) {
	s.WriteString(questionStyle.Render(title))
	s.WriteString("\n")
	s.WriteString(m.renderInput(label1, input1, m.focusIndex == 0))
	s.WriteString("\n")
	s.WriteString(m.renderInput(label2, input2, m.focusIndex == 1))
	s.WriteString("\n\n")
	s.WriteString(helpStyle.Render(help))
}

// renderTLSClientCertStep renders the TLS client certificate step.
func (m Model) renderTLSClientCertStep(s *strings.Builder) {
	m.renderDualInputStep(s,
		"? TLS Client Certificate & Key (optional):",
		"Client Certificate", "Client Key",
		"(Tab to switch, Enter to continue, leave empty to skip)",
		m.tlsClientCertInput, m.tlsClientKeyInput)
}

// renderMCPAdvancedConfirmStep renders the MCP advanced options confirm step.
func (m Model) renderMCPAdvancedConfirmStep(s *strings.Builder) {
	s.WriteString(questionStyle.Render("? Configure MCP Advanced Options:"))
	s.WriteString("\n")
	if m.progressiveRecommendation != "" {
		s.WriteString(blurredStyle.Render(m.progressiveRecommendation))
		s.WriteString("\n")
	}
	s.WriteString(m.renderChoice("", m.mcpAdvancedOptions, m.mcpAdvancedIndex))
	s.WriteString("\n\n(Left/Right to select, Enter to confirm)")
}

// renderMCPProgressiveStep renders the MCP progressive disclosure step.
func (m Model) renderMCPProgressiveStep(s *strings.Builder) {
	question := "? Enable Progressive Disclosure:"
	if m.progressiveRecommendation != "" {
		question = fmt.Sprintf("? Enable Progressive Disclosure (%s):", m.progressiveRecommendation)
	}
	m.renderChoiceStep(s, question, m.mcpProgressiveOptions, m.mcpProgressiveIndex)
}

// renderAuthDetailsStep renders the auth details step.
func (m Model) renderAuthDetailsStep(s *strings.Builder) {
	s.WriteString(questionStyle.Render(fmt.Sprintf("? Authentication Details (%s):", m.options.AuthType)))
	s.WriteString("\n")
	for i, input := range m.authInputs {
		s.WriteString(m.renderInput(m.authInputLabels[i], input, m.focusIndex == i))
		s.WriteString("\n")
	}
	s.WriteString(helpStyle.Render("(Tab/Shift+Tab to navigate, Enter to continue)"))
}

// renderHeaderInputStep renders the header input step.
func (m Model) renderHeaderInputStep(s *strings.Builder) {
	m.renderDualInputStep(s,
		"? Custom HTTP Headers:",
		"Header Name (Leave empty to finish)", "Header Value",
		"(Enter to add/next)",
		m.headerNameInput, m.headerValueInput)
}

func (m Model) View() string {
	if m.result != nil {
		return ""
	}

	var s strings.Builder
	s.WriteString(titleStyle.Render(fmt.Sprintf(" Installing %s ", m.appName)))
	s.WriteString("\n\n")

	if m.err != nil {
		s.WriteString(errorStyle.Render(fmt.Sprintf("Error: %v", m.err)))
		s.WriteString("\n\n")
	}

	for _, item := range m.history {
		s.WriteString(item)
		s.WriteString("\n")
	}
	if len(m.history) > 0 {
		s.WriteString("\n")
	}

	m.renderStepContent(&s)
	return s.String()
}

// Update helpers

func (m *Model) addHistory(label, value string) {
	m.history = append(m.history, fmt.Sprintf("%s %s: %s", successStyle.Render("✔"), label, value))
}

func (m Model) updateSpecInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEnter {
		val := m.specInput.Value()
		if val == "" {
			m.err = fmt.Errorf("spec source cannot be empty")
			return m, nil
		}
		m.options.SpecSource = val
		m.err = nil
		m.addHistory("Spec Source", val)
		m.step = StepBaseURL
		m.baseUrlInput.Focus()
		return m, textinput.Blink
	}

	var cmd tea.Cmd
	m.specInput, cmd = m.specInput.Update(msg)
	return m, cmd
}

func (m Model) updateLoading(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case specLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.step = StepSpecInput
			return m, nil
		}

		m.addHistory("Loaded Spec", fmt.Sprintf("%s (%d operations)", msg.info.Title, msg.info.Operations))
		m.specDoc = msg.info
		if m.defaultDescription == "" && m.specDoc.Title != "" {
			m.defaultDescription = m.specDoc.Title
			m.descInput.Placeholder = m.specDoc.Title
		}

		// Set progressive disclosure recommendation based on operation count
		threshold := m.options.ProgressiveThreshold
		if threshold <= 0 {
			threshold = config.DefaultProgressiveThreshold
		}
		if msg.info.Operations > threshold {
			m.progressiveRecommendation = fmt.Sprintf("Recommended: %d operations detected", msg.info.Operations)
			m.mcpProgressiveIndex = 1 // Default to Yes
			m.mcpAdvancedIndex = 1    // Default to Configure
		} else {
			m.progressiveRecommendation = fmt.Sprintf("%d operations detected", msg.info.Operations)
		}

		// Continue to description step
		m.step = StepDescription
		m.descInput.Focus()
		m.err = nil
		return m, textinput.Blink
	}
	return m, nil
}

func (m Model) updateDescription(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEnter {
		val := m.descInput.Value()
		if val == "" && m.defaultDescription != "" {
			val = m.defaultDescription
		}
		m.options.Description = val
		m.addHistory("Description", val)
		m.step = StepShim
		return m, nil
	}
	var cmd tea.Cmd
	m.descInput, cmd = m.descInput.Update(msg)
	return m, cmd
}

func (m Model) updateBaseURL(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEnter {
		val := m.baseUrlInput.Value()
		if val == "" && m.defaultBaseURL != "" {
			val = m.defaultBaseURL
		}

		// Validate URL if not empty
		if val != "" {
			u, err := url.Parse(val)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				m.err = fmt.Errorf("invalid Base URL: must start with http:// or https:// and have a host")
				return m, nil
			}
		}

		m.options.BaseURL = val
		m.err = nil
		if val != "" {
			m.addHistory("Base URL", val)
		}

		// Only show TLS skip verify option if HTTPS is involved (spec URL or base URL)
		if m.needsTLSConfiguration() {
			m.step = StepTLSSkipVerify
		} else {
			// Skip TLS configuration steps for non-HTTPS URLs
			m.step = StepAuthType
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.baseUrlInput, cmd = m.baseUrlInput.Update(msg)
	return m, cmd
}

// needsTLSConfiguration checks if TLS configuration is needed.
// Returns true if either the spec source or base URL uses HTTPS.
func (m *Model) needsTLSConfiguration() bool {
	// Check spec source
	if strings.HasPrefix(strings.ToLower(m.options.SpecSource), "https://") {
		return true
	}
	// Check base URL
	if strings.HasPrefix(strings.ToLower(m.options.BaseURL), "https://") {
		return true
	}
	return false
}

// updateTLSSkipVerify handles the TLS skip verify step.
func (m Model) updateTLSSkipVerify(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "left", "h":
			m.tlsSkipVerifyIndex = 0
		case "right", "l":
			m.tlsSkipVerifyIndex = 1
		case "enter":
			m.options.TLSSkipVerify = (m.tlsSkipVerifyIndex == 1)
			if m.options.TLSSkipVerify {
				m.addHistory("TLS Skip Verify", "Yes (insecure)")
			}
			m.step = StepTLSCACert
			m.tlsCACertInput.Focus()
			return m, textinput.Blink
		default:
			// Ignore other keys
		}
	}
	return m, nil
}

// updateTLSCACert handles the TLS CA certificate step.
func (m Model) updateTLSCACert(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.Type == tea.KeyEnter {
		val := m.tlsCACertInput.Value()
		if val != "" {
			// Validate path
			absPath, err := config.ValidateTLSCACertFile(val)
			if err != nil {
				m.err = fmt.Errorf("invalid CA certificate: %w", err)
				return m, nil
			}
			m.options.TLSCACert = absPath
			m.addHistory("TLS CA Cert", absPath)
		}
		m.err = nil
		m.step = StepTLSClientCert
		m.focusIndex = 0
		m.tlsClientCertInput.Focus()
		return m, textinput.Blink
	}
	var cmd tea.Cmd
	m.tlsCACertInput, cmd = m.tlsCACertInput.Update(msg)
	return m, cmd
}

// validateTLSClientCert validates and sets the TLS client certificate and key.
func (m *Model) validateTLSClientCert() error {
	certVal := m.tlsClientCertInput.Value()
	keyVal := m.tlsClientKeyInput.Value()

	// Both must be provided or both empty
	if (certVal != "" && keyVal == "") || (certVal == "" && keyVal != "") {
		return fmt.Errorf("client certificate and key must be provided together")
	}

	if certVal == "" && keyVal == "" {
		return nil // Both empty is valid (skip)
	}

	// Validate certificate
	absPath, err := config.ValidateTLSCertFile(certVal)
	if err != nil {
		return fmt.Errorf("invalid client certificate: %w", err)
	}
	m.options.TLSClientCert = absPath

	// Validate key
	absKeyPath, err := config.ValidateTLSKeyFile(keyVal)
	if err != nil {
		return fmt.Errorf("invalid client key: %w", err)
	}
	m.options.TLSClientKey = absKeyPath
	m.addHistory("TLS Client Cert", absPath)
	m.addHistory("TLS Client Key", absKeyPath)
	return nil
}

// updateTLSClientCert handles the TLS client certificate step.
func (m Model) updateTLSClientCert(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m.updateTLSClientCertInput(msg)
	}

	switch keyMsg.Type {
	case tea.KeyEnter:
		if err := m.validateTLSClientCert(); err != nil {
			m.err = err
			return m, nil
		}
		m.err = nil
		m.step = StepAuthType
		return m, nil
	case tea.KeyTab, tea.KeyShiftTab:
		return m.toggleTLSClientInputFocus()
	default:
		return m.updateTLSClientCertInput(msg)
	}
}

// toggleTLSClientInputFocus toggles focus between cert and key inputs.
func (m Model) toggleTLSClientInputFocus() (tea.Model, tea.Cmd) {
	if m.focusIndex == 0 {
		m.focusIndex = 1
		m.tlsClientCertInput.Blur()
		m.tlsClientKeyInput.Focus()
	} else {
		m.focusIndex = 0
		m.tlsClientKeyInput.Blur()
		m.tlsClientCertInput.Focus()
	}
	return m, textinput.Blink
}

// updateTLSClientCertInput updates the focused TLS client cert input.
func (m Model) updateTLSClientCertInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.focusIndex == 0 {
		m.tlsClientCertInput, cmd = m.tlsClientCertInput.Update(msg)
	} else {
		m.tlsClientKeyInput, cmd = m.tlsClientKeyInput.Update(msg)
	}
	return m, cmd
}

func (m Model) updateAuthType(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}

	switch keyMsg.String() {
	case "left", "h":
		m.authIndex--
		if m.authIndex < 0 {
			m.authIndex = len(m.authOptions) - 1
		}
	case "right", "l":
		m.authIndex++
		if m.authIndex >= len(m.authOptions) {
			m.authIndex = 0
		}
	case "enter":
		return m.handleAuthTypeEnter()
	default:
		// Ignore other keys
	}
	return m, nil
}

// handleAuthTypeEnter handles the enter key press in auth type selection.
func (m Model) handleAuthTypeEnter() (tea.Model, tea.Cmd) {
	val := m.authOptions[m.authIndex]
	m.options.AuthType = val
	m.addHistory("Auth Type", val)

	if val != "none" {
		m.prepareAuthDetails()
		if len(m.authInputs) > 0 {
			m.step = StepAuthDetails
			m.focusIndex = 0
			return m, textinput.Blink
		}
	}

	// Start loading spec after auth configuration
	m.step = StepLoading
	return m, tea.Batch(m.spinner.Tick, m.loadSpecCmd())
}

func (m Model) updateShim(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "left", "h":
			m.shimIndex--
			if m.shimIndex < 0 {
				m.shimIndex = len(m.shimOptions) - 1
			}
		case "right", "l":
			m.shimIndex++
			if m.shimIndex >= len(m.shimOptions) {
				m.shimIndex = 0
			}
		case "enter":
			m.options.CreateShim = (m.shimIndex == 0) // Yes is 0
			m.addHistory("Create Shim", m.shimOptions[m.shimIndex])

			m.step = StepAddHeadersConfirm
			m.addHeadersIndex = 0
			return m, nil
		default:
			// Ignore other keys
		}
	}
	return m, nil
}

func (m *Model) prepareAuthDetails() {
	// ... (Same logic)
	m.authInputs = []textinput.Model{}
	m.authInputLabels = []string{}

	switch m.options.AuthType {
	case "bearer":
		ti := textinput.New()
		ti.Placeholder = "Bearer Token"
		ti.Focus()
		ti.CharLimit = 500
		ti.EchoMode = textinput.EchoPassword
		m.authInputs = append(m.authInputs, ti)
		m.authInputLabels = append(m.authInputLabels, "Token")

	case "basic":
		tiUser := textinput.New()
		tiUser.Placeholder = "Username"
		tiUser.Focus()
		m.authInputs = append(m.authInputs, tiUser)
		m.authInputLabels = append(m.authInputLabels, "Username")

		tiPass := textinput.New()
		tiPass.Placeholder = "Password"
		tiPass.EchoMode = textinput.EchoPassword
		m.authInputs = append(m.authInputs, tiPass)
		m.authInputLabels = append(m.authInputLabels, "Password")

	case "api_key":
		tiName := textinput.New()
		tiName.Placeholder = "X-API-Key"
		tiName.SetValue("X-API-Key")
		tiName.Focus()
		m.authInputs = append(m.authInputs, tiName)
		m.authInputLabels = append(m.authInputLabels, "Header Name")

		tiValue := textinput.New()
		tiValue.Placeholder = "Key Value"
		tiValue.EchoMode = textinput.EchoPassword
		m.authInputs = append(m.authInputs, tiValue)
		m.authInputLabels = append(m.authInputLabels, "Key Value")

	default:
		// "none" or unknown auth type - no inputs needed
	}
}

// collectAuthParams collects auth parameters based on auth type.
func (m *Model) collectAuthParams() {
	m.options.AuthParams = make(map[string]string)
	switch m.options.AuthType {
	case "bearer":
		m.options.AuthParams["token"] = m.authInputs[0].Value()
	case "basic":
		m.options.AuthParams["username"] = m.authInputs[0].Value()
		m.options.AuthParams["password"] = m.authInputs[1].Value()
	case "api_key":
		m.options.AuthParams["key_name"] = m.authInputs[0].Value()
		m.options.AuthParams["token"] = m.authInputs[1].Value()
	default:
		// "none" or unknown auth type - no params to collect
	}
}

// handleAuthNavigation handles navigation between auth input fields.
func (m *Model) handleAuthNavigation(key string) {
	switch key {
	case "up", "shift+tab":
		m.focusIndex--
		if m.focusIndex < 0 {
			m.focusIndex = len(m.authInputs) - 1
		}
	default: // down, tab
		m.focusIndex++
		if m.focusIndex >= len(m.authInputs) {
			m.focusIndex = 0
		}
	}
}

// updateAuthInputFocus updates focus state for auth inputs.
func (m *Model) updateAuthInputFocus() tea.Cmd {
	cmds := make([]tea.Cmd, len(m.authInputs))
	for i := range m.authInputs {
		if i == m.focusIndex {
			cmds[i] = m.authInputs[i].Focus()
		} else {
			m.authInputs[i].Blur()
		}
	}
	return tea.Batch(cmds...)
}

func (m Model) updateAuthDetails(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m.updateAuthInput(msg)
	}

	switch keyMsg.String() {
	case "enter":
		if m.focusIndex == len(m.authInputs)-1 {
			m.collectAuthParams()
			m.addHistory("Auth Details", "Provided")
			// Start loading spec after auth details
			m.step = StepLoading
			return m, tea.Batch(m.spinner.Tick, m.loadSpecCmd())
		}
		m.focusIndex++
		return m, m.updateAuthInputFocus()
	case "up", "shift+tab", "down", "tab":
		m.handleAuthNavigation(keyMsg.String())
		return m, m.updateAuthInputFocus()
	default:
		return m.updateAuthInput(msg)
	}
}

// updateAuthInput updates the current auth input field.
func (m Model) updateAuthInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.focusIndex >= 0 && m.focusIndex < len(m.authInputs) {
		var cmd tea.Cmd
		m.authInputs[m.focusIndex], cmd = m.authInputs[m.focusIndex].Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) updateAddHeadersConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "left", "h":
			m.addHeadersIndex = 0
		case "right", "l":
			m.addHeadersIndex = 1
		case "enter":
			if m.addHeadersIndex == 1 { // Yes
				m.step = StepHeaderInput
				m.headerNameInput.Focus()
				m.headerNameInput.SetValue("")
				m.headerValueInput.SetValue("")
				m.focusIndex = 0
				return m, textinput.Blink
			} else {
				m.addHistory("Add Custom Headers", "No")
				return m.checkOverwriteOrFinish()
			}
		default:
			// Ignore other keys
		}
	}
	return m, nil
}

func (m Model) updateHeaderInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !ok {
		return m.updateHeaderInputFields(msg)
	}

	switch keyMsg.String() {
	case "enter":
		return m.handleHeaderInputEnter()
	case "tab", "shift+tab":
		return m.toggleHeaderInputFocus()
	default:
		return m.updateHeaderInputFields(msg)
	}
}

// handleHeaderInputEnter handles enter key in header input step.
func (m Model) handleHeaderInputEnter() (tea.Model, tea.Cmd) {
	if m.focusIndex == 0 {
		// Name input - check if empty (done) or go to value
		if m.headerNameInput.Value() == "" {
			return m.checkOverwriteOrFinish()
		}
		m.focusIndex = 1
		m.headerNameInput.Blur()
		return m, m.headerValueInput.Focus()
	}

	// Value input - add the header pair and reset for next
	key := strings.TrimSpace(m.headerNameInput.Value())
	val := strings.TrimSpace(m.headerValueInput.Value())
	if key != "" {
		m.collectedHeaders[key] = val
		m.addHistory("Header", fmt.Sprintf("%s: %s", key, val))
	}

	m.headerNameInput.SetValue("")
	m.headerValueInput.SetValue("")
	m.focusIndex = 0
	m.headerValueInput.Blur()
	return m, m.headerNameInput.Focus()
}

// toggleHeaderInputFocus toggles focus between header name and value inputs.
func (m Model) toggleHeaderInputFocus() (tea.Model, tea.Cmd) {
	if m.focusIndex == 0 {
		m.focusIndex = 1
		m.headerNameInput.Blur()
		return m, m.headerValueInput.Focus()
	}
	m.focusIndex = 0
	m.headerValueInput.Blur()
	return m, m.headerNameInput.Focus()
}

// updateHeaderInputFields updates the header input fields.
func (m Model) updateHeaderInputFields(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	if m.focusIndex == 0 {
		m.headerNameInput, cmd = m.headerNameInput.Update(msg)
	} else {
		m.headerValueInput, cmd = m.headerValueInput.Update(msg)
	}
	return m, cmd
}

// checkOverwriteOrFinish checks if app exists and needs overwrite confirmation,
// or proceeds to MCP advanced options. The tea.Cmd return is always nil but
// kept for consistency with the Update pattern.
//
//nolint:unparam // tea.Cmd is kept for Update pattern consistency
func (m Model) checkOverwriteOrFinish() (Model, tea.Cmd) {
	if m.appExists && !m.options.Force {
		m.step = StepOverwriteConfirm
		m.confirmIndex = 0 // Default No
		return m, nil
	}
	// Go to MCP advanced options
	m.step = StepMCPAdvancedConfirm
	m.mcpAdvancedIndex = 0 // Default to No (skip)
	return m, nil
}

// finishInstall completes the installation process.
func (m Model) finishInstall() (Model, tea.Cmd) {
	m.result = &m.options
	m.options.Headers = m.collectedHeaders // Save headers
	return m, tea.Quit
}

// updateMCPAdvancedConfirm handles the MCP advanced configuration confirm step.
func (m Model) updateMCPAdvancedConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "left", "h":
			m.mcpAdvancedIndex = 0
		case "right", "l":
			m.mcpAdvancedIndex = 1
		case "enter":
			if m.mcpAdvancedIndex == 1 { // Yes - configure MCP
				m.step = StepMCPProgressiveDisclosure
				return m, nil
			}
			// No - skip MCP config, use defaults
			m.addHistory("MCP Advanced Options", "Skipped (using defaults)")
			return m.finishInstall()
		default:
			// Ignore other keys
		}
	}
	return m, nil
}

// updateMCPProgressiveDisclosure handles the progressive disclosure configuration step.
func (m Model) updateMCPProgressiveDisclosure(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "left", "h":
			m.mcpProgressiveIndex = 0
		case "right", "l":
			m.mcpProgressiveIndex = 1
		case "enter":
			return m.confirmMCPProgressiveDisclosure()
		default:
			// Ignore other keys
		}
	}
	return m, nil
}

// confirmMCPProgressiveDisclosure processes the progressive disclosure selection and determines the next step.
func (m Model) confirmMCPProgressiveDisclosure() (tea.Model, tea.Cmd) {
	enabled := (m.mcpProgressiveIndex == 1) // Yes is 1
	m.options.ProgressiveDisclosure = &enabled
	if enabled {
		m.addHistory("Progressive Disclosure", "Enabled")
	} else {
		m.addHistory("Progressive Disclosure", "Disabled")
	}
	// Only show search engine selection if there are >= 2 options
	if len(m.mcpSearchEngineOptions) >= 2 {
		m.step = StepMCPSearchEngine
		return m, nil
	}
	// Auto-select the first (and only) search engine if available
	if len(m.mcpSearchEngineOptions) == 1 {
		m.options.SearchEngine = m.mcpSearchEngineOptions[0]
	}
	m.step = StepMCPReadOnlyMode
	return m, nil
}

// updateMCPSearchEngine handles the search engine configuration step.
func (m Model) updateMCPSearchEngine(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "left", "h":
			m.mcpSearchEngineIndex--
			if m.mcpSearchEngineIndex < 0 {
				m.mcpSearchEngineIndex = len(m.mcpSearchEngineOptions) - 1
			}
		case "right", "l":
			m.mcpSearchEngineIndex++
			if m.mcpSearchEngineIndex >= len(m.mcpSearchEngineOptions) {
				m.mcpSearchEngineIndex = 0
			}
		case "enter":
			m.options.SearchEngine = m.mcpSearchEngineOptions[m.mcpSearchEngineIndex]
			m.addHistory("Search Engine", m.options.SearchEngine)
			m.step = StepMCPReadOnlyMode
			return m, nil
		default:
			// Ignore other keys
		}
	}
	return m, nil
}

// updateMCPReadOnlyMode handles the read-only mode configuration step.
func (m Model) updateMCPReadOnlyMode(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "left", "h":
			m.mcpReadOnlyIndex = 0
		case "right", "l":
			m.mcpReadOnlyIndex = 1
		case "enter":
			m.options.ReadOnlyMode = (m.mcpReadOnlyIndex == 1) // Yes is 1
			if m.options.ReadOnlyMode {
				m.addHistory("Read-Only Mode", "Enabled")
			} else {
				m.addHistory("Read-Only Mode", "Disabled")
			}
			m.step = StepProtectSensitiveInfo
		default:
			// Ignore other keys
		}
	}
	return m, nil
}

func (m Model) updateProtectSensitiveInfo(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "left", "h":
			m.protectSensitiveInfoIndex = 0
		case "right", "l":
			m.protectSensitiveInfoIndex = 1
		case "enter":
			m.options.ProtectSensitiveInfo = (m.protectSensitiveInfoIndex == 1) // Yes is 1
			if m.options.ProtectSensitiveInfo {
				m.addHistory("Protect Sensitive Info", "Enabled")
			} else {
				m.addHistory("Protect Sensitive Info", "Disabled")
			}
			return m.finishInstall()
		default:
			// Ignore other keys
		}
	}
	return m, nil
}

func (m Model) updateOverwriteConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		switch keyMsg.String() {
		case "left", "h":
			m.confirmIndex = 0
		case "right", "l":
			m.confirmIndex = 1
		case "enter":
			if m.confirmIndex == 1 { // Yes
				m.options.Force = true
				m.addHistory("Overwrite App", "Yes")
				// Go to MCP advanced options
				m.step = StepMCPAdvancedConfirm
				m.mcpAdvancedIndex = 0 // Default to No (skip)
				return m, nil
			} else {
				m.err = fmt.Errorf("installation aborted by user")
				return m, tea.Quit
			}
		default:
			// Ignore other keys
		}
	}
	return m, nil
}

// ... (specLoadedMsg and loadSpecCmd are same)
type specLoadedMsg struct {
	info    *spec.SpecInfo
	baseURL string
	err     error
}

func (m Model) loadSpecCmd() tea.Cmd {
	return func() tea.Msg {
		parser := spec.NewParser()
		specDoc, err := parser.LoadSpec(m.options.SpecSource)
		if err != nil {
			return specLoadedMsg{err: err}
		}

		info := spec.GetSpecInfo(specDoc, m.options.SpecSource)

		baseURL := ""
		if len(specDoc.Servers) > 0 {
			baseURL = specDoc.Servers[0].URL
		}

		return specLoadedMsg{info: info, baseURL: baseURL, err: nil}
	}
}

// View helpers
func (m Model) renderInput(label string, input textinput.Model, focused bool) string {
	var s strings.Builder
	style := noStyle
	if focused {
		style = focusedStyle
	}
	s.WriteString(style.Render(label))
	s.WriteString("\n")
	s.WriteString(input.View())
	return s.String()
}

func (m Model) renderChoice(label string, choices []string, selectedIdx int) string {
	var s strings.Builder
	style := focusedStyle
	if label != "" {
		s.WriteString(style.Render(label))
		s.WriteString("\n")
	}

	for i, choice := range choices {
		if i > 0 {
			s.WriteString("  ")
		}
		if i == selectedIdx {
			fmt.Fprintf(&s, "[%s]", choice)
		} else {
			s.WriteString(choice)
		}
	}
	return s.String()
}

func (m Model) Result() *config.InstallOptions {
	return m.result
}
