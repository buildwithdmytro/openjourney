package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

const evalDatasetColumns = `id, tenant_id, workspace_id, task_type, name, created_at`

func validateEvalDataset(dataset domain.EvalDataset) error {
	if dataset.TaskType == "" {
		return errors.New("task_type is required")
	}
	if dataset.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func (s *Store) CreateEvalDataset(ctx context.Context, p domain.Principal, dataset domain.EvalDataset) (domain.EvalDataset, error) {
	if err := validateEvalDataset(dataset); err != nil {
		return domain.EvalDataset{}, err
	}
	var out domain.EvalDataset
	err := s.pool.QueryRow(ctx, `INSERT INTO eval_datasets (tenant_id, workspace_id, task_type, name)
		VALUES ($1, $2, $3, $4) RETURNING `+evalDatasetColumns,
		p.TenantID, p.WorkspaceID, dataset.TaskType, dataset.Name).Scan(
		&out.ID, &out.TenantID, &out.WorkspaceID, &out.TaskType, &out.Name, &out.CreatedAt)
	return out, err
}

func (s *Store) GetEvalDataset(ctx context.Context, p domain.Principal, id string) (domain.EvalDataset, error) {
	var out domain.EvalDataset
	err := s.pool.QueryRow(ctx, `SELECT `+evalDatasetColumns+` FROM eval_datasets
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`, p.TenantID, p.WorkspaceID, id).Scan(
		&out.ID, &out.TenantID, &out.WorkspaceID, &out.TaskType, &out.Name, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EvalDataset{}, ErrNotFound
	}
	return out, err
}

func (s *Store) ListEvalDatasets(ctx context.Context, p domain.Principal) ([]domain.EvalDataset, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+evalDatasetColumns+` FROM eval_datasets
		WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY name`, p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.EvalDataset
	for rows.Next() {
		var dataset domain.EvalDataset
		if err := rows.Scan(&dataset.ID, &dataset.TenantID, &dataset.WorkspaceID, &dataset.TaskType, &dataset.Name, &dataset.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, dataset)
	}
	return out, rows.Err()
}

func (s *Store) UpdateEvalDataset(ctx context.Context, p domain.Principal, dataset domain.EvalDataset) (domain.EvalDataset, error) {
	if err := validateEvalDataset(dataset); err != nil {
		return domain.EvalDataset{}, err
	}
	var out domain.EvalDataset
	err := s.pool.QueryRow(ctx, `UPDATE eval_datasets SET task_type=$1, name=$2
		WHERE tenant_id=$3 AND workspace_id=$4 AND id=$5 RETURNING `+evalDatasetColumns,
		dataset.TaskType, dataset.Name, p.TenantID, p.WorkspaceID, dataset.ID).Scan(
		&out.ID, &out.TenantID, &out.WorkspaceID, &out.TaskType, &out.Name, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EvalDataset{}, ErrNotFound
	}
	return out, err
}

func (s *Store) DeleteEvalDataset(ctx context.Context, p domain.Principal, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM eval_datasets WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`, p.TenantID, p.WorkspaceID, id)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return err
}

const evalCaseColumns = `id, dataset_id, tenant_id, input, expectations`

func validateEvalCase(evalCase domain.EvalCase) error {
	if evalCase.DatasetID == "" {
		return errors.New("dataset_id is required")
	}
	if !json.Valid(evalCase.Input) || !json.Valid(evalCase.Expectations) {
		return errors.New("input and expectations must be valid JSON")
	}
	return nil
}

func (s *Store) CreateEvalCase(ctx context.Context, p domain.Principal, evalCase domain.EvalCase) (domain.EvalCase, error) {
	if err := validateEvalCase(evalCase); err != nil {
		return domain.EvalCase{}, err
	}
	var out domain.EvalCase
	err := s.pool.QueryRow(ctx, `INSERT INTO eval_cases (dataset_id, tenant_id, input, expectations)
		SELECT $1, $2, $3, $4 WHERE EXISTS (SELECT 1 FROM eval_datasets WHERE id=$1 AND tenant_id=$2 AND workspace_id=$5)
		RETURNING `+evalCaseColumns, evalCase.DatasetID, p.TenantID, evalCase.Input, evalCase.Expectations, p.WorkspaceID).Scan(
		&out.ID, &out.DatasetID, &out.TenantID, &out.Input, &out.Expectations)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EvalCase{}, ErrNotFound
	}
	return out, err
}

func (s *Store) GetEvalCase(ctx context.Context, p domain.Principal, id string) (domain.EvalCase, error) {
	var out domain.EvalCase
	err := s.pool.QueryRow(ctx, `SELECT c.id, c.dataset_id, c.tenant_id, c.input, c.expectations
		FROM eval_cases c JOIN eval_datasets d ON d.id=c.dataset_id
		WHERE c.tenant_id=$1 AND d.workspace_id=$2 AND c.id=$3`, p.TenantID, p.WorkspaceID, id).Scan(
		&out.ID, &out.DatasetID, &out.TenantID, &out.Input, &out.Expectations)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EvalCase{}, ErrNotFound
	}
	return out, err
}

func (s *Store) ListEvalCases(ctx context.Context, p domain.Principal, datasetID string) ([]domain.EvalCase, error) {
	rows, err := s.pool.Query(ctx, `SELECT c.id, c.dataset_id, c.tenant_id, c.input, c.expectations
		FROM eval_cases c JOIN eval_datasets d ON d.id=c.dataset_id
		WHERE c.tenant_id=$1 AND d.workspace_id=$2 AND c.dataset_id=$3 ORDER BY c.id`, p.TenantID, p.WorkspaceID, datasetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.EvalCase
	for rows.Next() {
		var evalCase domain.EvalCase
		if err := rows.Scan(&evalCase.ID, &evalCase.DatasetID, &evalCase.TenantID, &evalCase.Input, &evalCase.Expectations); err != nil {
			return nil, err
		}
		out = append(out, evalCase)
	}
	return out, rows.Err()
}

func (s *Store) UpdateEvalCase(ctx context.Context, p domain.Principal, evalCase domain.EvalCase) (domain.EvalCase, error) {
	if err := validateEvalCase(evalCase); err != nil {
		return domain.EvalCase{}, err
	}
	var out domain.EvalCase
	err := s.pool.QueryRow(ctx, `UPDATE eval_cases c SET dataset_id=$1, input=$2, expectations=$3
		FROM eval_datasets d WHERE c.id=$4 AND c.tenant_id=$5 AND d.id=$1 AND d.tenant_id=$5 AND d.workspace_id=$6
		RETURNING c.id, c.dataset_id, c.tenant_id, c.input, c.expectations`,
		evalCase.DatasetID, evalCase.Input, evalCase.Expectations, evalCase.ID, p.TenantID, p.WorkspaceID).Scan(
		&out.ID, &out.DatasetID, &out.TenantID, &out.Input, &out.Expectations)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EvalCase{}, ErrNotFound
	}
	return out, err
}

func (s *Store) DeleteEvalCase(ctx context.Context, p domain.Principal, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM eval_cases c USING eval_datasets d
		WHERE c.id=$1 AND c.tenant_id=$2 AND d.id=c.dataset_id AND d.workspace_id=$3`, id, p.TenantID, p.WorkspaceID)
	if err == nil && tag.RowsAffected() != 1 {
		return ErrNotFound
	}
	return err
}

