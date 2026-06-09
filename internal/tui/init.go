/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package tui

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/config"
	"github.com/cisco-open/semantic-architecture-retrieval-analysis-system/internal/copilot"
)

// InitResult holds the user's choices from the init wizard.
type InitResult struct {
	Provider    string
	Model       string
	Endpoint    string
	APIKey      string
	LLMProvider string
	LLMModel    string
	LLMEndpoint string
	LLMAPIKey   string
	Done        bool
	Aborted     bool
}

type initStep int

const (
	stepProvider initStep = iota
	stepEndpoint
	stepAPIKey
	stepModel
	stepConfirm
)

type subFocus int

const (
	focusEmbed subFocus = iota
	focusLLM
)

// --- Ollama model fetch messages ---

type ollamaEmbedModelsMsg struct {
	models []string
	err    error
}

type ollamaLLMModelsMsg struct {
	models []string
	err    error
}

// fetchOllamaModelList fetches model names from an Ollama /api/tags endpoint.
func fetchOllamaModelList(endpoint string) ([]string, error) {
	endpoint = strings.TrimRight(endpoint, "/")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(endpoint + "/api/tags")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	var names []string
	for _, m := range result.Models {
		names = append(names, m.Name)
	}
	sort.Strings(names)
	return names, nil
}

func fetchOllamaEmbedModels(endpoint string) tea.Cmd {
	return func() tea.Msg {
		models, err := fetchOllamaModelList(endpoint)
		return ollamaEmbedModelsMsg{models: models, err: err}
	}
}

func fetchOllamaLLMModels(endpoint string) tea.Cmd {
	return func() tea.Msg {
		models, err := fetchOllamaModelList(endpoint)
		return ollamaLLMModelsMsg{models: models, err: err}
	}
}

// InitModel is the Bubble Tea model for the interactive init wizard.
type InitModel struct {
	step  initStep
	focus subFocus

	// Provider selection
	providers     []string
	embedProvider int
	llmProvider   int

	// Text inputs
	embedEndpInput  textinput.Model
	llmEndpInput    textinput.Model
	embedKeyInput   textinput.Model
	llmKeyInput     textinput.Model
	embedModelInput textinput.Model
	llmModelInput   textinput.Model

	// Ollama embed model selection
	embedModelList   []string // filtered: "embed" in name
	embedModelCursor int
	embedFetching    bool
	embedFetchErr    error

	// Ollama LLM model selection
	llmModelList   []string // filtered: non-embed
	llmModelCursor int
	llmFetching    bool
	llmFetchErr    error

	result   InitResult
	width    int
	height   int
	quitting bool
}

// NewInitModel creates a new init wizard model.
func NewInitModel() InitModel {
	eei := textinput.New()
	eei.Placeholder = "http://localhost:11434"
	eei.CharLimit = 200
	eei.Width = 50

	lei := textinput.New()
	lei.Placeholder = "http://localhost:11434"
	lei.CharLimit = 200
	lei.Width = 50

	eki := textinput.New()
	eki.Placeholder = "(optional, press Enter to skip)"
	eki.CharLimit = 200
	eki.Width = 50
	eki.EchoMode = textinput.EchoPassword
	eki.EchoCharacter = '•'

	lki := textinput.New()
	lki.Placeholder = "(optional, press Enter to skip)"
	lki.CharLimit = 200
	lki.Width = 50
	lki.EchoMode = textinput.EchoPassword
	lki.EchoCharacter = '•'

	emi := textinput.New()
	emi.Placeholder = "nomic-embed-text"
	emi.CharLimit = 100
	emi.Width = 50

	lmi := textinput.New()
	lmi.Placeholder = "llama3.2"
	lmi.CharLimit = 100
	lmi.Width = 50

	return InitModel{
		step:            stepProvider,
		focus:           focusEmbed,
		providers:       []string{"ollama", "lmstudio", "openai", "copilot"},
		embedProvider:   0,
		llmProvider:     0,
		embedEndpInput:  eei,
		llmEndpInput:    lei,
		embedKeyInput:   eki,
		llmKeyInput:     lki,
		embedModelInput: emi,
		llmModelInput:   lmi,
	}
}

// GetResult returns the init wizard result.
func (m InitModel) GetResult() InitResult {
	return m.result
}

func (m InitModel) Init() tea.Cmd {
	return nil
}

