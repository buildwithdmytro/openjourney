package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	journeygraph "github.com/buildwithdmytro/openjourney/internal/journey"
	"github.com/buildwithdmytro/openjourney/internal/schemas"
)

type journeyCopilotInput struct {
	Brief           string `json:"brief"`
	Name            string `json:"name,omitempty"`
	PromptVersionID string `json:"prompt_version_id,omitempty"`
}

func (s *Server) createJourneyCopilot(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	var input journeyCopilotInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(input.Brief) == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_journey_brief", "brief is required")
		return
	}

	prompt, err := s.store.GetPromptByName(r.Context(), principal, "journey-draft")
	if err != nil {
		internalError(w, err, "load journey prompt", principal)
		return
	}
	versionID := input.PromptVersionID
	if versionID == "" && prompt.CurrentVersionID != nil {
		versionID = *prompt.CurrentVersionID
	}
	if versionID == "" {
		writeError(w, http.StatusUnprocessableEntity, "prompt_unavailable", "journey prompt has no active version")
		return
	}
	version, err := s.store.GetPromptVersion(r.Context(), principal, versionID)
	if err != nil {
		internalError(w, err, "load journey prompt version", principal)
		return
	}
	if version.Status != "active" || version.EvalStatus != "passed" {
		writeError(w, http.StatusUnprocessableEntity, "prompt_unavailable", "prompt version is not active and evaluated")
		return
	}

	inputJSON, _ := json.Marshal(map[string]any{"brief": input.Brief})
	if err := schemas.Validate(version.InputSchema, inputJSON); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_journey_input", err.Error())
		return
	}
	response, err := s.aiGateway.Generate(r.Context(), principal, ai.GenerateRequest{
		Model: version.Model, Prompt: version.Template, OutputSchema: version.OutputSchema,
		RetrievedData: map[string]any{"brief": input.Brief}, Purpose: "journey_draft",
		Action: "ai.journey_draft", PromptVersionID: version.ID,
		DomainValidator: func(data []byte) error {
			graph, err := journeygraph.ParseGraph(data)
			if err != nil {
				return err
			}
			return journeygraph.Validate(graph)
		},
	})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "journey_generation_failed", err.Error())
		return
	}

	graphJSON := json.RawMessage(response.Content)
	graph, err := journeygraph.ParseGraph(graphJSON)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_journey_output", err.Error())
		return
	}
	if err := journeygraph.Validate(graph); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_journey_output", err.Error())
		return
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		name = "AI draft: " + strings.TrimSpace(input.Brief)
	}
	draft, err := s.store.CreateJourney(r.Context(), principal, domain.Journey{
		Name: name, Status: "draft", Graph: graphJSON,
	})
	if err != nil {
		internalError(w, err, "create journey draft", principal)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"draft": draft, "graph": json.RawMessage(draft.Graph), "activity_id": response.ActivityID,
	})
}
