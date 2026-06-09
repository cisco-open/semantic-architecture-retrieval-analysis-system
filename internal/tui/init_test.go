/* SPDX-License-Identifier: Apache-2.0
 * Copyright 2026 Cisco Systems, Inc. and its affiliates
 *
 * See CONTRIBUTORS.md for full contributor list.
 */

package tui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestNewInitModel(t *testing.T) {
	m := NewInitModel()

	if len(m.providers) != 4 {
		t.Errorf("expected 4 providers, got %d", len(m.providers))
	}
	if m.step != stepProvider {
		t.Errorf("expected initial step to be stepProvider")
	}
	if m.focus != focusEmbed {
		t.Errorf("expected initial focus to be focusEmbed")
	}
	if m.embedProvider != 0 {
		t.Errorf("expected initial embedProvider index 0, got %d", m.embedProvider)
	}
}

func TestInitModelProviderNavigation(t *testing.T) {
	m := NewInitModel()
	last := len(m.providers) - 1

	// Down arrow moves selection
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(InitModel)
	if m.embedProvider != 1 {
		t.Errorf("expected embedProvider 1 after down, got %d", m.embedProvider)
	}

	// Walk to the bottom
	for m.embedProvider < last {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
		m = updated.(InitModel)
	}
	if m.embedProvider != last {
		t.Errorf("expected embedProvider %d at bottom, got %d", last, m.embedProvider)
	}

	// Down at bottom should stay
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(InitModel)
	if m.embedProvider != last {
		t.Errorf("expected embedProvider to stay at %d, got %d", last, m.embedProvider)
	}

	// Up arrow
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = updated.(InitModel)
	if m.embedProvider != last-1 {
		t.Errorf("expected embedProvider %d after up, got %d", last-1, m.embedProvider)
	}

	// Up at top should stay
	for i := 0; i < len(m.providers)+2; i++ {
		updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
		m = updated.(InitModel)
	}
	if m.embedProvider != 0 {
		t.Errorf("expected embedProvider to stay at 0, got %d", m.embedProvider)
	}
}

func TestInitModelProviderSelectionOllama(t *testing.T) {
	m := NewInitModel()

	// Select embed provider (ollama at index 0) — moves to LLM focus
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)

	if m.step != stepProvider {
		t.Errorf("expected step to stay at stepProvider for LLM selection, got %d", m.step)
	}
	if m.focus != focusLLM {
		t.Errorf("expected focus to be focusLLM after embed provider selected")
	}
	if m.result.Provider != "ollama" {
		t.Errorf("expected embed provider ollama, got %s", m.result.Provider)
	}

	// Select LLM provider (pre-selected to ollama) — moves to endpoint step
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)

	if m.step != stepEndpoint {
		t.Errorf("expected step to advance to stepEndpoint, got %d", m.step)
	}
	if m.focus != focusEmbed {
		t.Errorf("expected focus to reset to focusEmbed")
	}
	if m.result.LLMProvider != "ollama" {
		t.Errorf("expected LLM provider ollama, got %s", m.result.LLMProvider)
	}
}

func TestInitModelProviderSelectionLMStudio(t *testing.T) {
	m := NewInitModel()

	// Navigate to lmstudio (index 1) and select as embed provider
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(InitModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)

	if m.result.Provider != "lmstudio" {
		t.Errorf("expected embed provider lmstudio, got %s", m.result.Provider)
	}

	// LLM provider should be pre-selected to lmstudio too
	if m.llmProvider != 1 {
		t.Errorf("expected llmProvider pre-selected to 1, got %d", m.llmProvider)
	}
}

func TestInitModelProviderSelectionOpenAI(t *testing.T) {
	m := NewInitModel()

	// Navigate to openai (index 2) and select as embed provider
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(InitModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(InitModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)

	if m.result.Provider != "openai" {
		t.Errorf("expected embed provider openai, got %s", m.result.Provider)
	}
}

func TestInitModelDifferentLLMProvider(t *testing.T) {
	m := NewInitModel()

	// Select ollama as embed provider
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)

	// Navigate to openai for LLM
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(InitModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(InitModel)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)

	if m.result.Provider != "ollama" {
		t.Errorf("expected embed provider ollama, got %s", m.result.Provider)
	}
	if m.result.LLMProvider != "openai" {
		t.Errorf("expected LLM provider openai, got %s", m.result.LLMProvider)
	}
}

func TestInitModelEscapeAborts(t *testing.T) {
	m := NewInitModel()

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = updated.(InitModel)

	if !m.result.Aborted {
		t.Error("expected Aborted to be true after Esc")
	}
	if cmd == nil {
		t.Error("expected quit command")
	}
}

