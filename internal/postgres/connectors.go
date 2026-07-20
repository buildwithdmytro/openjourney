package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

const connectorPipelineColumns = `id, tenant_id, workspace_id, app_id, connector_extension_id,
 name, direction, status, current_version_id, schedule_enabled, schedule_interval_seconds,
 next_run_at, last_run_at, created_at, updated_at`

func scanConnectorPipeline(row pgx.Row, out *domain.ConnectorPipeline) error {
	return row.Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.AppID, &out.ConnectorExtensionID,
		&out.Name, &out.Direction, &out.Status, &out.CurrentVersionID, &out.ScheduleEnabled,
		&out.ScheduleIntervalSeconds, &out.NextRunAt, &out.LastRunAt, &out.CreatedAt, &out.UpdatedAt)
}

func (s *Store) CreateConnectorPipeline(ctx context.Context, p domain.Principal, pipeline domain.ConnectorPipeline) (domain.ConnectorPipeline, error) {
	if pipeline.Name == "" || pipeline.ConnectorExtensionID == "" {
		return domain.ConnectorPipeline{}, errors.New("connector pipeline name and connector are required")
	}
	if pipeline.Direction != "source" && pipeline.Direction != "sink" && pipeline.Direction != "export" {
		return domain.ConnectorPipeline{}, errors.New("invalid connector pipeline direction")
	}
	var out domain.ConnectorPipeline
	err := scanConnectorPipeline(s.pool.QueryRow(ctx, `INSERT INTO connector_pipelines
		(tenant_id, workspace_id, app_id, connector_extension_id, name, direction, status,
		 schedule_enabled, schedule_interval_seconds, next_run_at)
		VALUES ($1,$2,$3,$4,$5,$6,COALESCE(NULLIF($7,''),'draft'),$8,$9,$10)
		RETURNING `+connectorPipelineColumns,
		p.TenantID, p.WorkspaceID, pipeline.AppID, pipeline.ConnectorExtensionID, pipeline.Name,
		pipeline.Direction, pipeline.Status, pipeline.ScheduleEnabled, pipeline.ScheduleIntervalSeconds,
		pipeline.NextRunAt), &out)
	if err != nil {
		return domain.ConnectorPipeline{}, err
	}
	return out, nil
}

