// Package tools contains the governed, typed tool surface used by AI agents.
// Tools are deliberately read-only in this first slice; mutations remain behind
// the domain API and its human approval gates.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/schemas"
)

var (
	ErrDenied           = errors.New("AI tool call denied")
	ErrAuditUnavailable = errors.New("AI tool audit recorder is required")
)

// Store is the narrow, read-only domain seam exposed to tools.
type Store interface {
	ListEventSchemas(context.Context, domain.Principal) ([]domain.EventSchema, error)
	PreviewSegment(context.Context, domain.Principal, string) (int, map[string]int, error)
	CampaignReport(context.Context, domain.Principal, string) (domain.CampaignReport, error)
	JourneyReport(context.Context, domain.Principal, string) (domain.JourneyReport, error)
	ExperimentReport(context.Context, domain.Principal, string) (domain.ExperimentReport, error)
}

// Tool describes a typed, purpose-bound operation. Input and output are JSON
// Schema documents so free-form model text cannot reach a domain method.
type Tool interface {
	Definition() Definition
	Run(context.Context, Store, domain.Principal, json.RawMessage) (json.RawMessage, error)
}

type Definition struct {
	Name           string
	InputSchema    json.RawMessage
	OutputSchema   json.RawMessage
	RequiredScopes []string
	Purpose        string
}

type ToolCall struct {
	Name           string
	Actor          domain.Principal
	PolicyDecision string
	Error          string
}

// Recorder is implemented by the AI activity audit seam. Denied calls are
// sent here too; callers must not provide a best-effort/no-op recorder for
// governed execution.
type Recorder interface {
	RecordToolCall(context.Context, ToolCall) error
}

type Runner struct {
	store    Store
	recorder Recorder
	tools    map[string]Tool
}

func NewRunner(store Store, recorder Recorder) *Runner {
	return &Runner{store: store, recorder: recorder, tools: map[string]Tool{}}
}

func (r *Runner) Register(tool Tool) error {
	definition := tool.Definition()
	if definition.Name == "" || definition.Purpose == "" {
		return errors.New("tool name and purpose are required")
	}
	if _, exists := r.tools[definition.Name]; exists {
		return fmt.Errorf("tool %q already registered", definition.Name)
	}
	if err := schemas.ValidateDefinition(definition.InputSchema); err != nil {
		return fmt.Errorf("tool %q input schema: %w", definition.Name, err)
	}
	if err := schemas.ValidateDefinition(definition.OutputSchema); err != nil {
		return fmt.Errorf("tool %q output schema: %w", definition.Name, err)
	}
	r.tools[definition.Name] = tool
	return nil
}

func (r *Runner) Call(ctx context.Context, caller domain.Principal, name string, input json.RawMessage) (json.RawMessage, error) {
	tool, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown AI tool %q", name)
	}
	if r.recorder == nil {
		return nil, ErrAuditUnavailable
	}
	d := tool.Definition()
	agent, missing := deriveAgent(caller, d.RequiredScopes)
	if len(missing) != 0 {
		err := fmt.Errorf("%w: tool %q requires scope(s): %v", ErrDenied, name, missing)
		if recordErr := r.record(ctx, ToolCall{Name: name, Actor: agent, PolicyDecision: "denied_scope", Error: err.Error()}); recordErr != nil {
			return nil, recordErr
		}
		return nil, err
	}
	if err := schemas.Validate(d.InputSchema, input); err != nil {
		_ = r.record(ctx, ToolCall{Name: name, Actor: agent, PolicyDecision: "denied_input", Error: err.Error()})
		return nil, fmt.Errorf("tool %q input rejected: %w", name, err)
	}
	output, err := tool.Run(ctx, r.store, agent, input)
	if err != nil {
		if recordErr := r.record(ctx, ToolCall{Name: name, Actor: agent, PolicyDecision: "execution_error", Error: err.Error()}); recordErr != nil {
			return nil, recordErr
		}
		return nil, err
	}
	if err := schemas.Validate(d.OutputSchema, output); err != nil {
		_ = r.record(ctx, ToolCall{Name: name, Actor: agent, PolicyDecision: "schema_reject", Error: err.Error()})
		return nil, fmt.Errorf("tool %q output rejected: %w", name, err)
	}
	if err := r.record(ctx, ToolCall{Name: name, Actor: agent, PolicyDecision: "allowed"}); err != nil {
		return nil, err
	}
	return output, nil
}