func (m InitModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case ollamaEmbedModelsMsg:
		m.embedFetching = false
		if msg.err != nil {
			m.embedFetchErr = msg.err
		} else {
			m.embedModelList = nil
			for _, name := range msg.models {
				if strings.Contains(strings.ToLower(name), "embed") {
					m.embedModelList = append(m.embedModelList, name)
				}
			}
		}
		m.embedModelCursor = 0
		// If at embed model step and no list available, focus text input
		if m.step == stepModel && m.focus == focusEmbed && len(m.embedModelList) == 0 {
			m.embedModelInput.Focus()
			return m, textinput.Blink
		}
		return m, nil

	case ollamaLLMModelsMsg:
		m.llmFetching = false
		if msg.err != nil {
			m.llmFetchErr = msg.err
		} else {
			m.llmModelList = nil
			for _, name := range msg.models {
				if !strings.Contains(strings.ToLower(name), "embed") {
					m.llmModelList = append(m.llmModelList, name)
				}
			}
		}
		m.llmModelCursor = 0
		// If at LLM model step and no list available, focus text input
		if m.step == stepModel && m.focus == focusLLM && len(m.llmModelList) == 0 {
			m.llmModelInput.Focus()
			return m, textinput.Blink
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			m.result.Aborted = true
			m.quitting = true
			return m, tea.Quit

		case "q":
			if m.step == stepProvider || m.step == stepConfirm {
				m.result.Aborted = true
				m.quitting = true
				return m, tea.Quit
			}
		}

		switch m.step {
		case stepProvider:
			return m.updateProvider(msg)
		case stepEndpoint:
			return m.updateEndpoint(msg)
		case stepAPIKey:
			return m.updateAPIKey(msg)
		case stepModel:
			return m.updateModel(msg)
		case stepConfirm:
			return m.updateConfirm(msg)
		}
	}

	return m, nil
}

// ---------------------------------------------------------------------------
// Step update handlers
// ---------------------------------------------------------------------------

func (m InitModel) updateProvider(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.focus == focusEmbed {
		switch msg.String() {
		case "up", "k":
			if m.embedProvider > 0 {
				m.embedProvider--
			}
		case "down", "j":
			if m.embedProvider < len(m.providers)-1 {
				m.embedProvider++
			}
		case "enter":
			m.result.Provider = m.providers[m.embedProvider]
			// Pre-select LLM provider to match embedding
			m.llmProvider = m.embedProvider
			m.focus = focusLLM
		}
		return m, nil
	}

	// focusLLM
	switch msg.String() {
	case "up", "k":
		if m.llmProvider > 0 {
			m.llmProvider--
		}
	case "down", "j":
		if m.llmProvider < len(m.providers)-1 {
			m.llmProvider++
		}
	case "enter":
		m.result.LLMProvider = m.providers[m.llmProvider]
		// Load provider-specific defaults
		embedDefaults := config.DefaultEmbedderForProvider(m.result.Provider)
		llmDefaults := config.DefaultLLMForProvider(m.result.LLMProvider)
		m.embedEndpInput.SetValue(embedDefaults.Endpoint)
		m.embedEndpInput.Placeholder = embedDefaults.Endpoint
		m.llmEndpInput.Placeholder = llmDefaults.Endpoint
		m.embedModelInput.SetValue(embedDefaults.Model)
		m.embedModelInput.Placeholder = embedDefaults.Model
		m.llmModelInput.SetValue(llmDefaults.Model)
		m.llmModelInput.Placeholder = llmDefaults.Model
		// Transition to endpoint step
		m.step = stepEndpoint
		m.focus = focusEmbed
		m.embedEndpInput.Focus()
		return m, textinput.Blink
	}
	return m, nil
}

