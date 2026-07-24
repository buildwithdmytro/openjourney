package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	journeygraph "github.com/buildwithdmytro/openjourney/internal/journey"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateJourney(ctx context.Context, p domain.Principal, j domain.Journey) (domain.Journey, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Journey{}, err
	}
	defer tx.Rollback(ctx)

	if j.Name == "" {
		return domain.Journey{}, errors.New("journey name is required")
	}
	if j.Status == "" {
		j.Status = "draft"
	}
	if len(j.Graph) == 0 {
		j.Graph = json.RawMessage("{}")
	}

	var out domain.Journey
	err = tx.QueryRow(ctx, `INSERT INTO journeys (tenant_id, workspace_id, name, description, status, graph)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, tenant_id, workspace_id, name, description, status, graph, latest_version, current_version_id, created_at, updated_at`,
		p.TenantID, p.WorkspaceID, j.Name, j.Description, j.Status, j.Graph).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Description, &out.Status, &out.Graph, &out.LatestVersion, &out.CurrentVersionID, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return domain.Journey{}, err
	}

	if err := s.audit(ctx, tx, p, "journey.create", "journey", out.ID, map[string]any{"name": out.Name}); err != nil {
		return domain.Journey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Journey{}, err
	}
	return out, nil
}

func (s *Store) GetJourney(ctx context.Context, p domain.Principal, id string) (domain.Journey, error) {
	var out domain.Journey
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, name, description, status, graph, latest_version, current_version_id, created_at, updated_at
		FROM journeys WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`,
		p.TenantID, p.WorkspaceID, id).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Description, &out.Status, &out.Graph, &out.LatestVersion, &out.CurrentVersionID, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Journey{}, ErrNotFound
	}
	return out, err
}

func (s *Store) UpdateJourney(ctx context.Context, p domain.Principal, j domain.Journey) (domain.Journey, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Journey{}, err
	}
	defer tx.Rollback(ctx)

	existing, err := s.GetJourney(ctx, p, j.ID)
	if err != nil {
		return domain.Journey{}, err
	}
	if existing.Status == "published" && j.Status != "draft" {
		return domain.Journey{}, errors.New("published journeys cannot be edited without reverting to draft")
	}
	if j.Name == "" {
		j.Name = existing.Name
	}
	if j.Status == "" {
		j.Status = existing.Status
	}
	if len(j.Graph) == 0 {
		j.Graph = existing.Graph
	}

	var out domain.Journey
	err = tx.QueryRow(ctx, `UPDATE journeys
		SET name=$4, description=$5, status=$6, graph=$7, updated_at=now()
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3
		RETURNING id, tenant_id, workspace_id, name, description, status, graph, latest_version, current_version_id, created_at, updated_at`,
		p.TenantID, p.WorkspaceID, j.ID, j.Name, j.Description, j.Status, j.Graph).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Description, &out.Status, &out.Graph, &out.LatestVersion, &out.CurrentVersionID, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Journey{}, ErrNotFound
	}
	if err != nil {
		return domain.Journey{}, err
	}

	if err := s.audit(ctx, tx, p, "journey.update", "journey", out.ID, map[string]any{"status": out.Status}); err != nil {
		return domain.Journey{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Journey{}, err
	}
	return out, nil
}

func (s *Store) ListJourneys(ctx context.Context, p domain.Principal) ([]domain.Journey, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, workspace_id, name, description, status, graph, latest_version, current_version_id, created_at, updated_at
		FROM journeys WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY created_at DESC`,
		p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Journey
	for rows.Next() {
		var j domain.Journey
		if err := rows.Scan(&j.ID, &j.TenantID, &j.WorkspaceID, &j.Name, &j.Description, &j.Status, &j.Graph, &j.LatestVersion, &j.CurrentVersionID, &j.CreatedAt, &j.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) PublishJourney(ctx context.Context, p domain.Principal, journeyID string, approverUserID string, manifestKey string) (domain.JourneyVersion, error) {
	if approverUserID == "" {
		return domain.JourneyVersion{}, errors.New("approver user id is required")
	}
	if manifestKey == "" {
		return domain.JourneyVersion{}, errors.New("manifest key is required")
	}
	if err := s.CheckMakerChecker(ctx, p, "journeys", journeyID, ""); err != nil {
		return domain.JourneyVersion{}, err
	}


	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.JourneyVersion{}, err
	}
	defer tx.Rollback(ctx)

	var draft domain.Journey
	err = tx.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, name, description, status, graph, latest_version, current_version_id, created_at, updated_at
		FROM journeys WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3 FOR UPDATE`,
		p.TenantID, p.WorkspaceID, journeyID).
		Scan(&draft.ID, &draft.TenantID, &draft.WorkspaceID, &draft.Name, &draft.Description, &draft.Status, &draft.Graph, &draft.LatestVersion, &draft.CurrentVersionID, &draft.CreatedAt, &draft.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.JourneyVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.JourneyVersion{}, err
	}

	graph, err := journeygraph.ParseGraph(draft.Graph)
	if err != nil {
		return domain.JourneyVersion{}, err
	}
	if err := journeygraph.Validate(graph); err != nil {
		return domain.JourneyVersion{}, err
	}
	canonicalGraph, err := json.Marshal(graph)
	if err != nil {
		return domain.JourneyVersion{}, fmt.Errorf("marshal journey graph: %w", err)
	}
	digest := sha256.Sum256(canonicalGraph)
	if strings.Contains(manifestKey, "/manifests/") && !strings.HasSuffix(manifestKey, fmt.Sprintf("/manifests/%x.json", digest)) {
		return domain.JourneyVersion{}, errors.New("journey draft changed during publication; retry publish")
	}
	entry, err := publishEntryConfig(graph)
	if err != nil {
		return domain.JourneyVersion{}, err
	}
	conversionGoal, attributionWindow, err := publishConversionGoal(graph)
	if err != nil {
		return domain.JourneyVersion{}, err
	}

	version := draft.LatestVersion + 1
	var out domain.JourneyVersion
	err = tx.QueryRow(ctx, `INSERT INTO journey_versions
		(journey_id, tenant_id, workspace_id, version, graph, manifest_key, entry_kind, entry_event_type, entry_segment_id, entry_schedule, reentry_policy, max_reentries, late_policy, conversion_goal, attribution_window, status, published_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, '')::uuid, NULLIF($10, ''), $11, $12, $13, $14, NULLIF($15, '')::interval, 'active', NULLIF($16, '')::uuid)
		RETURNING id, journey_id, tenant_id, workspace_id, version, graph, manifest_key, entry_kind, entry_event_type, entry_segment_id, entry_schedule, reentry_policy, max_reentries, late_policy, conversion_goal, attribution_window::text, status, published_by, published_at`,
		draft.ID, draft.TenantID, draft.WorkspaceID, version, draft.Graph, manifestKey,
		entry.EntryKind, entry.EventType, entry.SegmentID, entry.Schedule, entry.ReentryPolicy, entry.MaxReentries, entry.LatePolicy, nullableJSON(conversionGoal), attributionWindow, approverUserID).
		Scan(&out.ID, &out.JourneyID, &out.TenantID, &out.WorkspaceID, &out.Version, &out.Graph, &out.ManifestKey,
			&out.EntryKind, &out.EntryEventType, &out.EntrySegmentID, &out.EntrySchedule, &out.ReentryPolicy,
			&out.MaxReentries, &out.LatePolicy, &out.ConversionGoal, &out.AttributionWindow, &out.Status, &out.PublishedBy, &out.PublishedAt)
	if err != nil {
		return domain.JourneyVersion{}, err
	}

	if _, err := tx.Exec(ctx, `UPDATE journeys
		SET status='published', current_version_id=$1, latest_version=$2, updated_at=now()
		WHERE tenant_id=$3 AND workspace_id=$4 AND id=$5`,
		out.ID, out.Version, p.TenantID, p.WorkspaceID, draft.ID); err != nil {
		return domain.JourneyVersion{}, err
	}

		if err := s.audit(ctx, tx, p, "journey.publish", "journey", draft.ID, map[string]any{"version": out.Version, "manifest_key": manifestKey}); err != nil {
		return domain.JourneyVersion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.JourneyVersion{}, err
	}
	return out, nil
}

func publishConversionGoal(graph *journeygraph.Graph) (json.RawMessage, string, error) {
	for _, node := range graph.Nodes {
		if node.Type != journeygraph.NodeTypeGoal {
			continue
		}
		cfgAny, err := journeygraph.DecodeConfig(node)
		if err != nil {
			return nil, "", err
		}
		cfg := cfgAny.(journeygraph.GoalConfig)
		// A Milestone 3 path goal only has a name. An analytical conversion goal
		// additionally names the accepted event and its attribution window.
		if cfg.EventType == "" || cfg.Window == "" {
			continue
		}
		goal, err := json.Marshal(cfg)
		if err != nil {
			return nil, "", err
		}
		return goal, cfg.Window, nil
	}
	return nil, "", nil
}

type publishEntry struct {
	EntryKind     string
	EventType     string
	SegmentID     string
	Schedule      string
	ReentryPolicy string
	MaxReentries  int
	LatePolicy    string
}

func publishEntryConfig(graph *journeygraph.Graph) (publishEntry, error) {
	for _, node := range graph.Nodes {
		if node.ID != graph.EntryNodeID || node.Type != journeygraph.NodeTypeEntry {
			continue
		}
		cfgAny, err := journeygraph.DecodeConfig(node)
		if err != nil {
			return publishEntry{}, err
		}
		cfg := cfgAny.(journeygraph.EntryConfig)
		entry := publishEntry{
			EntryKind:     cfg.Trigger,
			EventType:     cfg.EventType,
			SegmentID:     cfg.SegmentID,
			Schedule:      cfg.Schedule,
			ReentryPolicy: cfg.ReentryPolicy,
			MaxReentries:  cfg.MaxReentries,
			LatePolicy:    cfg.LatePolicy,
		}
		if entry.ReentryPolicy == "" {
			entry.ReentryPolicy = "once"
		}
		if entry.LatePolicy == "" {
			entry.LatePolicy = "run"
		}
		switch entry.EntryKind {
		case "event":
			if entry.EventType == "" {
				return publishEntry{}, errors.New("event entry requires event_type")
			}
		case "scheduled":
			if entry.SegmentID == "" && entry.Schedule == "" {
				return publishEntry{}, errors.New("scheduled entry requires segment_id or schedule")
			}
		default:
			return publishEntry{}, errors.New("entry trigger must be event or scheduled")
		}
		return entry, nil
	}
	return publishEntry{}, errors.New("entry node not found")
}

func (s *Store) GetJourneyVersion(ctx context.Context, tenantID, versionID string) (domain.JourneyVersion, error) {
	var out domain.JourneyVersion
	err := s.pool.QueryRow(ctx, `SELECT id, journey_id, tenant_id, workspace_id, version, graph, manifest_key,
			entry_kind, entry_event_type, entry_segment_id, entry_schedule, reentry_policy,
			max_reentries, late_policy, conversion_goal, attribution_window::text, status, published_by, published_at
		FROM journey_versions
		WHERE tenant_id=$1 AND id=$2`,
		tenantID, versionID).
		Scan(&out.ID, &out.JourneyID, &out.TenantID, &out.WorkspaceID, &out.Version, &out.Graph, &out.ManifestKey,
			&out.EntryKind, &out.EntryEventType, &out.EntrySegmentID, &out.EntrySchedule, &out.ReentryPolicy,
			&out.MaxReentries, &out.LatePolicy, &out.ConversionGoal, &out.AttributionWindow, &out.Status, &out.PublishedBy, &out.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.JourneyVersion{}, ErrNotFound
	}
	return out, err
}

func (s *Store) GetJourneyVersionNumber(ctx context.Context, p domain.Principal, journeyID string, version int) (domain.JourneyVersion, error) {
	var out domain.JourneyVersion
	err := s.pool.QueryRow(ctx, `SELECT id, journey_id, tenant_id, workspace_id, version, graph, manifest_key,
			entry_kind, entry_event_type, entry_segment_id, entry_schedule, reentry_policy,
			max_reentries, late_policy, conversion_goal, attribution_window::text, status, published_by, published_at
		FROM journey_versions
		WHERE tenant_id=$1 AND workspace_id=$2 AND journey_id=$3 AND version=$4`,
		p.TenantID, p.WorkspaceID, journeyID, version).
		Scan(&out.ID, &out.JourneyID, &out.TenantID, &out.WorkspaceID, &out.Version, &out.Graph, &out.ManifestKey,
			&out.EntryKind, &out.EntryEventType, &out.EntrySegmentID, &out.EntrySchedule, &out.ReentryPolicy,
			&out.MaxReentries, &out.LatePolicy, &out.ConversionGoal, &out.AttributionWindow, &out.Status, &out.PublishedBy, &out.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.JourneyVersion{}, ErrNotFound
	}
	return out, err
}

func (s *Store) SetJourneyVersionStatus(ctx context.Context, p domain.Principal, journeyID string, version int, status string) error {
	res, err := s.pool.Exec(ctx, `UPDATE journey_versions SET status = $1, published_at = now()
		WHERE tenant_id = $2 AND workspace_id = $3 AND journey_id = $4 AND version = $5`,
		status, p.TenantID, p.WorkspaceID, journeyID, version)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