func TestInitModelCtrlCAborts(t *testing.T) {
	m := NewInitModel()

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	m = updated.(InitModel)

	if !m.result.Aborted {
		t.Error("expected Aborted to be true after Ctrl+C")
	}
}

func TestInitModelViewRendersLogo(t *testing.T) {
	m := NewInitModel()
	view := m.View()

	if !strings.Contains(view, "║ S ║") {
		t.Error("expected logo in view")
	}
}

func TestInitModelViewProviderStep(t *testing.T) {
	m := NewInitModel()
	view := m.View()

	if !strings.Contains(view, "ollama") {
		t.Error("expected ollama in provider view")
	}
	if !strings.Contains(view, "lmstudio") {
		t.Error("expected lmstudio in provider view")
	}
	if !strings.Contains(view, "openai") {
		t.Error("expected openai in provider view")
	}
	if !strings.Contains(view, "Embedding Provider") {
		t.Error("expected embedding provider title")
	}
}

func TestInitModelViewConfirmStep(t *testing.T) {
	m := NewInitModel()

	// Walk through to confirm
	m.result.Provider = "ollama"
	m.result.Model = "nomic-embed-text"
	m.result.Endpoint = "http://localhost:11434"
	m.result.LLMProvider = "ollama"
	m.result.LLMModel = "llama3.2"
	m.result.LLMEndpoint = "http://localhost:11434"
	m.step = stepConfirm

	view := m.View()
	if !strings.Contains(view, "Review configuration") {
		t.Error("expected review title")
	}
	if !strings.Contains(view, "nomic-embed-text") {
		t.Error("expected embed model in confirm view")
	}
	if !strings.Contains(view, "llama3.2") {
		t.Error("expected LLM model in confirm view")
	}
}

func TestInitModelConfirmYes(t *testing.T) {
	m := NewInitModel()
	m.result.Provider = "ollama"
	m.result.Model = "nomic-embed-text"
	m.result.Endpoint = "http://localhost:11434"
	m.step = stepConfirm

	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = updated.(InitModel)

	if !m.result.Done {
		t.Error("expected Done to be true after 'y'")
	}
	if m.result.Aborted {
		t.Error("expected Aborted to be false")
	}
	if cmd == nil {
		t.Error("expected quit command")
	}
}

func TestInitModelConfirmNo(t *testing.T) {
	m := NewInitModel()
	m.step = stepConfirm

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	m = updated.(InitModel)

	if m.result.Done {
		t.Error("expected Done to be false after 'n'")
	}
	if !m.result.Aborted {
		t.Error("expected Aborted to be true after 'n'")
	}
}

func TestInitModelConfirmBack(t *testing.T) {
	m := NewInitModel()
	m.step = stepConfirm

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	m = updated.(InitModel)

	if m.step != stepProvider {
		t.Errorf("expected step to go back to provider, got %d", m.step)
	}
	if m.focus != focusEmbed {
		t.Errorf("expected focus to reset to focusEmbed")
	}
}

func TestInitModelQuitView(t *testing.T) {
	m := NewInitModel()
	m.quitting = true
	m.result.Done = true

	view := m.View()
	if !strings.Contains(view, "initialized successfully") {
		t.Errorf("expected success message in quit view, got: %s", view)
	}
}

func TestInitModelAbortedView(t *testing.T) {
	m := NewInitModel()
	m.quitting = true
	m.result.Aborted = true

	view := m.View()
	if !strings.Contains(view, "cancelled") {
		t.Errorf("expected cancelled message in quit view, got: %s", view)
	}
}

func TestInitModelWindowSize(t *testing.T) {
	m := NewInitModel()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = updated.(InitModel)

	if m.width != 120 {
		t.Errorf("expected width 120, got %d", m.width)
	}
	if m.height != 40 {
		t.Errorf("expected height 40, got %d", m.height)
	}
}

func TestInitModelVimNavigation(t *testing.T) {
	m := NewInitModel()

	// 'j' should move down
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = updated.(InitModel)
	if m.embedProvider != 1 {
		t.Errorf("expected embedProvider 1 after 'j', got %d", m.embedProvider)
	}

	// 'k' should move up
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = updated.(InitModel)
	if m.embedProvider != 0 {
		t.Errorf("expected embedProvider 0 after 'k', got %d", m.embedProvider)
	}
}

func TestInitModelGetResult(t *testing.T) {
	m := NewInitModel()
	m.result = InitResult{
		Provider:    "ollama",
		Model:       "test-model",
		Endpoint:    "http://test:1234",
		LLMProvider: "ollama",
		Done:        true,
	}

	r := m.GetResult()
	if r.Provider != "ollama" {
		t.Errorf("expected provider ollama, got %s", r.Provider)
	}
	if r.Model != "test-model" {
		t.Errorf("expected model test-model, got %s", r.Model)
	}
	if !r.Done {
		t.Error("expected Done to be true")
	}
}