func (m InitModel) updateEndpoint(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.focus == focusEmbed {
		switch msg.String() {
		case "enter":
			val := m.embedEndpInput.Value()
			if val == "" {
				val = m.embedEndpInput.Placeholder
			}
			m.result.Endpoint = val
			m.embedEndpInput.Blur()
			// Pre-fill LLM endpoint. If both providers match, reuse the
			// embedding endpoint so users only type once. Otherwise fall
			// back to the LLM provider's own default (e.g. ollama embed +
			// copilot LLM should not point copilot at localhost).
			if m.result.Provider == m.result.LLMProvider {
				m.llmEndpInput.SetValue(val)
			} else {
				m.llmEndpInput.SetValue(config.DefaultLLMForProvider(m.result.LLMProvider).Endpoint)
			}
			m.focus = focusLLM
			m.llmEndpInput.Focus()
			return m, textinput.Blink
		}
		var cmd tea.Cmd
		m.embedEndpInput, cmd = m.embedEndpInput.Update(msg)
		return m, cmd
	}

	// focusLLM
	switch msg.String() {
	case "enter":
		val := m.llmEndpInput.Value()
		if val == "" {
			val = m.llmEndpInput.Placeholder
		}
		m.result.LLMEndpoint = val
		m.llmEndpInput.Blur()
		m.step = stepAPIKey
		m.focus = focusEmbed
		m.embedKeyInput.Focus()
		return m, textinput.Blink
	}
	var cmd tea.Cmd
	m.llmEndpInput, cmd = m.llmEndpInput.Update(msg)
	return m, cmd
}

func (m InitModel) updateAPIKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.focus == focusEmbed {
		switch msg.String() {
		case "enter":
			// Copilot doesn't use a static API key — its bearer token is
			// minted on the fly from the user's OAuth credential by the
			// copilot package, so we record an empty key regardless of
			// what the user typed.
			if m.result.Provider == "copilot" {
				m.result.APIKey = ""
			} else {
				m.result.APIKey = m.embedKeyInput.Value()
			}
			m.embedKeyInput.Blur()
			// Pre-fill LLM API key only when both providers match and we
			// actually have a key. Otherwise leave the LLM key blank so
			// users don't accidentally send an OpenAI key to Copilot or
			// vice versa.
			if m.result.APIKey != "" && m.result.Provider == m.result.LLMProvider {
				m.llmKeyInput.SetValue(m.result.APIKey)
			}
			m.focus = focusLLM
			m.llmKeyInput.Focus()
			return m, textinput.Blink
		}
		var cmd tea.Cmd
		m.embedKeyInput, cmd = m.embedKeyInput.Update(msg)
		return m, cmd
	}

	// focusLLM
	switch msg.String() {
	case "enter":
		if m.result.LLMProvider == "copilot" {
			m.result.LLMAPIKey = ""
		} else {
			m.result.LLMAPIKey = m.llmKeyInput.Value()
		}
		m.llmKeyInput.Blur()
		// Transition to model step — trigger Ollama fetches if needed
		m.step = stepModel
		m.focus = focusEmbed
		var cmds []tea.Cmd
		if m.result.Provider == "ollama" {
			m.embedFetching = true
			m.embedFetchErr = nil
			cmds = append(cmds, fetchOllamaEmbedModels(m.result.Endpoint))
		} else {
			m.embedModelInput.Focus()
			cmds = append(cmds, textinput.Blink)
		}
		if m.result.LLMProvider == "ollama" {
			m.llmFetching = true
			m.llmFetchErr = nil
			cmds = append(cmds, fetchOllamaLLMModels(m.result.LLMEndpoint))
		}
		if len(cmds) > 0 {
			return m, tea.Batch(cmds...)
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.llmKeyInput, cmd = m.llmKeyInput.Update(msg)
	return m, cmd
}

func (m InitModel) updateModel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.focus == focusEmbed {
		return m.updateEmbedModel(msg)
	}
	return m.updateLLMModel(msg)
}

func (m InitModel) updateEmbedModel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Still fetching from Ollama — ignore input
	if m.result.Provider == "ollama" && m.embedFetching {
		return m, nil
	}
	// Ollama list selection
	if m.result.Provider == "ollama" && len(m.embedModelList) > 0 {
		return m.updateEmbedModelList(msg)
	}
	// Text input fallback
	switch msg.String() {
	case "enter":
		val := m.embedModelInput.Value()
		if val == "" {
			val = m.embedModelInput.Placeholder
		}
		m.result.Model = val
		m.embedModelInput.Blur()
		return m.transitionToLLMModel()
	}
	var cmd tea.Cmd
	m.embedModelInput, cmd = m.embedModelInput.Update(msg)
	return m, cmd
}

