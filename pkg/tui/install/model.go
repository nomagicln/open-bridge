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
	StepLoading
	StepDescription
	StepBaseURL
	StepAuthType
	StepShim
	StepAuthDetails
	StepAddHeadersConfirm
	StepHeaderInput
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

	// Focus index for forms
	focusIndex int

	// Spinner
	spinner spinner.Model

	// Spec Context
	specDoc            *spec.SpecInfo
	defaultBaseURL     string
	defaultDescription string
}

// initializeTextInputs sets up all text input fields for the model.
func (m *Model) initializeTextInputs(opts config.InstallOptions) {
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

	m.headerNameInput = textinput.New()
	m.headerNameInput.Placeholder = "Header Name"
	m.headerNameInput.Width = 30
	m.headerNameInput.CharLimit = 100

	m.headerValueInput = textinput.New()
	m.headerValueInput.Placeholder = "Value"
	m.headerValueInput.Width = 30
	m.headerValueInput.CharLimit = 500
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
		step:              StepSpecInput,
		appName:           appName,
		options:           opts,
		appExists:         appExists,
		collectedHeaders:  make(map[string]string),
		authOptions:       []string{"none", "bearer", "api_key", "basic"},
		shimOptions:       []string{"Yes", "No"},
		confirmOptions:    []string{"No", "Yes"},
		addHeadersOptions: []string{"No", "Yes"},
		history:           []string{},
	}

	m.initializeTextInputs(opts)
	m.initializeSpinner()

	if opts.SpecSource != "" || len(opts.SpecSources) > 0 {
		m.step = StepLoading
	}

	if opts.AuthType != "" {
		for i, opt := range m.authOptions {
			if opt == opts.AuthType {
				m.authIndex = i
				break
			}
		}
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
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}
	}

	switch m.step {
	case StepSpecInput:
		return m.updateSpecInput(msg)
	case StepLoading:
		return m.updateLoading(msg)
	case StepDescription:
		return m.updateDescription(msg)
	case StepBaseURL:
		return m.updateBaseURL(msg)
	case StepAuthType:
		return m.updateAuthType(msg)
	case StepShim:
		return m.updateShim(msg)
	case StepAuthDetails:
		return m.updateAuthDetails(msg)
	case StepAddHeadersConfirm:
		return m.updateAddHeadersConfirm(msg)
	case StepHeaderInput:
		return m.updateHeaderInput(msg)
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
	case StepLoading:
		fmt.Fprintf(s, "%s Loading specification...", m.spinner.View())
	case StepDescription:
		renderTextInputStep(s, "? Description:", m.descInput.View())
	case StepBaseURL:
		renderTextInputStep(s, "? Base URL:", m.baseUrlInput.View())
	case StepAuthType:
		m.renderChoiceStep(s, "? Authentication Type:", m.authOptions, m.authIndex)
	case StepShim:
		m.renderChoiceStep(s, "? Create Shim Executable:", m.shimOptions, m.shimIndex)
	case StepAuthDetails:
		m.renderAuthDetailsStep(s)
	case StepAddHeadersConfirm:
		s.WriteString(questionStyle.Render("? Add Custom HTTP Headers:"))
		s.WriteString("\n")
		s.WriteString(m.renderChoice("", m.addHeadersOptions, m.addHeadersIndex))
		s.WriteString("\n\n")
	case StepHeaderInput:
		m.renderHeaderInputStep(s)
	case StepOverwriteConfirm:
		fmt.Fprintf(s, "App '%s' already exists.\n\n", m.appName)
		s.WriteString(m.renderChoice("Overwrite?", m.confirmOptions, m.confirmIndex))
		s.WriteString("\n\n")
	}
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
	s.WriteString(questionStyle.Render("? Custom HTTP Headers:"))
	s.WriteString("\n")
	s.WriteString(m.renderInput("Header Name (Leave empty to finish)", m.headerNameInput, m.focusIndex == 0))
	s.WriteString("\n")
	s.WriteString(m.renderInput("Header Value", m.headerValueInput, m.focusIndex == 1))
	s.WriteString("\n\n")
	s.WriteString(helpStyle.Render("(Enter to add/next)"))
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
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyEnter:
			val := m.specInput.Value()
			if val == "" {
				m.err = fmt.Errorf("spec source cannot be empty")
				return m, nil
			}
			m.options.SpecSource = val
			m.err = nil
			m.addHistory("Spec Source", val)
			m.step = StepLoading
			return m, tea.Batch(m.spinner.Tick, m.loadSpecCmd())
		}
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
			// Remove last history (SpecSource) if failed? Or just error.
			return m, nil
		}

		m.addHistory("Loaded Spec", msg.info.Title)
		m.specDoc = msg.info
		m.addHistory("Loaded Spec", msg.info.Title)
		m.specDoc = msg.info
		if m.defaultDescription == "" && m.specDoc.Title != "" {
			m.defaultDescription = m.specDoc.Title
			m.descInput.Placeholder = m.specDoc.Title
		}
		if m.defaultBaseURL == "" && msg.baseURL != "" {
			m.defaultBaseURL = msg.baseURL
			m.baseUrlInput.Placeholder = msg.baseURL
		}

		m.step = StepDescription
		m.descInput.Focus()
		m.err = nil
		return m, textinput.Blink
	}
	return m, nil
}

