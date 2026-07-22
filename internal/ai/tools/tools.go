// Package tools contains the governed, typed tool surface used by AI agents.
// Tools are deliberately read-only in this first slice; mutations remain behind
// the domain API and its human approval gates.
package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/flags"
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
	CampaignReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.CampaignReport, error)
	JourneyReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.JourneyReport, error)
	ExperimentReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.ExperimentReport, error)
	FunnelOverTimeReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.FunnelOverTimeReport, error)
	RetentionReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.RetentionReport, error)
	GrowthReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.GrowthReport, error)
	CostReport(context.Context, domain.Principal, string, domain.ReportQuery) (domain.CostReport, error)
	GetCatalogItem(context.Context, domain.Principal, string, string) (domain.CatalogItem, error)
	GetFeatureFlag(context.Context, domain.Principal, string) (domain.FeatureFlag, error)
	EvaluateAudience(context.Context, domain.Principal, string, json.RawMessage) (bool, error)
	GetJourney(context.Context, domain.Principal, string) (domain.Journey, error)
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
		value, err = store.CampaignReport(ctx, p, in.ResourceID, domain.ReportQuery{})
	case "journey":
		value, err = store.JourneyReport(ctx, p, in.ResourceID, domain.ReportQuery{})
	case "experiment":
		value, err = store.ExperimentReport(ctx, p, in.ResourceID, domain.ReportQuery{})
	default:
		err = fmt.Errorf("unsupported report type %q", in.ReportType)
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

type reportTimeseriesTool struct{}

func (reportTimeseriesTool) Definition() Definition {
	return Definition{
		Name:    "report.timeseries",
		Purpose: "read time-series reports over funnels, retention, growth, or costs",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["report_type", "campaign_id"],
			"additionalProperties": false,
			"properties": {
				"report_type": {"type": "string", "enum": ["funnel_over_time", "retention", "growth", "cost"]},
				"campaign_id": {"type": "string", "minLength": 1},
				"granularity": {"type": "string"},
				"start": {"type": "string"},
				"end": {"type": "string"}
			}
		}`),
		OutputSchema:   json.RawMessage(`{"type":"object"}`),
		RequiredScopes: []string{"reports:read"},
	}
}

func (reportTimeseriesTool) Run(ctx context.Context, store Store, p domain.Principal, input json.RawMessage) (json.RawMessage, error) {
	var in struct {
		ReportType  string `json:"report_type"`
		CampaignID  string `json:"campaign_id"`
		Granularity string `json:"granularity"`
		Start       string `json:"start"`
		End         string `json:"end"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	var query domain.ReportQuery
	query.Granularity = in.Granularity
	if in.Start != "" {
		t, err := time.Parse(time.RFC3339, in.Start)
		if err == nil {
			query.Start = t
		}
	}
	if in.End != "" {
		t, err := time.Parse(time.RFC3339, in.End)
		if err == nil {
			query.End = t
		}
	}
	var value any
	var err error
	switch in.ReportType {
	case "funnel_over_time":
		value, err = store.FunnelOverTimeReport(ctx, p, in.CampaignID, query)
	case "retention":
		value, err = store.RetentionReport(ctx, p, in.CampaignID, query)
	case "growth":
		value, err = store.GrowthReport(ctx, p, in.CampaignID, query)
	case "cost":
		value, err = store.CostReport(ctx, p, in.CampaignID, query)
	default:
		err = fmt.Errorf("unsupported report_type %q", in.ReportType)
	}
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}

type catalogLookupTool struct{}

func (catalogLookupTool) Definition() Definition {
	return Definition{
		Name:    "catalog.lookup",
		Purpose: "lookup catalog item by catalog ID and item key",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["catalog_id", "item_key"],
			"additionalProperties": false,
			"properties": {
				"catalog_id": {"type": "string", "minLength": 1},
				"item_key": {"type": "string", "minLength": 1}
			}
		}`),
		OutputSchema:   json.RawMessage(`{"type":"object"}`),
		RequiredScopes: []string{"catalogs:read"},
	}
}

func (catalogLookupTool) Run(ctx context.Context, store Store, p domain.Principal, input json.RawMessage) (json.RawMessage, error) {
	var in struct {
		CatalogID string `json:"catalog_id"`
		ItemKey   string `json:"item_key"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	item, err := store.GetCatalogItem(ctx, p, in.CatalogID, in.ItemKey)
	if err != nil {
		return nil, err
	}
	return json.Marshal(item)
}

type evalAudienceAdapter struct {
	store Store
	p     domain.Principal
}

func (a evalAudienceAdapter) Eval(ctx context.Context, profileID string, dsl json.RawMessage) (bool, error) {
	return a.store.EvaluateAudience(ctx, a.p, profileID, dsl)
}

type flagEvaluateTool struct{}

func (flagEvaluateTool) Definition() Definition {
	return Definition{
		Name:    "flag.evaluate",
		Purpose: "evaluate feature flag for a subject profile",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["flag_id", "profile_id"],
			"additionalProperties": false,
			"properties": {
				"flag_id": {"type": "string", "minLength": 1},
				"profile_id": {"type": "string", "minLength": 1}
			}
		}`),
		OutputSchema:   json.RawMessage(`{"type":"object"}`),
		RequiredScopes: []string{"flags:read"},
	}
}

func (flagEvaluateTool) Run(ctx context.Context, store Store, p domain.Principal, input json.RawMessage) (json.RawMessage, error) {
	var in struct {
		FlagID    string `json:"flag_id"`
		ProfileID string `json:"profile_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	flag, err := store.GetFeatureFlag(ctx, p, in.FlagID)
	if err != nil {
		return nil, err
	}
	res, err := flags.Evaluate(ctx, &flag, in.ProfileID, evalAudienceAdapter{store: store, p: p})
	if err != nil {
		return nil, err
	}
	return json.Marshal(res)
}

type journeyInspectTool struct{}

func (journeyInspectTool) Definition() Definition {
	return Definition{
		Name:    "journey.inspect",
		Purpose: "inspect journey definition by ID",
		InputSchema: json.RawMessage(`{
			"type": "object",
			"required": ["journey_id"],
			"additionalProperties": false,
			"properties": {
				"journey_id": {"type": "string", "minLength": 1}
			}
		}`),
		OutputSchema:   json.RawMessage(`{"type":"object"}`),
		RequiredScopes: []string{"journeys:read"},
	}
}

func (journeyInspectTool) Run(ctx context.Context, store Store, p domain.Principal, input json.RawMessage) (json.RawMessage, error) {
	var in struct {
		JourneyID string `json:"journey_id"`
	}
	if err := json.Unmarshal(input, &in); err != nil {
		return nil, err
	}
	journey, err := store.GetJourney(ctx, p, in.JourneyID)
	if err != nil {
		return nil, err
	}
	return json.Marshal(journey)
}

// ReadOnlyTools returns the initial governed tool registry.
func ReadOnlyTools() []Tool {
	return []Tool{
		schemaInspectTool{},
		segmentPreviewTool{},
		reportReadTool{},
		reportTimeseriesTool{},
		catalogLookupTool{},
		flagEvaluateTool{},
		journeyInspectTool{},
	}
}

var _ Store = (ports.Store)(nil)