func (m InitModel) updateEmbedModelList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.embedModelList) + 1 // +1 for "Custom"
	switch msg.String() {
	case "up", "k":
		if m.embedModelCursor > 0 {
			m.embedModelCursor--
		}
	case "down", "j":
		if m.embedModelCursor < total-1 {
			m.embedModelCursor++
		}
	case "enter":
		if m.embedModelCursor < len(m.embedModelList) {
			m.result.Model = m.embedModelList[m.embedModelCursor]
		} else {
			// "Custom" selected — switch to text input
			m.embedModelList = nil
			m.embedModelInput.Focus()
			return m, textinput.Blink
		}
		return m.transitionToLLMModel()
	}
	return m, nil
}

func (m InitModel) transitionToLLMModel() (tea.Model, tea.Cmd) {
	m.focus = focusLLM
	// If Ollama LLM models are available or still fetching, use list
	if m.result.LLMProvider == "ollama" && (m.llmFetching || len(m.llmModelList) > 0) {
		return m, nil
	}
	m.llmModelInput.Focus()
	return m, textinput.Blink
}

func (m InitModel) updateLLMModel(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Still fetching from Ollama — ignore input
	if m.result.LLMProvider == "ollama" && m.llmFetching {
		return m, nil
	}
	// Ollama list selection
	if m.result.LLMProvider == "ollama" && len(m.llmModelList) > 0 {
		return m.updateLLMModelList(msg)
	}
	// Text input fallback
	switch msg.String() {
	case "enter":
		val := m.llmModelInput.Value()
		if val == "" {
			val = m.llmModelInput.Placeholder
		}
		m.result.LLMModel = val
		m.llmModelInput.Blur()
		m.step = stepConfirm
		return m, nil
	}
	var cmd tea.Cmd
	m.llmModelInput, cmd = m.llmModelInput.Update(msg)
	return m, cmd
}

func (m InitModel) updateLLMModelList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	total := len(m.llmModelList) + 1
	switch msg.String() {
	case "up", "k":
		if m.llmModelCursor > 0 {
			m.llmModelCursor--
		}
	case "down", "j":
		if m.llmModelCursor < total-1 {
			m.llmModelCursor++
		}
	case "enter":
		if m.llmModelCursor < len(m.llmModelList) {
			m.result.LLMModel = m.llmModelList[m.llmModelCursor]
		} else {
			// "Custom" selected — switch to text input
			m.llmModelList = nil
			m.llmModelInput.Focus()
			return m, textinput.Blink
		}
		m.step = stepConfirm
		return m, nil
	}
	return m, nil
}

func (m InitModel) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y", "enter":
		m.result.Done = true
		m.quitting = true
		return m, tea.Quit
	case "n", "N":
		m.result.Aborted = true
		m.quitting = true
		return m, tea.Quit
	case "b":
		m.step = stepProvider
		m.focus = focusEmbed
		return m, nil
	}
	return m, nil
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m InitModel) View() string {
	if m.quitting {
		if m.result.Done {
			return SuccessStyle.Render(SymbolCheck+" Project initialized successfully!") + "\n"
		}
		return WarningStyle.Render("Init cancelled.") + "\n"
	}

	var b strings.Builder

	// Header
	b.WriteString(LogoStyle.Render(LogoText) + "\n")
	b.WriteString(TaglineStyle.Render("  AI-native codebase intelligence") + "\n\n")

	// Progress indicator
	steps := []string{"Provider", "Endpoint", "API Key", "Model", "Confirm"}
	b.WriteString(m.renderProgress(steps) + "\n\n")

	switch m.step {
	case stepProvider:
		b.WriteString(m.renderProviderStep())
	case stepEndpoint:
		b.WriteString(m.renderEndpointStep())
	case stepAPIKey:
		b.WriteString(m.renderAPIKeyStep())
	case stepModel:
		b.WriteString(m.renderModelStep())
	case stepConfirm:
		b.WriteString(m.renderConfirmStep())
	}

	// Footer help
	b.WriteString("\n" + m.renderHelp())

	return b.String()
}

func (m InitModel) renderProgress(steps []string) string {
	var parts []string
	for i, name := range steps {
		if i == int(m.step) {
			parts = append(parts, FocusedStyle.Render(fmt.Sprintf("[%s]", name)))
		} else if i < int(m.step) {
			parts = append(parts, SuccessStyle.Render(fmt.Sprintf(" %s %s ", SymbolCheck, name)))
		} else {
			parts = append(parts, DimStyle.Render(fmt.Sprintf(" %s ", name)))
		}
	}
	return strings.Join(parts, DimStyle.Render(" "+SymbolArrow+" "))
}