func TestInitModelInit(t *testing.T) {
	m := NewInitModel()
	cmd := m.Init()
	if cmd != nil {
		t.Error("expected nil Init command")
	}
}

func TestInitModelEndpointPrefillsLLM(t *testing.T) {
	m := NewInitModel()
	m.result.Provider = "ollama"
	m.result.LLMProvider = "ollama"
	m.step = stepEndpoint
	m.focus = focusEmbed
	m.embedEndpInput.SetValue("http://custom:9999")
	m.embedEndpInput.Focus()

	// Enter on embed endpoint — should pre-fill LLM endpoint
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)

	if m.focus != focusLLM {
		t.Errorf("expected focus to move to focusLLM")
	}
	if m.result.Endpoint != "http://custom:9999" {
		t.Errorf("expected embed endpoint http://custom:9999, got %s", m.result.Endpoint)
	}
	if m.llmEndpInput.Value() != "http://custom:9999" {
		t.Errorf("expected LLM endpoint pre-filled with embed endpoint, got %s", m.llmEndpInput.Value())
	}
}

func TestInitModelAPIKeyPrefillsLLM(t *testing.T) {
	m := NewInitModel()
	m.result.Provider = "openai"
	m.result.LLMProvider = "openai"
	m.result.Endpoint = "https://api.openai.com/v1"
	m.result.LLMEndpoint = "https://api.openai.com/v1"
	m.step = stepAPIKey
	m.focus = focusEmbed
	m.embedKeyInput.SetValue("sk-test-key")
	m.embedKeyInput.Focus()

	// Enter on embed API key — should pre-fill LLM API key
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)

	if m.focus != focusLLM {
		t.Errorf("expected focus to move to focusLLM")
	}
	if m.result.APIKey != "sk-test-key" {
		t.Errorf("expected embed API key sk-test-key, got %s", m.result.APIKey)
	}
	if m.llmKeyInput.Value() != "sk-test-key" {
		t.Errorf("expected LLM API key pre-filled, got %s", m.llmKeyInput.Value())
	}
}

func TestInitModelFullFlowOllama(t *testing.T) {
	m := NewInitModel()

	// Step 1: Select embed provider (ollama)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)
	// Select LLM provider (ollama, pre-selected)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)

	if m.step != stepEndpoint || m.focus != focusEmbed {
		t.Fatalf("expected stepEndpoint/focusEmbed, got step=%d focus=%d", m.step, m.focus)
	}

	// Step 2: Embed endpoint
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)
	// LLM endpoint (pre-filled)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)

	if m.step != stepAPIKey || m.focus != focusEmbed {
		t.Fatalf("expected stepAPIKey/focusEmbed, got step=%d focus=%d", m.step, m.focus)
	}

	// Step 3: Embed API key (skip)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)
	// LLM API key (skip) — should trigger Ollama fetch and move to model step
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)

	if m.step != stepModel || m.focus != focusEmbed {
		t.Fatalf("expected stepModel/focusEmbed, got step=%d focus=%d", m.step, m.focus)
	}
	if !m.embedFetching {
		t.Error("expected embedFetching to be true for ollama")
	}
	if cmd == nil {
		t.Error("expected fetch command for ollama")
	}

	// Simulate embed fetch returning models
	updated, _ = m.Update(ollamaEmbedModelsMsg{
		models: []string{"nomic-embed-text:latest", "llama3.2:latest"},
	})
	m = updated.(InitModel)

	if len(m.embedModelList) != 1 {
		t.Errorf("expected 1 embed model, got %d", len(m.embedModelList))
	}

	// Select embed model
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)

	if m.result.Model != "nomic-embed-text:latest" {
		t.Errorf("expected embed model nomic-embed-text:latest, got %s", m.result.Model)
	}
	if m.focus != focusLLM {
		t.Errorf("expected focus to move to focusLLM for LLM model")
	}

	// Simulate LLM fetch returning models
	updated, _ = m.Update(ollamaLLMModelsMsg{
		models: []string{"nomic-embed-text:latest", "llama3.2:latest", "gemma4"},
	})
	m = updated.(InitModel)

	if len(m.llmModelList) != 2 {
		t.Errorf("expected 2 LLM models, got %d", len(m.llmModelList))
	}

	// Select first LLM model
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)

	if m.result.LLMModel != "llama3.2:latest" {
		t.Errorf("expected LLM model llama3.2:latest, got %s", m.result.LLMModel)
	}
	if m.step != stepConfirm {
		t.Errorf("expected stepConfirm, got step %d", m.step)
	}
}

