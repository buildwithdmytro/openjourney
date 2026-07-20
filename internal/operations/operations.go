package operations

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/ai"
	"github.com/buildwithdmytro/openjourney/internal/connector"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/extension"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/scoring"
	"github.com/buildwithdmytro/openjourney/internal/telemetry"
)

type Store interface {
	ClaimOperationJob(context.Context) (domain.OperationJob, bool, error)
	CompleteOperationJob(context.Context, string) error
	FailOperationJob(context.Context, string, error) error
	ExportPrivacyData(context.Context, string) (domain.PrivacyData, error)
	CompletePrivacyExport(context.Context, string, string) error
	DeletePrivacyData(context.Context, string) ([]string, error)
	EnforceRetention(context.Context, string) (domain.RetentionReport, error)
}

type ImportStore interface {
	GetImportJob(context.Context, string) (domain.ImportRequest, string, json.RawMessage, error)
	MarkImportProcessing(context.Context, string) error
	CompleteImport(context.Context, string, string, int, int, int) error
	FailImport(context.Context, string, string) error
	AcceptEvents(context.Context, domain.Principal, []domain.Event) ([]string, error)
	IsSuppressed(context.Context, domain.Principal, string, string) (bool, error)
}

type ConnectorStore interface {
	GetConnectorPipeline(context.Context, domain.Principal, string) (domain.ConnectorPipeline, error)
	GetConnectorPipelineVersion(context.Context, domain.Principal, string) (domain.ConnectorPipelineVersion, error)
	GetExtensionConfig(context.Context, domain.Principal, string) (domain.ExtensionConfig, error)
	RecordConnectorRun(context.Context, domain.ConnectorRun) error
	AcceptEvents(context.Context, domain.Principal, []domain.Event) ([]string, error)
}

type AIGenerationStore interface {
	GetAIGenerationJob(context.Context, string) (domain.AIGenerationJob, error)
	GetPromptVersion(context.Context, domain.Principal, string) (domain.PromptVersion, error)
	MarkAIGenerationProcessing(context.Context, string) error
	CompleteAIGeneration(context.Context, string, string) error
}

type AIGateway interface {
	Generate(context.Context, domain.Principal, ai.GenerateRequest) (*ai.GenerateResponse, error)
}

// ExtensionInvoker is the bounded extension host used by connector jobs. The
// operations package depends only on this seam so the job worker remains
// usable with a nil invoker in deployments that have extensions disabled.
type ExtensionInvoker interface {
	InvokeWithScope(context.Context, domain.Principal, string, string, string, json.RawMessage) (json.RawMessage, string, error)
}

func Drain(ctx context.Context, store Store, blobs ports.BlobStore, maxItems int, watch bool) (int, error) {
	return DrainWithGateway(ctx, store, blobs, nil, maxItems, watch)
}

func DrainWithGateway(ctx context.Context, store Store, blobs ports.BlobStore, gateway AIGateway, maxItems int, watch bool) (int, error) {
	return DrainWithGatewayAndExtensions(ctx, store, blobs, gateway, nil, maxItems, watch)
}