// ---------------------------------------------------------------------------
// Render helpers
// ---------------------------------------------------------------------------

func (m InitModel) renderProviderStep() string {
	var b strings.Builder

	descriptions := map[string]string{
		"ollama":   "Local inference, free, runs on your machine",
		"lmstudio": "Local inference via LM Studio desktop app",
		"openai":   "OpenAI API or any OpenAI-compatible endpoint",
		"copilot":  "GitHub Copilot (sign in once with 'saras copilot login')",
	}

	if m.focus == focusEmbed {
		b.WriteString(TitleStyle.Render("Embedding Provider") + "\n\n")
		for i, p := range m.providers {
			cursor := "  "
			style := ItemStyle
			if i == m.embedProvider {
				cursor = FocusedStyle.Render(SymbolArrow + " ")
				style = ActiveItemStyle
			}
			name := style.Render(p)
			desc := MutedStyle.Render(" " + SymbolDot + " " + descriptions[p])
			b.WriteString(fmt.Sprintf("%s%s%s\n", cursor, name, desc))
		}
		b.WriteString("\n" + DimStyle.Render("LLM Provider") + "\n")
		b.WriteString(DimStyle.Render("  (select embedding provider first)") + "\n")
	} else {
		b.WriteString(SuccessStyle.Render(fmt.Sprintf("%s Embedding Provider: %s", SymbolCheck, m.result.Provider)) + "\n\n")
		b.WriteString(TitleStyle.Render("LLM Provider") + "\n\n")
		for i, p := range m.providers {
			cursor := "  "
			style := ItemStyle
			if i == m.llmProvider {
				cursor = FocusedStyle.Render(SymbolArrow + " ")
				style = ActiveItemStyle
			}
			name := style.Render(p)
			desc := MutedStyle.Render(" " + SymbolDot + " " + descriptions[p])
			b.WriteString(fmt.Sprintf("%s%s%s\n", cursor, name, desc))
		}
	}

	return b.String()
}

func (m InitModel) renderEndpointStep() string {
	var b strings.Builder

	if m.focus == focusEmbed {
		b.WriteString(TitleStyle.Render("Embedding API Endpoint") + "\n")
		b.WriteString(MutedStyle.Render(fmt.Sprintf("Provider: %s", m.result.Provider)) + "\n\n")
		b.WriteString(m.embedEndpInput.View() + "\n")
		b.WriteString("\n" + DimStyle.Render("LLM Endpoint") + "\n")
		b.WriteString(DimStyle.Render("  (will be pre-filled with embedding endpoint)") + "\n")
	} else {
		b.WriteString(SuccessStyle.Render(fmt.Sprintf("%s Embedding Endpoint: %s", SymbolCheck, m.result.Endpoint)) + "\n\n")
		b.WriteString(TitleStyle.Render("LLM API Endpoint") + "\n")
		b.WriteString(MutedStyle.Render(fmt.Sprintf("Provider: %s | Pre-filled with embedding endpoint — change if needed", m.result.LLMProvider)) + "\n\n")
		b.WriteString(m.llmEndpInput.View() + "\n")
	}

	return b.String()
}

func (m InitModel) renderAPIKeyStep() string {
	var b strings.Builder

	if m.focus == focusEmbed {
		b.WriteString(TitleStyle.Render("Embedding API Key") + "\n")
		if m.result.Provider == "copilot" {
			b.WriteString(MutedStyle.Render("GitHub Copilot uses OAuth — leave blank and press Enter. Run 'saras copilot login' if you haven't already.") + "\n\n")
		} else {
			b.WriteString(MutedStyle.Render("Optional. Required for OpenAI, skip for local providers.") + "\n\n")
		}
		b.WriteString(m.embedKeyInput.View() + "\n")
		b.WriteString("\n" + DimStyle.Render("LLM API Key") + "\n")
		b.WriteString(DimStyle.Render("  (will be pre-filled with embedding key)") + "\n")
	} else {
		if m.result.APIKey != "" {
			b.WriteString(SuccessStyle.Render(fmt.Sprintf("%s Embedding API Key: ••••••••", SymbolCheck)) + "\n\n")
		} else {
			b.WriteString(SuccessStyle.Render(fmt.Sprintf("%s Embedding API Key: (none)", SymbolCheck)) + "\n\n")
		}
		b.WriteString(TitleStyle.Render("LLM API Key") + "\n")
		if m.result.LLMProvider == "copilot" {
			b.WriteString(MutedStyle.Render("GitHub Copilot uses OAuth — leave blank and press Enter. Run 'saras copilot login' if you haven't already.") + "\n\n")
		} else {
			b.WriteString(MutedStyle.Render("Optional. Pre-filled with embedding key if set.") + "\n\n")
		}
		b.WriteString(m.llmKeyInput.View() + "\n")
	}

	return b.String()
}