func (r *Runner) record(ctx context.Context, call ToolCall) error {
	if r.recorder == nil {
		return nil
	}
	return r.recorder.RecordToolCall(ctx, call)
}

func deriveAgent(caller domain.Principal, required []string) (domain.Principal, []string) {
	agent := caller
	agent.ActorType = "ai_agent"
	agent.Scopes = nil
	var missing []string
	for _, scope := range required {
		if caller.HasScope(scope) {
			agent.Scopes = append(agent.Scopes, scope)
		} else {
			missing = append(missing, scope)
		}
	}
	return agent, missing
}

type schemaInspectTool struct{}

func (schemaInspectTool) Definition() Definition {
	return Definition{
		Name: "schema.inspect", Purpose: "inspect event schemas for governed authoring",
		InputSchema:  json.RawMessage(`{"type":"object","additionalProperties":false}`),
		OutputSchema: json.RawMessage(`{"type":"object","required":["schemas"],"additionalProperties":false,"properties":{"schemas":{"type":"array"}}}`), RequiredScopes: []string{"schemas:read"},
	}
}
func (schemaInspectTool) Run(ctx context.Context, store Store, p domain.Principal, _ json.RawMessage) (json.RawMessage, error) {
	items, err := store.ListEventSchemas(ctx, p)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Schemas []domain.EventSchema `json:"schemas"`
	}{items})
}

type segmentPreviewTool struct{}

func (segmentPreviewTool) Definition() Definition {
	return Definition{
		Name: "segment.preview", Purpose: "preview a segment without changing it",
		InputSchema:    json.RawMessage(`{"type":"object","required":["segment_id"],"additionalProperties":false,"properties":{"segment_id":{"type":"string","minLength":1}}}`),
		OutputSchema:   json.RawMessage(`{"type":"object","required":["count","per_leg"],"additionalProperties":false,"properties":{"count":{"type":"integer"},"per_leg":{"type":"object"}}}`),
		RequiredScopes: []string{"segments:read"},
	}
}
func (segmentPreviewTool) Run(ctx context.Context, store Store, p domain.Principal, input json.RawMessage) (json.RawMessage, error) {
	var in struct {
		SegmentID string `json:"segment_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	count, perLeg, err := store.PreviewSegment(ctx, p, in.SegmentID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(struct {
		Count  int            `json:"count"`
		PerLeg map[string]int `json:"per_leg"`
	}{count, perLeg})
}

type reportReadTool struct{}

func (reportReadTool) Definition() Definition {
	return Definition{
		Name: "report.read", Purpose: "read an aggregate performance report",
		InputSchema:  json.RawMessage(`{"type":"object","required":["report_type","resource_id"],"additionalProperties":false,"properties":{"report_type":{"type":"string","enum":["campaign","journey","experiment"]},"resource_id":{"type":"string","minLength":1}}}`),
		OutputSchema: json.RawMessage(`{"type":"object"}`), RequiredScopes: []string{"reports:read"},
	}
}
func (reportReadTool) Run(ctx context.Context, store Store, p domain.Principal, input json.RawMessage) (json.RawMessage, error) {
	var in struct {
		ReportType string `json:"report_type"`
		ResourceID string `json:"resource_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	var value any
	var err error
	switch in.ReportType {
	case "campaign":
		value, err = store.CampaignReport(ctx, p, in.ResourceID)
	case "journey":
		value, err = store.JourneyReport(ctx, p, in.ResourceID)
	case "experiment":
		value, err = store.ExperimentReport(ctx, p, in.ResourceID)
	default:
		err = fmt.Errorf("unsupported report type %q", in.ReportType)
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

// ReadOnlyTools returns the initial governed tool registry.
func ReadOnlyTools() []Tool {
	return []Tool{schemaInspectTool{}, segmentPreviewTool{}, reportReadTool{}}
}

var _ Store = (ports.Store)(nil)