func (s *Store) ListConnectorPipelines(ctx context.Context, p domain.Principal) ([]domain.ConnectorPipeline, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+connectorPipelineColumns+`
		FROM connector_pipelines WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY created_at DESC`, p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ConnectorPipeline
	for rows.Next() {
		var item domain.ConnectorPipeline
		if err := rows.Scan(&item.ID, &item.TenantID, &item.WorkspaceID, &item.AppID, &item.ConnectorExtensionID,
			&item.Name, &item.Direction, &item.Status, &item.CurrentVersionID, &item.ScheduleEnabled,
			&item.ScheduleIntervalSeconds, &item.NextRunAt, &item.LastRunAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetConnectorPipeline(ctx context.Context, p domain.Principal, id string) (domain.ConnectorPipeline, error) {
	var out domain.ConnectorPipeline
	err := scanConnectorPipeline(s.pool.QueryRow(ctx, `SELECT `+connectorPipelineColumns+`
		FROM connector_pipelines WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`, p.TenantID, p.WorkspaceID, id), &out)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ConnectorPipeline{}, ErrNotFound
	}
	return out, err
}

func (s *Store) ListConnectorRuns(ctx context.Context, p domain.Principal, pipelineID string) ([]domain.ConnectorRun, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,tenant_id,workspace_id,app_id,pipeline_id,pipeline_version_id,
		job_type,status,cursor,rows_in,rows_out,rows_rejected,COALESCE(reject_blob_key,''),COALESCE(error,''),started_at,finished_at
		FROM connector_runs WHERE tenant_id=$1 AND workspace_id=$2 AND pipeline_id=$3 ORDER BY started_at DESC LIMIT 100`,
		p.TenantID, p.WorkspaceID, pipelineID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ConnectorRun
	for rows.Next() {
		var run domain.ConnectorRun
		if err := rows.Scan(&run.ID, &run.TenantID, &run.WorkspaceID, &run.AppID, &run.PipelineID,
			&run.PipelineVersionID, &run.JobType, &run.Status, &run.Cursor, &run.RowsIn, &run.RowsOut,
			&run.RowsRejected, &run.RejectBlobKey, &run.Error, &run.StartedAt, &run.FinishedAt); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}

func (s *Store) GetConnectorPipelineVersion(ctx context.Context, p domain.Principal, id string) (domain.ConnectorPipelineVersion, error) {
	var out domain.ConnectorPipelineVersion
	err := s.pool.QueryRow(ctx, `SELECT v.id,v.pipeline_id,v.version,v.mapping_key,v.mapping,v.definition_sha,v.created_by_user_id,v.created_at
		FROM connector_pipeline_versions v JOIN connector_pipelines p ON p.id=v.pipeline_id
		WHERE p.tenant_id=$1 AND p.workspace_id=$2 AND v.id=$3`, p.TenantID, p.WorkspaceID, id).
		Scan(&out.ID, &out.PipelineID, &out.Version, &out.MappingKey, &out.Mapping, &out.DefinitionSHA, &out.CreatedByUserID, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ConnectorPipelineVersion{}, ErrNotFound
	}
	return out, err
}

func (s *Store) RecordConnectorRun(ctx context.Context, run domain.ConnectorRun) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO connector_runs
		(tenant_id,workspace_id,app_id,pipeline_id,pipeline_version_id,job_type,status,cursor,rows_in,rows_out,rows_rejected,reject_blob_key,error,started_at,finished_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,NULLIF($12,''),NULLIF($13,''),COALESCE($14,now()),$15)`,
		run.TenantID, run.WorkspaceID, run.AppID, run.PipelineID, run.PipelineVersionID, run.JobType, run.Status,
		run.Cursor, run.RowsIn, run.RowsOut, run.RowsRejected, run.RejectBlobKey, run.Error, run.StartedAt, run.FinishedAt)
	return err
}

// ReplayConnectorRun re-queues the source page identified by a failed run's
// cursor. Source idempotency keys make this safe when the page contains rows
// that were already accepted; the quarantined rows are retried after the
// operator fixes the mapping or destination condition.
func (s *Store) ReplayConnectorRun(ctx context.Context, p domain.Principal, runID string) (string, error) {
	var tenantID, workspaceID, appID, pipelineID, cursor, rejectBlob string
	err := s.pool.QueryRow(ctx, `SELECT tenant_id,workspace_id,app_id,pipeline_id,cursor,reject_blob_key
		FROM connector_runs WHERE id=$1 AND tenant_id=$2 AND workspace_id=$3
			AND status='failed' AND reject_blob_key IS NOT NULL`, runID, p.TenantID, p.WorkspaceID).
		Scan(&tenantID, &workspaceID, &appID, &pipelineID, &cursor, &rejectBlob)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(map[string]any{"tenant_id": tenantID, "workspace_id": workspaceID, "app_id": appID, "pipeline_id": pipelineID, "cursor": cursor, "replay_run_id": runID, "reject_blob_key": rejectBlob})
	if err != nil {
		return "", err
	}
	var jobID string
	err = s.pool.QueryRow(ctx, `INSERT INTO operation_jobs(tenant_id,workspace_id,job_type,payload)
		VALUES($1,$2,'warehouse.sync',$3) RETURNING id`, tenantID, workspaceID, payload).Scan(&jobID)
	return jobID, err
}

func (s *Store) UpdateConnectorPipeline(ctx context.Context, p domain.Principal, pipeline domain.ConnectorPipeline) (domain.ConnectorPipeline, error) {
	if pipeline.Name == "" || pipeline.ConnectorExtensionID == "" {
		return domain.ConnectorPipeline{}, errors.New("connector pipeline name and connector are required")
	}
	if pipeline.Direction != "source" && pipeline.Direction != "sink" && pipeline.Direction != "export" {
		return domain.ConnectorPipeline{}, errors.New("invalid connector pipeline direction")
	}
	var out domain.ConnectorPipeline
	err := scanConnectorPipeline(s.pool.QueryRow(ctx, `UPDATE connector_pipelines
		SET app_id=$4, connector_extension_id=$5, name=$6, direction=$7,
			status=CASE WHEN status='enabled' THEN status ELSE COALESCE(NULLIF($8,''),status) END,
			schedule_enabled=$9, schedule_interval_seconds=$10, next_run_at=$11, updated_at=now()
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3
		RETURNING `+connectorPipelineColumns,
		p.TenantID, p.WorkspaceID, pipeline.ID, pipeline.AppID, pipeline.ConnectorExtensionID,
		pipeline.Name, pipeline.Direction, pipeline.Status, pipeline.ScheduleEnabled,
		pipeline.ScheduleIntervalSeconds, pipeline.NextRunAt), &out)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ConnectorPipeline{}, ErrNotFound
	}
	return out, err
}