func (m InitModel) renderModelStep() string {
	var b strings.Builder

	if m.focus == focusEmbed {
		b.WriteString(TitleStyle.Render("Embedding Model") + "\n")
		b.WriteString(MutedStyle.Render(fmt.Sprintf("Provider: %s | Endpoint: %s", m.result.Provider, m.result.Endpoint)) + "\n\n")
		b.WriteString(m.renderEmbedModelSelector())
		b.WriteString("\n" + DimStyle.Render("LLM Model") + "\n")
		b.WriteString(DimStyle.Render("  (select embedding model first)") + "\n")
	} else {
		b.WriteString(SuccessStyle.Render(fmt.Sprintf("%s Embedding Model: %s", SymbolCheck, m.result.Model)) + "\n\n")
		b.WriteString(TitleStyle.Render("LLM Model") + "\n")
		b.WriteString(MutedStyle.Render(fmt.Sprintf("Provider: %s | Endpoint: %s", m.result.LLMProvider, m.result.LLMEndpoint)) + "\n\n")
		b.WriteString(m.renderLLMModelSelector())
	}

	return b.String()
}

func (m InitModel) renderEmbedModelSelector() string {
	var b strings.Builder

	if m.result.Provider == "ollama" && m.embedFetching {
		b.WriteString(MutedStyle.Render("  Fetching models from Ollama...") + "\n")
		return b.String()
	}

	if m.result.Provider == "ollama" && len(m.embedModelList) > 0 {
		b.WriteString(MutedStyle.Render("  Select an embedding model:") + "\n\n")
		for i, name := range m.embedModelList {
			cursor := "  "
			style := ItemStyle
			if i == m.embedModelCursor {
				cursor = FocusedStyle.Render(SymbolArrow + " ")
				style = ActiveItemStyle
			}
			b.WriteString(fmt.Sprintf("%s%s\n", cursor, style.Render(name)))
		}
		cursor := "  "
		style := ItemStyle
		if m.embedModelCursor == len(m.embedModelList) {
			cursor = FocusedStyle.Render(SymbolArrow + " ")
			style = ActiveItemStyle
		}
		b.WriteString(fmt.Sprintf("%s%s\n", cursor, style.Render("Type custom name...")))
		return b.String()
	}

	if m.result.Provider == "ollama" && m.embedFetchErr != nil {
		b.WriteString(WarningStyle.Render(fmt.Sprintf("  Could not fetch models: %v", m.embedFetchErr)) + "\n")
		b.WriteString(MutedStyle.Render("  Type model name manually:") + "\n\n")
	}

	b.WriteString(m.embedModelInput.View() + "\n")
	return b.String()
}

func (m InitModel) renderLLMModelSelector() string {
	var b strings.Builder

	if m.result.LLMProvider == "ollama" && m.llmFetching {
		b.WriteString(MutedStyle.Render("  Fetching models from Ollama...") + "\n")
		return b.String()
	}

	if m.result.LLMProvider == "ollama" && len(m.llmModelList) > 0 {
		b.WriteString(MutedStyle.Render("  Select a chat/LLM model:") + "\n\n")
		for i, name := range m.llmModelList {
			cursor := "  "
			style := ItemStyle
			if i == m.llmModelCursor {
				cursor = FocusedStyle.Render(SymbolArrow + " ")
				style = ActiveItemStyle
			}
			b.WriteString(fmt.Sprintf("%s%s\n", cursor, style.Render(name)))
		}
		cursor := "  "
		style := ItemStyle
		if m.llmModelCursor == len(m.llmModelList) {
			cursor = FocusedStyle.Render(SymbolArrow + " ")
			style = ActiveItemStyle
		}
		b.WriteString(fmt.Sprintf("%s%s\n", cursor, style.Render("Type custom name...")))
		return b.String()
	}

	if m.result.LLMProvider == "ollama" && m.llmFetchErr != nil {
		b.WriteString(WarningStyle.Render(fmt.Sprintf("  Could not fetch models: %v", m.llmFetchErr)) + "\n")
		b.WriteString(MutedStyle.Render("  Type model name manually:") + "\n\n")
	}

	b.WriteString(m.llmModelInput.View() + "\n")
	return b.String()
}

