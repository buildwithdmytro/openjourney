package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

const savedReportColumns = `id, tenant_id, workspace_id, name, report_type, query, created_by_user_id, created_at, updated_at`

func scanSavedReport(row pgx.Row) (domain.SavedReport, error) {
	var out domain.SavedReport
	var queryJSON []byte

	err := row.Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.ReportType, &queryJSON, &out.CreatedByUserID, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return domain.SavedReport{}, err
	}

	if len(queryJSON) > 0 {
		if err := json.Unmarshal(queryJSON, &out.Query); err != nil {
			return domain.SavedReport{}, err
		}
	}

	return out, nil
}

func (s *Store) CreateSavedReport(ctx context.Context, p domain.Principal, input domain.SavedReport) (domain.SavedReport, error) {
	if input.Name == "" {
		return domain.SavedReport{}, errors.New("saved report name is required")
	}
	if input.ReportType == "" {
		return domain.SavedReport{}, errors.New("saved report type is required")
	}

	// Validate report_type
	switch input.ReportType {
	case "funnel", "deliverability", "retention", "cohort", "growth", "cost", "experiment":
	default:
		return domain.SavedReport{}, errors.New("report_type must be one of: funnel, deliverability, retention, cohort, growth, cost, experiment")
	}

	queryJSON, err := json.Marshal(input.Query)
	if err != nil {
		return domain.SavedReport{}, err
	}

	out, err := scanSavedReport(s.pool.QueryRow(ctx, `INSERT INTO saved_reports
		(tenant_id, workspace_id, name, report_type, query, created_by_user_id)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING `+savedReportColumns,
		p.TenantID, p.WorkspaceID, input.Name, input.ReportType, queryJSON, p.UserID))
	if err != nil {
		if strings.Contains(err.Error(), "unique constraint") {
			return domain.SavedReport{}, errors.New("saved report name already exists in this workspace")
		}
		return domain.SavedReport{}, err
	}

	_ = s.audit(ctx, p, "saved_report.create", "saved_report", out.ID, map[string]any{"name": out.Name, "report_type": out.ReportType})
	return out, nil
}

func (s *Store) GetSavedReport(ctx context.Context, p domain.Principal, id string) (domain.SavedReport, error) {
	r, err := scanSavedReport(s.pool.QueryRow(ctx, `SELECT `+savedReportColumns+` FROM saved_reports
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`,
		p.TenantID, p.WorkspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SavedReport{}, ErrNotFound
	}
	return r, err
}

func (s *Store) ListSavedReports(ctx context.Context, p domain.Principal) ([]domain.SavedReport, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+savedReportColumns+` FROM saved_reports
		WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY created_at DESC`,
		p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.SavedReport
	for rows.Next() {
		r, err := scanSavedReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) DeleteSavedReport(ctx context.Context, p domain.Principal, id string) error {
	result, err := s.pool.Exec(ctx, `DELETE FROM saved_reports
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`,
		p.TenantID, p.WorkspaceID, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	_ = s.audit(ctx, p, "saved_report.delete", "saved_report", id, map[string]any{})
	return nil
}