func (m Model) updateDescription(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyEnter {
			val := m.descInput.Value()
			if val == "" && m.defaultDescription != "" {
				val = m.defaultDescription
			}
			m.options.Description = val
			m.addHistory("Description", val)
			m.step = StepBaseURL
			m.baseUrlInput.Focus()
			return m, textinput.Blink
		}
	}
	var cmd tea.Cmd
	m.descInput, cmd = m.descInput.Update(msg)
	return m, cmd
}

func (m Model) updateBaseURL(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.Type == tea.KeyEnter {
			val := m.baseUrlInput.Value()
			if val == "" && m.defaultBaseURL != "" {
				val = m.defaultBaseURL
			}

			// Validate URL
			u, err := url.Parse(val)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				m.err = fmt.Errorf("invalid Base URL: must start with http:// or https:// and have a host")
				return m, nil
			}

			m.options.BaseURL = val
			m.err = nil
			m.addHistory("Base URL", val)
			m.step = StepAuthType
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.baseUrlInput, cmd = m.baseUrlInput.Update(msg)
	return m, cmd
}

func (m Model) updateAuthType(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
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

			m.step = StepShim
			return m, nil
		}
	}
	return m, nil
}

func (m Model) updateShim(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
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
			m.step = StepShim
			return m, nil
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
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
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
		}
	}
	return m, nil
}

func (m Model) updateHeaderInput(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Navigation logic
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if m.focusIndex == 0 {
				// Name input
				if m.headerNameInput.Value() == "" {
					// Empty name -> Done
					return m.checkOverwriteOrFinish()
				}
				// Go to value
				m.focusIndex = 1
				m.headerNameInput.Blur()
				return m, m.headerValueInput.Focus()
			} else {
				// Value input
				// Add pair
				key := strings.TrimSpace(m.headerNameInput.Value())
				val := strings.TrimSpace(m.headerValueInput.Value())
				if key != "" {
					m.collectedHeaders[key] = val
					m.addHistory("Header", fmt.Sprintf("%s: %s", key, val))
				}

				// Reset for next
				m.headerNameInput.SetValue("")
				m.headerValueInput.SetValue("")
				m.focusIndex = 0
				m.headerValueInput.Blur()
				return m, m.headerNameInput.Focus()
			}

		case "tab", "shift+tab":
			if m.focusIndex == 0 {
				m.focusIndex = 1
				m.headerNameInput.Blur()
				return m, m.headerValueInput.Focus()
			} else {
				m.focusIndex = 0
				m.headerValueInput.Blur()
				return m, m.headerNameInput.Focus()
			}
		}
	}

	var cmd tea.Cmd
	if m.focusIndex == 0 {
		m.headerNameInput, cmd = m.headerNameInput.Update(msg)
		return m, cmd
	} else {
		m.headerValueInput, cmd = m.headerValueInput.Update(msg)
		return m, cmd
	}
}

func (m Model) checkOverwriteOrFinish() (Model, tea.Cmd) {
	if m.appExists && !m.options.Force {
		m.step = StepOverwriteConfirm
		m.confirmIndex = 0 // Default No
		return m, nil
	}
	m.result = &m.options
	m.options.Headers = m.collectedHeaders // Save headers
	return m, tea.Quit
}

func (m Model) updateOverwriteConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "left", "h":
			m.confirmIndex = 0
		case "right", "l":
			m.confirmIndex = 1
		case "enter":
			if m.confirmIndex == 1 { // Yes
				m.options.Force = true
				m.result = &m.options
				m.addHistory("Overwrite App", "Yes")
				// Done
				return m, tea.Quit
			} else {
				m.err = fmt.Errorf("installation aborted by user")
				return m, tea.Quit
			}
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
