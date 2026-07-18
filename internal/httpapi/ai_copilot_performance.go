package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/schemas"
)

type performanceCopilotOutput struct {
	Summary    string `json:"summary"`
	KeyMetrics []struct {
		Name   string `json:"name"`
		Value  any    `json:"value"`
		Source string `json:"source"`
	} `json:"key_metrics"`
	Recommendations []string `json:"recommendations"`
	ProposedVersion struct {
		Name    string         `json:"name"`
		Changes map[string]any `json:"changes"`
	} `json:"proposed_version"`
}

func (s *Server) createPerformanceCopilot(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	campaignID := r.PathValue("campaignId")
	campaign, err := s.store.GetCampaign(r.Context(), p, campaignID)
	if err != nil {
		if errors.Is(err, postgres.ErrNotFound) {
			writeError(w, http.StatusNotFound, "not_found", "campaign not found")
			return
		}
		internalError(w, err, "load campaign for performance copilot", p)
		return
	}
	report, err := s.store.CampaignReport(r.Context(), p, campaignID)
	if err != nil {
		internalError(w, err, "load campaign report", p)
		return
	}

	data := map[string]any{"campaign_id": campaignID, "campaign_report": report}
	if campaign.ExperimentID != nil {
		experimentReport, reportErr := s.store.ExperimentReport(r.Context(), p, *campaign.ExperimentID)
		if reportErr != nil {
			internalError(w, reportErr, "load experiment report", p)
			return
		}
		data["experiment_id"] = *campaign.ExperimentID
		data["experiment_report"] = experimentReport
	}
	prompt, err := s.store.GetPromptByName(r.Context(), p, "performance-summary")
	if err != nil {
		internalError(w, err, "load performance prompt", p)
		return
	}
	if prompt.CurrentVersionID == nil {
		writeError(w, http.StatusUnprocessableEntity, "prompt_unavailable", "performance prompt has no active version")
		return
	}
	version, err := s.store.GetPromptVersion(r.Context(), p, *prompt.CurrentVersionID)
	if err != nil {
		internalError(w, err, "load performance prompt version", p)
		return
	}
	if version.Status != "active" || version.EvalStatus != "passed" {
		writeError(w, http.StatusUnprocessableEntity, "prompt_unavailable", "prompt version is not active and evaluated")
		return
	}
	inputJSON, _ := json.Marshal(data)
	if err := schemas.Validate(version.InputSchema, inputJSON); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_performance_input", err.Error())
		return
	}
	response, err := s.aiGateway.Generate(r.Context(), p, ai.GenerateRequest{
		Model: version.Model, Prompt: version.Template, OutputSchema: version.OutputSchema,
		RetrievedData: data, Purpose: "performance_summary", Action: "ai.performance_summary",
		PromptVersionID: version.ID,
		DomainValidator: func(content []byte) error {
			var output performanceCopilotOutput
			if err := json.Unmarshal(content, &output); err != nil {
				return err
			}
			if strings.TrimSpace(output.Summary) == "" || strings.TrimSpace(output.ProposedVersion.Name) == "" || len(output.KeyMetrics) == 0 {
				return fmt.Errorf("summary, proposed version, and at least one key metric are required")
			}
			for _, metric := range output.KeyMetrics {
				if strings.TrimSpace(metric.Name) == "" || strings.TrimSpace(metric.Source) == "" || !reportContainsValue(report, metric.Value) {
					return fmt.Errorf("key metric %q does not cite a value from the report", metric.Name)
				}
			}
			return nil
		},
	})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "performance_generation_failed", err.Error())
		return
	}
	var output performanceCopilotOutput
	if err := json.Unmarshal([]byte(response.Content), &output); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_performance_output", err.Error())
		return
	}
	description := output.Summary
	draft, err := s.store.CreateCampaign(r.Context(), p, domain.Campaign{
		Name: output.ProposedVersion.Name, Description: &description, SegmentID: campaign.SegmentID,
		TemplateID: campaign.TemplateID, Status: "draft", SegmentVersion: campaign.SegmentVersion,
		TemplateVersion: campaign.TemplateVersion,
	})
	if err != nil {
		internalError(w, err, "create performance draft", p)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"summary": output.Summary, "key_metrics": output.KeyMetrics, "recommendations": output.Recommendations,
		"proposed_version": output.ProposedVersion, "draft": draft, "activity_id": response.ActivityID,
	})
}

func reportContainsValue(report domain.CampaignReport, value any) bool {
	encoded, _ := json.Marshal(report)
	var raw any
	_ = json.Unmarshal(encoded, &raw)
	return jsonValuePresent(raw, value)
}

func jsonValuePresent(report, wanted any) bool {
	switch value := report.(type) {
	case map[string]any:
		for _, child := range value {
			if jsonValuePresent(child, wanted) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if jsonValuePresent(child, wanted) {
				return true
			}
		}
	case float64:
		wantedNumber, ok := wanted.(float64)
		return ok && value == wantedNumber
	case string:
		return value == fmt.Sprint(wanted)
	}
	return false
}