func TestInitModelOllamaFetchError(t *testing.T) {
	m := NewInitModel()
	m.result.Provider = "ollama"
	m.result.LLMProvider = "ollama"
	m.result.Endpoint = "http://localhost:11434"
	m.result.LLMEndpoint = "http://localhost:11434"
	m.step = stepModel
	m.focus = focusEmbed
	m.embedFetching = true

	// Simulate fetch error — should fall back to text input
	updated, _ := m.Update(ollamaEmbedModelsMsg{err: fmt.Errorf("connection refused")})
	m = updated.(InitModel)

	if m.embedFetching {
		t.Error("expected embedFetching to be false after error")
	}
	if m.embedFetchErr == nil {
		t.Error("expected embedFetchErr to be set")
	}

	// Text input should work
	m.embedModelInput.SetValue("nomic-embed-text")
	m.embedModelInput.Focus()
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)

	if m.result.Model != "nomic-embed-text" {
		t.Errorf("expected model nomic-embed-text, got %s", m.result.Model)
	}
	if m.focus != focusLLM {
		t.Errorf("expected focus to move to focusLLM")
	}
}

func TestInitModelOllamaCustomModelFallback(t *testing.T) {
	m := NewInitModel()
	m.result.Provider = "ollama"
	m.result.LLMProvider = "ollama"
	m.result.Endpoint = "http://localhost:11434"
	m.result.LLMEndpoint = "http://localhost:11434"
	m.step = stepModel
	m.focus = focusEmbed

	// Simulate fetch with one embed model
	updated, _ := m.Update(ollamaEmbedModelsMsg{
		models: []string{"nomic-embed-text:latest", "llama3.2:latest"},
	})
	m = updated.(InitModel)

	// Navigate to "Type custom name..." (index 1 = after the 1 embed model)
	updated, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = updated.(InitModel)
	if m.embedModelCursor != 1 {
		t.Errorf("expected embedModelCursor 1, got %d", m.embedModelCursor)
	}

	// Select "Custom" — should switch to text input
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)

	if len(m.embedModelList) != 0 {
		t.Error("expected embedModelList cleared for custom fallback")
	}
	if m.step != stepModel {
		t.Errorf("expected to stay on stepModel for text input, got step %d", m.step)
	}
	if cmd == nil {
		t.Error("expected blink command for text input focus")
	}
}

func TestInitModelOllamaFetchingView(t *testing.T) {
	m := NewInitModel()
	m.result.Provider = "ollama"
	m.result.Endpoint = "http://localhost:11434"
	m.step = stepModel
	m.focus = focusEmbed
	m.embedFetching = true

	view := m.View()
	if !strings.Contains(view, "Fetching models from Ollama") {
		t.Error("expected fetching message in view")
	}
}

func TestInitModelOllamaModelListView(t *testing.T) {
	m := NewInitModel()
	m.result.Provider = "ollama"
	m.result.Endpoint = "http://localhost:11434"
	m.step = stepModel
	m.focus = focusEmbed
	m.embedModelList = []string{"nomic-embed-text:latest", "mxbai-embed-large:latest"}
	m.embedModelCursor = 0

	view := m.View()
	if !strings.Contains(view, "nomic-embed-text:latest") {
		t.Error("expected embed model in view")
	}
	if !strings.Contains(view, "mxbai-embed-large:latest") {
		t.Error("expected second embed model in view")
	}
	if !strings.Contains(view, "Type custom name") {
		t.Error("expected custom option in view")
	}
}

func TestInitModelLMStudioNoFetch(t *testing.T) {
	m := NewInitModel()
	m.result.Provider = "lmstudio"
	m.result.LLMProvider = "lmstudio"
	m.result.Endpoint = "http://localhost:1234"
	m.result.LLMEndpoint = "http://localhost:1234"
	m.step = stepAPIKey
	m.focus = focusLLM
	m.llmKeyInput.Focus()

	// Enter on LLM API key — should move to model step without triggering fetch
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(InitModel)

	if m.step != stepModel {
		t.Errorf("expected stepModel, got step %d", m.step)
	}
	if m.embedFetching {
		t.Error("expected embedFetching to be false for lmstudio")
	}
	if m.llmFetching {
		t.Error("expected llmFetching to be false for lmstudio")
	}
}

func TestInitModelHelpText(t *testing.T) {
	m := NewInitModel()

	// Provider step help
	view := m.View()
	if !strings.Contains(view, "select") {
		t.Error("expected 'select' in provider help")
	}

	// Confirm step help
	m.step = stepConfirm
	view = m.View()
	if !strings.Contains(view, "confirm") {
		t.Error("expected 'confirm' in confirm help")
	}
}