const evalRunColumns = `id, prompt_version_id, tenant_id, dataset_id, passed, failed, verdict, created_at`

func validateEvalRun(run domain.EvalRun) error {
	if run.PromptVersionID == "" || run.DatasetID == "" {
		return errors.New("prompt_version_id and dataset_id are required")
	}
	if run.Passed < 0 || run.Failed < 0 {
		return errors.New("passed and failed must be non-negative")
	}
	if run.Verdict != "passed" && run.Verdict != "failed" {
		return fmt.Errorf("invalid verdict: %s", run.Verdict)
	}
	return nil
}

func (s *Store) CreateEvalRun(ctx context.Context, p domain.Principal, run domain.EvalRun) (domain.EvalRun, error) {
	if err := validateEvalRun(run); err != nil {
		return domain.EvalRun{}, err
	}
	var out domain.EvalRun
	err := s.pool.QueryRow(ctx, `INSERT INTO eval_runs (prompt_version_id, tenant_id, dataset_id, passed, failed, verdict)
		SELECT $1, $2, $3, $4, $5, $6 WHERE EXISTS (SELECT 1 FROM eval_datasets WHERE id=$3 AND tenant_id=$2 AND workspace_id=$7)
		RETURNING `+evalRunColumns, run.PromptVersionID, p.TenantID, run.DatasetID, run.Passed, run.Failed, run.Verdict, p.WorkspaceID).Scan(
		&out.ID, &out.PromptVersionID, &out.TenantID, &out.DatasetID, &out.Passed, &out.Failed, &out.Verdict, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EvalRun{}, ErrNotFound
	}
	return out, err
}

func (s *Store) GetEvalRun(ctx context.Context, p domain.Principal, id string) (domain.EvalRun, error) {
	var out domain.EvalRun
	err := s.pool.QueryRow(ctx, `SELECT r.id, r.prompt_version_id, r.tenant_id, r.dataset_id, r.passed, r.failed, r.verdict, r.created_at
		FROM eval_runs r JOIN eval_datasets d ON d.id=r.dataset_id
		WHERE r.tenant_id=$1 AND d.workspace_id=$2 AND r.id=$3`, p.TenantID, p.WorkspaceID, id).Scan(
		&out.ID, &out.PromptVersionID, &out.TenantID, &out.DatasetID, &out.Passed, &out.Failed, &out.Verdict, &out.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.EvalRun{}, ErrNotFound
	}
	return out, err
}

func (s *Store) ListEvalRuns(ctx context.Context, p domain.Principal, datasetID string) ([]domain.EvalRun, error) {
	rows, err := s.pool.Query(ctx, `SELECT r.id, r.prompt_version_id, r.tenant_id, r.dataset_id, r.passed, r.failed, r.verdict, r.created_at
		FROM eval_runs r JOIN eval_datasets d ON d.id=r.dataset_id
		WHERE r.tenant_id=$1 AND d.workspace_id=$2 AND r.dataset_id=$3 ORDER BY r.created_at DESC`, p.TenantID, p.WorkspaceID, datasetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.EvalRun
	for rows.Next() {
		var run domain.EvalRun
		if err := rows.Scan(&run.ID, &run.PromptVersionID, &run.TenantID, &run.DatasetID, &run.Passed, &run.Failed, &run.Verdict, &run.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, run)
	}
	return out, rows.Err()
}