func (m InitModel) renderConfirmStep() string {
	var b strings.Builder
	b.WriteString(TitleStyle.Render("Review configuration") + "\n\n")

	labelStyle := lipgloss.NewStyle().Foreground(ColorSecondary).Width(18)

	b.WriteString(MutedStyle.Render("  Embedding") + "\n")
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Provider:"), m.result.Provider))
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Model:"), m.result.Model))
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Endpoint:"), m.result.Endpoint))
	if m.result.APIKey != "" {
		b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("API Key:"), "••••••••"))
	}

	b.WriteString("\n")
	b.WriteString(MutedStyle.Render("  LLM") + "\n")
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Provider:"), m.result.LLMProvider))
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Model:"), m.result.LLMModel))
	b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("Endpoint:"), m.result.LLMEndpoint))
	if m.result.LLMAPIKey != "" {
		b.WriteString(fmt.Sprintf("  %s %s\n", labelStyle.Render("API Key:"), "••••••••"))
	}

	if m.result.Provider == "copilot" || m.result.LLMProvider == "copilot" {
		b.WriteString("\n" + renderCopilotAuthStatus())
	}

	b.WriteString("\n")
	b.WriteString(MutedStyle.Render("  This will create .saras/config.yaml in the current directory.") + "\n\n")
	b.WriteString(FocusedStyle.Render("  Proceed? ") + MutedStyle.Render("[y]es / [n]o / [b]ack") + "\n")

	return b.String()
}

// renderCopilotAuthStatus returns a short banner showing whether the user is
// already authenticated with GitHub Copilot. The check is local-file only
// (no network), so it's safe to call from the TUI render path.
func renderCopilotAuthStatus() string {
	pt, err := copilot.LoadPersistedToken()
	if errors.Is(err, copilot.ErrNotLoggedIn) {
		return WarningStyle.Render(fmt.Sprintf("  %s GitHub Copilot login required — run 'saras copilot login' before using saras.", SymbolWarning)) + "\n"
	}
	if err != nil {
		return WarningStyle.Render(fmt.Sprintf("  %s Could not check Copilot login: %v", SymbolWarning, err)) + "\n"
	}
	who := "your GitHub account"
	if pt.User != "" {
		who = "@" + pt.User
	}
	return SuccessStyle.Render(fmt.Sprintf("  %s Signed in to GitHub Copilot as %s.", SymbolCheck, who)) + "\n"
}

func (m InitModel) renderHelp() string {
	switch m.step {
	case stepProvider:
		return HelpKeyStyle.Render("↑↓") + HelpDescStyle.Render(" select  ") +
			HelpKeyStyle.Render("enter") + HelpDescStyle.Render(" confirm  ") +
			HelpKeyStyle.Render("esc") + HelpDescStyle.Render(" quit")
	case stepModel:
		showList := (m.focus == focusEmbed && m.result.Provider == "ollama" && len(m.embedModelList) > 0) ||
			(m.focus == focusLLM && m.result.LLMProvider == "ollama" && len(m.llmModelList) > 0)
		if showList {
			return HelpKeyStyle.Render("↑↓") + HelpDescStyle.Render(" select  ") +
				HelpKeyStyle.Render("enter") + HelpDescStyle.Render(" confirm  ") +
				HelpKeyStyle.Render("esc") + HelpDescStyle.Render(" quit")
		}
		return HelpKeyStyle.Render("enter") + HelpDescStyle.Render(" next  ") +
			HelpKeyStyle.Render("esc") + HelpDescStyle.Render(" quit")
	case stepConfirm:
		return HelpKeyStyle.Render("y") + HelpDescStyle.Render(" confirm  ") +
			HelpKeyStyle.Render("n") + HelpDescStyle.Render(" cancel  ") +
			HelpKeyStyle.Render("b") + HelpDescStyle.Render(" back")
	default:
		return HelpKeyStyle.Render("enter") + HelpDescStyle.Render(" next  ") +
			HelpKeyStyle.Render("esc") + HelpDescStyle.Render(" quit")
	}
}