func DrainWithGatewayAndExtensions(ctx context.Context, store Store, blobs ports.BlobStore, gateway AIGateway, extensions ExtensionInvoker, maxItems int, watch bool) (int, error) {
	processed := 0
	for processed < maxItems {
		job, found, err := store.ClaimOperationJob(ctx)
		if err != nil {
			return processed, err
		}
		if !found {
			if !watch {
				return processed, nil
			}
			select {
			case <-ctx.Done():
				return processed, nil
			case <-time.After(500 * time.Millisecond):
				continue
			}
		}
		if err := execute(ctx, store, blobs, gateway, extensions, job.Type, job.Payload); err != nil {
			if failErr := store.FailOperationJob(ctx, job.ID, err); failErr != nil {
				return processed, failErr
			}
			continue
		}
		if err := store.CompleteOperationJob(ctx, job.ID); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func execute(ctx context.Context, store Store, blobs ports.BlobStore, gateway AIGateway, extensions ExtensionInvoker, jobType string, payload json.RawMessage) error {
	var input struct {
		RequestID   string          `json:"request_id"`
		TenantID    string          `json:"tenant_id"`
		WorkspaceID string          `json:"workspace_id"`
		AppID       string          `json:"app_id"`
		PipelineID  string          `json:"pipeline_id"`
		Cursor      string          `json:"cursor"`
		TaskType    string          `json:"task_type"`
		Input       json.RawMessage `json:"input"`
		Scopes      []string        `json:"scopes"`
	}
	if err := json.Unmarshal(payload, &input); err != nil {
		return errors.New("operation payload must be a JSON object")
	}
	switch jobType {
	case "privacy.export":
		if input.RequestID == "" {
			return errors.New("privacy.export payload requires request_id")
		}
		data, err := store.ExportPrivacyData(ctx, input.RequestID)
		if err != nil {
			return err
		}
		content, err := json.Marshal(data)
		if err != nil {
			return err
		}
		key := fmt.Sprintf("privacy/%s/%s/export.json", data.TenantID, input.RequestID)
		if err := blobs.Put(ctx, key, content, "application/json"); err != nil {
			return err
		}
		return store.CompletePrivacyExport(ctx, input.RequestID, key)
	case "connector.run":
		if extensions == nil {
			return errors.New("connector.run requires an extension host")
		}
		var connector struct {
			ExtensionID string          `json:"extension_id"`
			EventID     string          `json:"event_id"`
			EventType   string          `json:"event_type"`
			Event       json.RawMessage `json:"event"`
		}
		if err := json.Unmarshal(payload, &connector); err != nil {
			return errors.New("connector.run payload must be a JSON object")
		}
		if connector.ExtensionID == "" || connector.EventID == "" || connector.EventType == "" || len(connector.Event) == 0 {
			return errors.New("connector.run payload requires extension_id, event_id, event_type, and event")
		}
		connectorInput, err := json.Marshal(map[string]any{
			"event_id": connector.EventID, "event_type": connector.EventType,
			"idempotency_key": "connector:" + connector.ExtensionID + ":" + connector.EventID,
			"event":           json.RawMessage(connector.Event),
		})
		if err != nil {
			return err
		}
		principal := domain.Principal{TenantID: input.TenantID, WorkspaceID: input.WorkspaceID, ActorType: "system"}
		_, _, err = extensions.InvokeWithScope(ctx, principal, connector.ExtensionID, "deliver", "events:write", connectorInput)
		return err
	case "privacy.delete":
		if input.RequestID == "" {
			return errors.New("privacy.delete payload requires request_id")
		}
		objectKeys, err := store.DeletePrivacyData(ctx, input.RequestID)
		if err != nil {
			return err
		}
		for _, key := range objectKeys {
			if err := blobs.Delete(ctx, key); err != nil {
				return err
			}
		}
		return nil
	case "retention.enforce":
		tenantID := input.TenantID
		if tenantID == "" {
			return errors.New("retention.enforce payload requires tenant_id")
		}
		_, err := store.EnforceRetention(ctx, tenantID)
		return err
	case "ai.generate":
		return executeAIGeneration(ctx, store, blobs, gateway, input.RequestID, input.TaskType, input.Input, input.Scopes)
	case "scores.compute":
		return executeScoring(ctx, store, gateway, input.RequestID, input.Scopes)
	case "profiles.import":
		importStore, ok := store.(ImportStore)
		if !ok {
			return errors.New("operation store does not support imports")
		}
		return executeImport(ctx, importStore, blobs, input.RequestID)
	case "warehouse.sync":
		cs, ok := store.(ConnectorStore)
		if !ok {
			return errors.New("operation store does not support connector sources")
		}
		job := warehouseSyncInput{RequestID: input.RequestID, TenantID: input.TenantID, WorkspaceID: input.WorkspaceID, AppID: input.AppID, PipelineID: input.PipelineID, Cursor: input.Cursor}
		if len(input.Input) > 0 {
			if err := json.Unmarshal(input.Input, &job); err != nil {
				return fmt.Errorf("warehouse.sync input: %w", err)
			}
		}
		return executeWarehouseSync(ctx, cs, blobs, job)
	default:
		return fmt.Errorf("unsupported operation type %q", jobType)
	}
}

type warehouseSyncInput struct {
	RequestID   string `json:"request_id"`
	TenantID    string `json:"tenant_id"`
	WorkspaceID string `json:"workspace_id"`
	AppID       string `json:"app_id"`
	PipelineID  string `json:"pipeline_id"`
	Cursor      string `json:"cursor"`
}

func executeWarehouseSync(ctx context.Context, store ConnectorStore, blobs ports.BlobStore, job warehouseSyncInput) error {
	return executeWarehouseSyncWithRegistry(ctx, store, blobs, job, connector.DefaultRegistry())
}

func executeWarehouseSyncWithRegistry(ctx context.Context, store ConnectorStore, blobs ports.BlobStore, job warehouseSyncInput, registry *connector.Registry) error {
	if job.PipelineID == "" {
		return errors.New("warehouse.sync requires pipeline_id")
	}
	p := domain.Principal{TenantID: job.TenantID, WorkspaceID: job.WorkspaceID, AppID: job.AppID, ActorType: "connector"}
	pipeline, err := store.GetConnectorPipeline(ctx, p, job.PipelineID)
	if err != nil {
		return err
	}
	if pipeline.CurrentVersionID == nil {
		return errors.New("connector pipeline has no published version")
	}
	version, err := store.GetConnectorPipelineVersion(ctx, p, *pipeline.CurrentVersionID)
	if err != nil {
		return err
	}
	cfg, err := store.GetExtensionConfig(ctx, p, pipeline.ConnectorExtensionID)
	if err != nil {
		return err
	}
	resolved, err := extension.ResolveConfigMap(cfg.Config)
	if err != nil {
		return fmt.Errorf("connector config: %w", err)
	}
	var mapping map[string]any
	if err := json.Unmarshal(version.Mapping, &mapping); err != nil {
		return fmt.Errorf("pipeline mapping: %w", err)
	}
	resolved["mapping"] = mapping
	if registry == nil {
		return errors.New("connector registry is required")
	}
	driver := registry.For(stringValue(resolved, "driver"))
	rows, nextCursor, err := driver.Read(ctx, resolved, job.Cursor)
	if err != nil {
		return err
	}
	run := domain.ConnectorRun{TenantID: job.TenantID, WorkspaceID: job.WorkspaceID, AppID: pipeline.AppID, PipelineID: pipeline.ID, PipelineVersionID: version.ID, JobType: "warehouse.sync", Status: "running", Cursor: job.Cursor, RowsIn: int64(len(rows)), StartedAt: time.Now().UTC()}
	if err := store.RecordConnectorRun(ctx, run); err != nil {
		return err
	}
	valid, rejected := makeSourceEvents(rows, resolved, pipeline.ConnectorExtensionID, version.Version, job.Cursor)
	batchSize := 75
	if configured, ok := resolved["max_batch_size"].(float64); ok && configured >= 1 && configured <= 1000 {
		batchSize = int(configured)
	}
	for start := 0; start < len(valid); start += batchSize {
		end := start + batchSize
		if end > len(valid) {
			end = len(valid)
		}
		ids, acceptErr := store.AcceptEvents(ctx, p, valid[start:end])
		if acceptErr != nil {
			for row := start; row < end; row++ {
				rejected = append(rejected, reject{Row: row, Error: acceptErr.Error()})
			}
		} else {
			run.RowsOut += int64(len(ids))
		}
	}
	run.RowsRejected = int64(len(rejected))
	run.Cursor = nextCursor
	finished := time.Now().UTC()
	run.FinishedAt = &finished
	if len(rejected) > 0 && blobs != nil {
		data, _ := json.Marshal(rejected)
		run.RejectBlobKey = fmt.Sprintf("connectors/%s/%s/rejected/%s.json", job.TenantID, pipeline.ID, nextCursor)
		if err := blobs.Put(ctx, run.RejectBlobKey, data, "application/json"); err != nil {
			return err
		}
	}
	run.Status = "succeeded"
	if len(rejected) == len(rows) && len(rows) > 0 {
		run.Status = "failed"
	}
	return store.RecordConnectorRun(ctx, run)
}

type reject struct {
	Row   int    `json:"row"`
	Error string `json:"error"`
}

func makeSourceEvents(rows []connector.Row, cfg map[string]any, connectorID string, version int, cursor string) ([]domain.Event, []reject) {
	mapping, _ := cfg["mapping"].(map[string]any)
	if mapping == nil {
		mapping = cfg
	}
	eventType, _ := mapping["event_type"].(string)
	if eventType == "" {
		eventType = "profile.updated"
	}
	schemaVersion := 1
	if n, ok := mapping["schema_version"].(float64); ok && n > 0 {
		schemaVersion = int(n)
	}
	var events []domain.Event
	var rejects []reject
	for i, row := range rows {
		get := func(key string) string {
			source, _ := mapping[key].(string)
			if source == "" {
				source = key
			}
			value, ok := row[source]
			if !ok || value == nil {
				return ""
			}
			return strings.TrimSpace(fmt.Sprint(value))
		}
		externalID := get("external_id")
		anonymousID := get("anonymous_id")
		if externalID == "" && anonymousID == "" {
			rejects = append(rejects, reject{i, "external_id or anonymous_id is required"})
			continue
		}
		attrs := map[string]any{}
		if fields, ok := mapping["attributes"].(map[string]any); ok {
			for target, source := range fields {
				if s, ok := source.(string); ok {
					attrs[target] = row[s]
				}
			}
		}
		payload := map[string]any{"attributes": attrs}
		if raw, ok := mapping["payload"].(map[string]any); ok {
			payload = map[string]any{}
			for key, source := range raw {
				if s, ok := source.(string); ok {
					payload[key] = row[s]
				}
			}
		}
		data, _ := json.Marshal(payload)
		events = append(events, domain.Event{Type: eventType, SchemaVersion: schemaVersion, ExternalID: externalID, AnonymousID: anonymousID, IdempotencyKey: fmt.Sprintf("%s:%d:%s:%d", connectorID, version, cursor, i), OccurredAt: time.Now().UTC(), Source: "connector:" + connectorID, Payload: data})
	}
	return events, rejects
}

func stringValue(cfg map[string]any, key string) string { value, _ := cfg[key].(string); return value }

func executeImport(ctx context.Context, base ImportStore, blobs ports.BlobStore, requestID string) error {
	if requestID == "" {
		return errors.New("profiles.import payload requires request_id")
	}
	store := base
	job, key, rawMapping, err := store.GetImportJob(ctx, requestID)
	if err != nil {
		return err
	}
	if err := store.MarkImportProcessing(ctx, requestID); err != nil {
		return err
	}
	eventTime := job.CreatedAt.UTC()
	if eventTime.IsZero() {
		eventTime = time.Now().UTC().Truncate(time.Microsecond)
	}
	data, err := blobs.Get(ctx, key)
	if err != nil {
		return err
	}
	mapping := map[string]string{}
	if err := json.Unmarshal(rawMapping, &mapping); err != nil {
		return fmt.Errorf("mapping: %w", err)
	}
	reader := csv.NewReader(bytes.NewReader(data))
	header, err := reader.Read()
	if err != nil {
		return err
	}
	indices := map[string]int{}
	for i, name := range header {
		indices[name] = i
	}
	type result struct {
		Row    int    `json:"row"`
		Status string `json:"status"`
		Error  string `json:"error,omitempty"`
	}
	results := []result{}
	imported, failed, total := 0, 0, 0
	for {
		row, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		total++
		line := total + 1
		if readErr != nil {
			failed++
			results = append(results, result{line, "failed", readErr.Error()})
			continue
		}
		value := func(column string) string {
			i, ok := indices[column]
			if !ok || i >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[i])
		}
		externalID := value("external_id")
		if externalID == "" {
			for column, target := range mapping {
				if target == "external_id" {
					externalID = value(column)
					break
				}
			}
		}
		if externalID == "" {
			externalID = value("email")
		}
		if job.Kind == "suppressions" && externalID == "" {
			externalID = value("endpoint")
		}
		if externalID == "" {
			failed++
			results = append(results, result{line, "failed", "external_id or email is required"})
			continue
		}
		attrs := map[string]any{}
		for column, target := range mapping {
			if target != "" && target != "external_id" {
				if v := value(column); v != "" {
					attrs[target] = v
				}
			}
		}
		if email := value("email"); email != "" {
			if suppressed, _ := store.IsSuppressed(ctx, domain.Principal{TenantID: job.TenantID, WorkspaceID: job.WorkspaceID, AppID: job.AppID}, "email", email); suppressed {
				failed++
				results = append(results, result{line, "skipped", "endpoint is suppressed"})
				continue
			}
		}
		payload, _ := json.Marshal(map[string]any{"attributes": attrs})
		event := domain.Event{Type: "profile.updated", SchemaVersion: 1, ExternalID: externalID, IdempotencyKey: "profiles.import:" + key + ":" + strconv.Itoa(total), OccurredAt: eventTime, Source: "profiles.import", Payload: payload}
		if job.Kind == "companies" {
			name := value("name")
			if name == "" {
				name = externalID
			}
			cp, _ := json.Marshal(map[string]any{"company": map[string]any{"external_id": externalID, "name": name, "attributes": attrs}, "members": []any{}})
			event = domain.Event{Type: "company.updated", SchemaVersion: 1, ExternalID: externalID, IdempotencyKey: "companies.import:" + key + ":" + strconv.Itoa(total), OccurredAt: eventTime, Source: "profiles.import", Payload: cp}
		}
		if job.Kind == "suppressions" {
			channel := value("channel")
			endpoint := value("endpoint")
			if channel == "" || endpoint == "" {
				failed++
				results = append(results, result{line, "failed", "channel and endpoint are required"})
				continue
			}
			cp, _ := json.Marshal(map[string]any{"channel": channel, "state": "unsubscribed", "evidence": map[string]any{"source": "profiles.import", "row": line}})
			event = domain.Event{Type: "consent.changed", SchemaVersion: 1, ExternalID: externalID, IdempotencyKey: "suppressions.import:" + key + ":" + strconv.Itoa(total), OccurredAt: eventTime, Source: "profiles.import", Payload: cp}
		}
		_, err = store.AcceptEvents(ctx, domain.Principal{TenantID: job.TenantID, WorkspaceID: job.WorkspaceID, AppID: job.AppID, ActorType: "import"}, []domain.Event{event})
		if err != nil {
			failed++
			results = append(results, result{line, "failed", err.Error()})
		} else {
			imported++
			results = append(results, result{line, "imported", ""})
		}
	}
	content, _ := json.Marshal(results)
	resultKey := fmt.Sprintf("imports/%s/results.json", requestID)
	if err := blobs.Put(ctx, resultKey, content, "application/json"); err != nil {
		return err
	}
	return store.CompleteImport(ctx, requestID, resultKey, total, imported, failed)
}

func executeAIGeneration(ctx context.Context, store Store, blobs ports.BlobStore, gateway AIGateway, requestID, taskType string, rawInput json.RawMessage, scopes []string) error {
	if requestID == "" {
		return errors.New("ai.generate payload requires request_id")
	}
	if gateway == nil {
		return errors.New("ai.generate requires an AI gateway")
	}
	jobStore, ok := store.(AIGenerationStore)
	if !ok {
		return errors.New("operation store does not support AI generation")
	}
	job, err := jobStore.GetAIGenerationJob(ctx, requestID)
	if err != nil {
		return err
	}
	if taskType == "" {
		return errors.New("ai.generate payload requires task_type")
	}
	var input struct {
		Prompt          string          `json:"prompt"`
		SystemPrompt    string          `json:"system_prompt"`
		PromptVersionID string          `json:"prompt_version_id"`
		OutputSchema    json.RawMessage `json:"output_schema"`
		Model           string          `json:"model"`
		Temperature     float64         `json:"temperature"`
		MaxTokens       int             `json:"max_tokens"`
	}
	if len(rawInput) == 0 || !json.Valid(rawInput) {
		return errors.New("ai.generate input must be valid JSON")
	}
	if err := json.Unmarshal(rawInput, &input); err != nil {
		return err
	}
	if input.PromptVersionID == "" {
		return errors.New("ai.generate input requires prompt_version_id")
	}
	if input.Prompt == "" {
		return errors.New("ai.generate input requires prompt")
	}
	principal := domain.Principal{
		TenantID: job.TenantID, WorkspaceID: job.WorkspaceID, UserID: job.RequestedBy,
		ActorType: "ai_agent", Scopes: append([]string(nil), scopes...),
	}
	promptVersion, err := jobStore.GetPromptVersion(ctx, principal, input.PromptVersionID)
	if err != nil {
		return err
	}
	if promptVersion.Status != "active" || promptVersion.EvalStatus != "passed" {
		return fmt.Errorf("prompt version %q is not usable", input.PromptVersionID)
	}
	if err := jobStore.MarkAIGenerationProcessing(ctx, requestID); err != nil {
		return err
	}
	response, err := gateway.Generate(ctx, principal, ai.GenerateRequest{
		Prompt: input.Prompt, SystemPrompt: input.SystemPrompt, PromptVersionID: input.PromptVersionID,
		OutputSchema: promptVersion.OutputSchema, Model: promptVersion.Model, Temperature: input.Temperature, MaxTokens: input.MaxTokens,
		Action: "ai." + taskType,
	})
	if err != nil {
		return err
	}
	content, err := json.Marshal(struct {
		TaskType string   `json:"task_type"`
		Content  string   `json:"content"`
		Usage    ai.Usage `json:"usage"`
	}{taskType, response.Content, response.Usage})
	if err != nil {
		return err
	}
	key := fmt.Sprintf("ai/generations/%s/%s.json", job.TenantID, requestID)
	if err := blobs.Put(ctx, key, content, "application/json"); err != nil {
		return err
	}
	return jobStore.CompleteAIGeneration(ctx, requestID, key)
}

type ScoringStore interface {
	ports.Store
	GetScoringJob(context.Context, string) (domain.ScoringJob, error)
	GetScoringModel(context.Context, domain.Principal, string) (domain.ScoringModel, error)
	GetScoringModelVersion(context.Context, domain.Principal, string) (domain.ScoringModelVersion, error)
	GetScoringModelVersionByNumber(context.Context, domain.Principal, string, int) (domain.ScoringModelVersion, error)
	MarkScoringProcessing(context.Context, string) error
	CompleteScoring(context.Context, string) error
	FailScoring(context.Context, string, string) error
	ResolveSegment(context.Context, domain.Principal, string) ([]string, error)
	GetProfileByIDSystem(context.Context, string, string, string) (domain.Profile, error)
	GetFirstAppID(context.Context, string, string) (string, error)
	UpsertProfileScores(context.Context, []domain.ProfileScore) error
	GetEventCount(ctx context.Context, tenantID, workspaceID, externalID, anonymousID, eventType string, days int) (int64, error)
}

func executeScoring(ctx context.Context, store Store, gateway AIGateway, requestID string, scopes []string) error {
	if requestID == "" {
		return errors.New("scores.compute payload requires request_id")
	}
	scoringStore, ok := store.(ScoringStore)
	if !ok {
		return errors.New("operation store does not support scoring compute")
	}
	job, err := scoringStore.GetScoringJob(ctx, requestID)
	if err != nil {
		return err
	}

	principal := domain.Principal{
		TenantID: job.TenantID, WorkspaceID: job.WorkspaceID, UserID: job.RequestedBy,
		ActorType: "ai_agent", Scopes: append([]string(nil), scopes...),
	}

	model, err := scoringStore.GetScoringModel(ctx, principal, job.ScoringModelID)
	if err != nil {
		return err
	}
	if model.CurrentVersionID == nil || *model.CurrentVersionID == "" {
		return fmt.Errorf("scoring model %q has no active version", job.ScoringModelID)
	}

	sv, err := scoringStore.GetScoringModelVersion(ctx, principal, *model.CurrentVersionID)
	if err != nil {
		return err
	}
	if sv.Status != "active" || sv.EvalStatus != "passed" {
		return fmt.Errorf("scoring model version %q is not active or passed", sv.ID)
	}

	if err := scoringStore.MarkScoringProcessing(ctx, requestID); err != nil {
		return err
	}

	profileIDs, err := scoringStore.ResolveSegment(ctx, principal, job.SegmentID)
	if err != nil {
		return err
	}

	appID, err := scoringStore.GetFirstAppID(ctx, job.TenantID, job.WorkspaceID)
	if err != nil {
		return err
	}

	var scores []domain.ProfileScore
	for _, profileID := range profileIDs {
		profile, err := scoringStore.GetProfileByIDSystem(ctx, job.TenantID, job.WorkspaceID, profileID)
		if err != nil {
			return err
		}

		var profileAttrs map[string]any
		if len(profile.Attributes) > 0 {
			_ = json.Unmarshal(profile.Attributes, &profileAttrs)
		}
		if profileAttrs == nil {
			profileAttrs = make(map[string]any)
		}

		var scoreVal float64
		if model.Kind == "expression" {
			var def struct {
				Expr string `json:"expr"`
			}
			if err := json.Unmarshal(sv.Definition, &def); err != nil {
				return err
			}
			env, err := scoring.BuildExpressionEnv(ctx, scoringStore, job.TenantID, job.WorkspaceID, profile, def.Expr)
			if err != nil {
				return err
			}
			scoreVal, err = scoring.Evaluate(def.Expr, env, sv.OutputMin, sv.OutputMax)
			if err != nil {
				return err
			}
		} else if model.Kind == "llm" {
			if gateway == nil {
				return errors.New("scores.compute for llm kind requires an AI gateway")
			}
			aiGateway, ok := gateway.(*ai.Gateway)
			if !ok {
				return errors.New("ai gateway is not of type *ai.Gateway")
			}
			env := map[string]any{
				"profile": profileAttrs,
			}
			scoreVal, err = scoring.EvaluateLLM(ctx, scoringStore, aiGateway, principal, sv, env)
			if err != nil {
				return err
			}
		} else {
			return fmt.Errorf("unsupported scoring model kind: %s", model.Kind)
		}

		scores = append(scores, domain.ProfileScore{
			TenantID:       job.TenantID,
			WorkspaceID:    job.WorkspaceID,
			AppID:          appID,
			ProfileID:      profileID,
			ScoringModelID: job.ScoringModelID,
			ScoreName:      sv.ScoreName,
			Value:          scoreVal,
			ModelVersion:   sv.Version,
		})
	}

	if len(scores) > 0 {
		if err := scoringStore.UpsertProfileScores(ctx, scores); err != nil {
			return err
		}
		telemetry.ScoresComputed.Add(ctx, int64(len(scores)))
	}

	return scoringStore.CompleteScoring(ctx, requestID)
}
