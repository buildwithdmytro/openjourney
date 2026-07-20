package postgres

import (
	"context"
	"errors"

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