func (s *Store) PublishConnectorPipeline(ctx context.Context, p domain.Principal, id, publisher, manifestKey string, definition json.RawMessage, definitionSHA string) (domain.ConnectorPipelineVersion, error) {
	if publisher == "" || manifestKey == "" || len(definition) == 0 || definitionSHA == "" {
		return domain.ConnectorPipelineVersion{}, errors.New("publisher, manifest, definition, and definition sha are required")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.ConnectorPipelineVersion{}, err
	}
	defer tx.Rollback(ctx)
	var current *string
	var latest int
	err = tx.QueryRow(ctx, `SELECT current_version_id,
		COALESCE((SELECT MAX(version) FROM connector_pipeline_versions WHERE pipeline_id=connector_pipelines.id),0)
		FROM connector_pipelines WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3 FOR UPDATE`,
		p.TenantID, p.WorkspaceID, id).Scan(&current, &latest)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ConnectorPipelineVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.ConnectorPipelineVersion{}, err
	}
	if current != nil {
		var existing domain.ConnectorPipelineVersion
		err = tx.QueryRow(ctx, `SELECT id,pipeline_id,version,mapping_key,mapping,definition_sha,created_by_user_id,created_at
			FROM connector_pipeline_versions WHERE id=$1`, *current).Scan(&existing.ID, &existing.PipelineID,
			&existing.Version, &existing.MappingKey, &existing.Mapping, &existing.DefinitionSHA, &existing.CreatedByUserID, &existing.CreatedAt)
		if err != nil {
			return domain.ConnectorPipelineVersion{}, err
		}
		if existing.DefinitionSHA == definitionSHA {
			return existing, nil
		}
	}
	var out domain.ConnectorPipelineVersion
	err = tx.QueryRow(ctx, `INSERT INTO connector_pipeline_versions
		(pipeline_id,version,mapping_key,mapping,definition_sha,created_by_user_id)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id,pipeline_id,version,mapping_key,mapping,definition_sha,created_by_user_id,created_at`,
		id, latest+1, manifestKey, definition, definitionSHA, publisher).Scan(&out.ID, &out.PipelineID,
		&out.Version, &out.MappingKey, &out.Mapping, &out.DefinitionSHA, &out.CreatedByUserID, &out.CreatedAt)
	if err != nil {
		return domain.ConnectorPipelineVersion{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE connector_pipelines SET current_version_id=$1,status='enabled',updated_at=now()
		WHERE tenant_id=$2 AND workspace_id=$3 AND id=$4`, out.ID, p.TenantID, p.WorkspaceID, id); err != nil {
		return domain.ConnectorPipelineVersion{}, fmt.Errorf("activate connector pipeline: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ConnectorPipelineVersion{}, err
	}
	return out, nil
}

// ClaimDueConnectorPipeline atomically leases one due pipeline and enqueues
// its operation. The row lock is held until both the schedule advance and job
// insert commit, so concurrent scheduler workers cannot enqueue the same run.
func (s *Store) ClaimDueConnectorPipeline(ctx context.Context) (bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var pipelineID, tenantID, workspaceID, appID, direction string
	var intervalSeconds int
	err = tx.QueryRow(ctx, `SELECT id,tenant_id,workspace_id,app_id,direction,schedule_interval_seconds
		FROM connector_pipelines
		WHERE status='enabled' AND schedule_enabled
		  AND schedule_interval_seconds > 0 AND next_run_at <= now()
		  AND NOT EXISTS (SELECT 1 FROM connector_runs r WHERE r.pipeline_id=connector_pipelines.id AND r.status='running')
		ORDER BY next_run_at,id
		FOR UPDATE SKIP LOCKED LIMIT 1`).Scan(
		&pipelineID, &tenantID, &workspaceID, &appID, &direction, &intervalSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}

	jobType := map[string]string{
		"source": "warehouse.sync",
		"sink":   "reverse_etl.run",
		"export": "export.replay",
	}[direction]
	if jobType == "" {
		return false, fmt.Errorf("unsupported connector pipeline direction %q", direction)
	}
	payload, err := json.Marshal(map[string]string{
		"pipeline_id":  pipelineID,
		"tenant_id":    tenantID,
		"workspace_id": workspaceID,
		"app_id":       appID,
	})
	if err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `UPDATE connector_pipelines
		SET next_run_at=now()+($2 * interval '1 second'), updated_at=now()
		WHERE id=$1`, pipelineID, intervalSeconds); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO operation_jobs(tenant_id,workspace_id,job_type,payload)
		VALUES($1,$2,$3,$4)`, tenantID, workspaceID, jobType, payload); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}
