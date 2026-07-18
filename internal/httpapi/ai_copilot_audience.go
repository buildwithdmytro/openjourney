package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/audience"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/schemas"
)

type audienceCopilotInput struct {
	Brief           string `json:"brief"`
	PromptVersionID string `json:"prompt_version_id,omitempty"`
}

func (s *Server) createAudienceCopilot(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	var input audienceCopilotInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(input.Brief) == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_audience_brief", "brief is required")
		return
	}

	prompt, err := s.store.GetPromptByName(r.Context(), principal, "audience-dsl")
	if err != nil {
		internalError(w, err, "load audience prompt", principal)
		return
	}
	versionID := input.PromptVersionID
	if versionID == "" && prompt.CurrentVersionID != nil {
		versionID = *prompt.CurrentVersionID
	}
	if versionID == "" {
		writeError(w, http.StatusUnprocessableEntity, "prompt_unavailable", "audience prompt has no active version")
		return
	}
	version, err := s.store.GetPromptVersion(r.Context(), principal, versionID)
	if err != nil {
		internalError(w, err, "load audience prompt version", principal)
		return
	}
	if version.Status != "active" || version.EvalStatus != "passed" {
		writeError(w, http.StatusUnprocessableEntity, "prompt_unavailable", "prompt version is not active and evaluated")
		return
	}

	inputJSON, _ := json.Marshal(map[string]any{"brief": input.Brief})
	if err := schemas.Validate(version.InputSchema, inputJSON); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_audience_input", err.Error())
		return
	}
	response, err := s.aiGateway.Generate(r.Context(), principal, ai.GenerateRequest{
		Model: version.Model, Prompt: version.Template, OutputSchema: version.OutputSchema,
		RetrievedData: map[string]any{"brief": input.Brief}, Purpose: "audience_dsl",
		Action: "ai.audience_dsl", PromptVersionID: version.ID,
		DomainValidator: func(data []byte) error {
			_, err := audience.Parse(data)
			return err
		},
	})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "audience_generation_failed", err.Error())
		return
	}

	dsl := json.RawMessage(response.Content)
	if _, err := audience.Parse(dsl); err != nil {
		// The gateway validates this too; keep this check adjacent to the draft boundary.
		writeError(w, http.StatusUnprocessableEntity, "invalid_audience_output", err.Error())
		return
	}
	draft, err := s.store.CreateSegment(r.Context(), principal, domain.Segment{
		Name: "AI draft: " + strings.TrimSpace(input.Brief), Type: "dynamic", Status: "draft", DSL: dsl,
	})
	if err != nil {
		internalError(w, err, "create audience draft", principal)
		return
	}
	count, perLeg, err := s.store.PreviewSegment(r.Context(), principal, draft.ID)
	if err != nil {
		internalError(w, err, "preview audience draft", principal)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"draft": draft, "dsl": json.RawMessage(draft.DSL),
		"preview":     map[string]any{"count": count, "per_leg_counts": perLeg},
		"explanation": "Audience draft validated against the supported DSL and previewed against the current audience.",
		"activity_id": response.ActivityID,
	})
}
