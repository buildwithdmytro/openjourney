package postgres

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/telemetry"
	"github.com/jackc/pgx/v5"
)

const experimentColumns = `id, tenant_id, workspace_id, name, description, subject_type, status, method, seed, holdout_pct, primary_goal, guardrail_goals, winner_variant, created_at, updated_at`

func normalizeExperiment(e domain.Experiment) (domain.Experiment, error) {
	if e.Name == "" || e.SubjectType == "" || e.Seed == "" {
		return domain.Experiment{}, errors.New("experiment name, subject_type, and seed are required")
	}
	if e.Status == "" {
		e.Status = "draft"
	}
	if e.Method == "" {
		e.Method = "frequentist"
	}
	if len(e.GuardrailGoals) == 0 {
		e.GuardrailGoals = json.RawMessage("[]")
	}
	return e, nil
}

func scanExperiment(row pgx.Row) (domain.Experiment, error) {
	var e domain.Experiment
	err := row.Scan(&e.ID, &e.TenantID, &e.WorkspaceID, &e.Name, &e.Description, &e.SubjectType, &e.Status, &e.Method, &e.Seed, &e.HoldoutPct, &e.PrimaryGoal, &e.GuardrailGoals, &e.WinnerVariant, &e.CreatedAt, &e.UpdatedAt)
	return e, err
}

func (s *Store) CreateExperiment(ctx context.Context, p domain.Principal, input domain.Experiment) (domain.Experiment, error) {
	e, err := normalizeExperiment(input)
	if err != nil {
		return domain.Experiment{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Experiment{}, err
	}
	defer tx.Rollback(ctx)

	out, err := scanExperiment(tx.QueryRow(ctx, `INSERT INTO experiments
		(tenant_id, workspace_id, name, description, subject_type, status, method, seed, holdout_pct, primary_goal, guardrail_goals, winner_variant)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) RETURNING `+experimentColumns,
		p.TenantID, p.WorkspaceID, e.Name, e.Description, e.SubjectType, e.Status, e.Method, e.Seed, e.HoldoutPct, nullableJSON(e.PrimaryGoal), e.GuardrailGoals, e.WinnerVariant))
	if err != nil {
		return domain.Experiment{}, err
	}
	if err := insertExperimentVariants(ctx, tx, p.TenantID, out.ID, e.Variants); err != nil {
		return domain.Experiment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Experiment{}, err
	}
	out.Variants = e.Variants
	for i := range out.Variants {
		out.Variants[i].ExperimentID = out.ID
		out.Variants[i].TenantID = p.TenantID
	}
	return out, nil
}

func nullableJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return nil
	}
	return value
}

func insertExperimentVariants(ctx context.Context, tx pgx.Tx, tenantID, experimentID string, variants []domain.ExperimentVariant) error {
	for i := range variants {
		v := &variants[i]
		if v.Label == "" {
			return errors.New("variant label is required")
		}
		err := tx.QueryRow(ctx, `INSERT INTO experiment_variants (experiment_id, tenant_id, label, weight, is_control, template_id)
			VALUES ($1,$2,$3,$4,$5,$6) RETURNING id, created_at`, experimentID, tenantID, v.Label, v.Weight, v.IsControl, v.TemplateID).Scan(&v.ID, &v.CreatedAt)
		if err != nil {
			return err
		}
		v.ExperimentID, v.TenantID = experimentID, tenantID
	}
	return nil
}

