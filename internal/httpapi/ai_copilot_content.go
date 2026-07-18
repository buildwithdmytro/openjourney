package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/render"
	"github.com/buildwithdmytro/openjourney/internal/schemas"
)

type contentCopilotInput struct {
	Brief           string `json:"brief"`
	Locale          string `json:"locale,omitempty"`
	BrandVoice      string `json:"brand_voice,omitempty"`
	Channel         string `json:"channel,omitempty"`
	PromptVersionID string `json:"prompt_version_id,omitempty"`
}

type contentCopilotOutput struct {
	Subject       string                    `json:"subject"`
	Body          string                    `json:"body"`
	Title         string                    `json:"title"`
	PushData      map[string]string         `json:"push_data"`
	Localizations map[string]map[string]any `json:"localizations"`
	QA            struct {
		Passed bool     `json:"passed"`
		Issues []string `json:"issues"`
	} `json:"qa"`
}

func (s *Server) createContentCopilot(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	var input contentCopilotInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(input.Brief) == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_content_brief", "brief is required")
		return
	}

	prompt, err := s.store.GetPromptByName(r.Context(), principal, "content-draft")
	if err != nil {
		internalError(w, err, "load content prompt", principal)
		return
	}
	versionID := input.PromptVersionID
	if versionID == "" && prompt.CurrentVersionID != nil {
		versionID = *prompt.CurrentVersionID
	}
	if versionID == "" {
		writeError(w, http.StatusUnprocessableEntity, "prompt_unavailable", "content prompt has no active version")
		return
	}
	version, err := s.store.GetPromptVersion(r.Context(), principal, versionID)
	if err != nil {
		internalError(w, err, "load content prompt version", principal)
		return
	}
	if version.Status != "active" || version.EvalStatus != "passed" {
		writeError(w, http.StatusUnprocessableEntity, "prompt_unavailable", "prompt version is not active and evaluated")
		return
	}
	inputJSON, _ := json.Marshal(map[string]any{"brief": input.Brief, "locale": input.Locale, "brand_voice": input.BrandVoice})
	if err := schemas.Validate(version.InputSchema, inputJSON); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_content_input", err.Error())
		return
	}
	requestData := map[string]any{"brief": input.Brief, "locale": input.Locale, "brand_voice": input.BrandVoice}
	requestPrompt := version.Template
	response, err := s.aiGateway.Generate(r.Context(), principal, ai.GenerateRequest{
		Model: version.Model, Prompt: requestPrompt, OutputSchema: version.OutputSchema,
		RetrievedData: requestData, Purpose: "content_draft", Action: "ai.content_draft", PromptVersionID: version.ID,
	})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "content_generation_failed", err.Error())
		return
	}
	var generated contentCopilotOutput
	if err := json.Unmarshal([]byte(response.Content), &generated); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_content_output", err.Error())
		return
	}
	if !generated.QA.Passed {
		writeError(w, http.StatusUnprocessableEntity, "content_qa_failed", fmt.Sprintf("QA failed: %v", generated.QA.Issues))
		return
	}
	if _, err := render.Render(generated.Body, map[string]any{}); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "content_render_failed", err.Error())
		return
	}
	channel := input.Channel
	if channel == "" {
		channel = "email"
	}
	if channel != "email" {
		writeError(w, http.StatusUnprocessableEntity, "unsupported_content_channel", "content copilot currently creates email drafts")
		return
	}
	subject, body := generated.Subject, generated.Body
	draft, err := s.store.CreateTemplate(r.Context(), principal, domain.Template{
		Name: "AI draft: " + strings.TrimSpace(input.Brief), Channel: channel,
		SubjectTemplate: &subject, HTMLTemplate: &body, TextTemplate: &body,
	})
	if err != nil {
		internalError(w, err, "create content draft", principal)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"draft": draft, "localizations": generated.Localizations, "qa": generated.QA, "activity_id": response.ActivityID})
}
