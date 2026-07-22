package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/ai/agent"
	"github.com/buildwithdmytro/openjourney/internal/ai/tools"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/schemas"
)

type insightsCopilotOutput struct {
	Summary    string   `json:"summary"`
	Insights   []string `json:"insights"`
	KeyMetrics []struct {
		Name   string `json:"name"`
		Value  any    `json:"value"`
		Source string `json:"source"`
	} `json:"key_metrics"`
}

type aiActivityRecorder interface {
	RecordAIActivity(context.Context, domain.Principal, domain.AIActivity) (domain.AIActivity, error)
}

type aiActivityToolRecorder struct {
	store ports.Store
}

func (r *aiActivityToolRecorder) RecordToolCall(ctx context.Context, call tools.ToolCall) error {
	rec, ok := r.store.(aiActivityRecorder)
	if !ok {
		return nil
	}
	var actorID *string
	if call.Actor.UserID != "" {
		actorID = &call.Actor.UserID
	}
	toolCallsJSON, _ := json.Marshal(map[string]any{
		"tool":  call.Name,
		"error": call.Error,
	})
	_, err := rec.RecordAIActivity(ctx, call.Actor, domain.AIActivity{
		TenantID:       call.Actor.TenantID,
		WorkspaceID:    call.Actor.WorkspaceID,
		ActorUserID:    actorID,
		Action:         "ai.tool." + call.Name,
		PolicyDecision: call.PolicyDecision,
		ToolCalls:      toolCallsJSON,
	})
	return err
}

func (s *Server) createInsightsCopilot(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)

	var input struct {
		Question string         `json:"question"`
		Query    map[string]any `json:"query"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	if strings.TrimSpace(input.Question) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "question is required")
		return
	}

	prompt, err := s.store.GetPromptByName(r.Context(), p, "analytics-insight")
	if err != nil {
		internalError(w, err, "load analytics-insight prompt", p)
		return
	}
	if prompt.CurrentVersionID == nil {
		writeError(w, http.StatusUnprocessableEntity, "prompt_unavailable", "analytics-insight prompt has no active version")
		return
	}
	version, err := s.store.GetPromptVersion(r.Context(), p, *prompt.CurrentVersionID)
	if err != nil {
		internalError(w, err, "load analytics-insight prompt version", p)
		return
	}
	if version.Status != "active" || version.EvalStatus != "passed" {
		writeError(w, http.StatusUnprocessableEntity, "prompt_unavailable", "prompt version is not active and evaluated")
		return
	}

	inputJSON, _ := json.Marshal(input)
	if err := schemas.Validate(version.InputSchema, inputJSON); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_insights_input", err.Error())
		return
	}

	toolRecorder := &aiActivityToolRecorder{store: s.store}
	runner := tools.NewRunner(s.store, toolRecorder)
	readTools := tools.ReadOnlyTools()
	for _, t := range readTools {
		_ = runner.Register(t)
	}

	ag := agent.NewAgent(s.aiGateway, runner, readTools, agent.Config{
		MaxSteps: 6,
		Timeout:  30 * time.Second,
	})

	res, err := ag.Run(r.Context(), p, input.Question)
	if err != nil {
		if errors.Is(err, agent.ErrMaxStepsExceeded) {
			writeError(w, http.StatusUnprocessableEntity, "agent_max_steps_exceeded", err.Error())
			return
		}
		writeError(w, http.StatusUnprocessableEntity, "insights_generation_failed", err.Error())
		return
	}

	var output insightsCopilotOutput
	if err := json.Unmarshal([]byte(res.Answer), &output); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_insights_output", fmt.Sprintf("failed to parse agent answer: %v", err))
		return
	}

	if strings.TrimSpace(output.Summary) == "" {
		writeError(w, http.StatusUnprocessableEntity, "invalid_insights_output", "summary is required")
		return
	}

	// Grounding validation: every numeric key metric value must appear in retrieved tool results
	for _, metric := range output.KeyMetrics {
		if strings.TrimSpace(metric.Name) == "" || strings.TrimSpace(metric.Source) == "" {
			writeError(w, http.StatusUnprocessableEntity, "invalid_insights_output", "metric name and source are required")
			return
		}
		if !toolTraceContainsValue(res.Trace, metric.Value) {
			writeError(w, http.StatusUnprocessableEntity, "ungrounded_metric", fmt.Errorf("key metric %q value %v is not grounded in retrieved report values", metric.Name, metric.Value).Error())
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"summary":     output.Summary,
		"insights":    output.Insights,
		"key_metrics": output.KeyMetrics,
		"activity_id": res.FinalActivityID,
		"trace":       res.Trace,
		"status":      res.Status,
	})
}

func toolTraceContainsValue(trace []agent.StepTrace, value any) bool {
	if len(trace) == 0 {
		return false
	}
	for _, st := range trace {
		if len(st.Result) > 0 {
			var raw any
			if err := json.Unmarshal(st.Result, &raw); err == nil {
				if jsonValuePresent(raw, value) {
					return true
				}
			}
		}
	}
	return false
}