func (s *Store) GetExperiment(ctx context.Context, p domain.Principal, id string) (domain.Experiment, error) {
	e, err := scanExperiment(s.pool.QueryRow(ctx, `SELECT `+experimentColumns+` FROM experiments WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`, p.TenantID, p.WorkspaceID, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Experiment{}, ErrNotFound
	}
	if err != nil {
		return domain.Experiment{}, err
	}
	e.Variants, err = s.listExperimentVariants(ctx, p, id)
	return e, err
}

func (s *Store) listExperimentVariants(ctx context.Context, p domain.Principal, experimentID string) ([]domain.ExperimentVariant, error) {
	rows, err := s.pool.Query(ctx, `SELECT v.id, v.experiment_id, v.tenant_id, v.label, v.weight, v.is_control, v.template_id, v.created_at
		FROM experiment_variants v JOIN experiments e ON e.id=v.experiment_id
		WHERE e.tenant_id=$1 AND e.workspace_id=$2 AND e.id=$3 ORDER BY v.created_at, v.label`, p.TenantID, p.WorkspaceID, experimentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ExperimentVariant
	for rows.Next() {
		var v domain.ExperimentVariant
		if err := rows.Scan(&v.ID, &v.ExperimentID, &v.TenantID, &v.Label, &v.Weight, &v.IsControl, &v.TemplateID, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s *Store) UpdateExperiment(ctx context.Context, p domain.Principal, input domain.Experiment) (domain.Experiment, error) {
	e, err := normalizeExperiment(input)
	if err != nil {
		return domain.Experiment{}, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.Experiment{}, err
	}
	defer tx.Rollback(ctx)
	out, err := scanExperiment(tx.QueryRow(ctx, `UPDATE experiments SET name=$4, description=$5, subject_type=$6, status=$7, method=$8, seed=$9,
		holdout_pct=$10, primary_goal=$11, guardrail_goals=$12, winner_variant=$13, updated_at=now()
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3 AND (status <> 'running' OR seed=$9) RETURNING `+experimentColumns,
		p.TenantID, p.WorkspaceID, e.ID, e.Name, e.Description, e.SubjectType, e.Status, e.Method, e.Seed, e.HoldoutPct, nullableJSON(e.PrimaryGoal), e.GuardrailGoals, e.WinnerVariant))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Experiment{}, ErrNotFound
	}
	if err != nil {
		return domain.Experiment{}, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM experiment_variants WHERE experiment_id=$1 AND tenant_id=$2`, e.ID, p.TenantID); err != nil {
		return domain.Experiment{}, err
	}
	if err := insertExperimentVariants(ctx, tx, p.TenantID, e.ID, e.Variants); err != nil {
		return domain.Experiment{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Experiment{}, err
	}
	out.Variants = e.Variants
	return out, nil
}

func (s *Store) ListExperiments(ctx context.Context, p domain.Principal) ([]domain.Experiment, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+experimentColumns+` FROM experiments WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY created_at DESC`, p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Experiment
	for rows.Next() {
		e, err := scanExperiment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) AssignExperiment(ctx context.Context, p domain.Principal, experimentID, profileID, variant string) (domain.ExperimentAssignment, error) {
	var out domain.ExperimentAssignment
	var created bool
	err := s.pool.QueryRow(ctx, `WITH authorized AS (
		SELECT id FROM experiments WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3
	), inserted AS (
		INSERT INTO experiment_assignments (experiment_id, tenant_id, workspace_id, profile_id, variant)
		SELECT id, $1, $2, $4, $5 FROM authorized ON CONFLICT (experiment_id, profile_id) DO NOTHING
		RETURNING experiment_id, tenant_id, workspace_id, profile_id, variant, assigned_at
	)
	SELECT experiment_id, tenant_id, workspace_id, profile_id, variant, assigned_at, true FROM inserted
	UNION ALL
	SELECT a.experiment_id, a.tenant_id, a.workspace_id, a.profile_id, a.variant, a.assigned_at, false
	FROM experiment_assignments a JOIN authorized e ON e.id=a.experiment_id WHERE a.profile_id=$4
	LIMIT 1`,
		p.TenantID, p.WorkspaceID, experimentID, profileID, variant).
		Scan(&out.ExperimentID, &out.TenantID, &out.WorkspaceID, &out.ProfileID, &out.Variant, &out.AssignedAt, &created)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ExperimentAssignment{}, ErrNotFound
	}
	if err == nil && created {
		telemetry.RecordExperimentAssignment(ctx, out.Variant)
	}
	return out, err
}
