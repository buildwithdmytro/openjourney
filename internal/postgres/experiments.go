package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	journeygraph "github.com/buildwithdmytro/openjourney/internal/journey"
	"github.com/buildwithdmytro/openjourney/internal/telemetry"
	"github.com/jackc/pgx/v5"
)

const experimentColumns = `id, tenant_id, workspace_id, name, description, subject_type, status, method, seed, holdout_pct, primary_goal, guardrail_goals, winner_variant, created_at, updated_at`

var ErrExperimentWinnerRequired = errors.New("experiment has no recommended winner")
var ErrOptimizationUnavailable = errors.New("experiment has no eligible optimization")

const optimizationProposalColumns = `id, tenant_id, workspace_id, experiment_id, kind, report_snapshot,
	proposed_weights, winner_variant, rationale, status, approved_by, approved_at, created_at`

func scanOptimizationProposal(row pgx.Row) (domain.OptimizationProposal, error) {
	var out domain.OptimizationProposal
	err := row.Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.ExperimentID, &out.Kind,
		&out.ReportSnapshot, &out.ProposedWeights, &out.WinnerVariant, &out.Rationale,
		&out.Status, &out.ApprovedBy, &out.ApprovedAt, &out.CreatedAt)
	return out, err
}

// ProposeExperimentOptimization snapshots the current report and records only
// an advisory winner. It deliberately does not alter variants or assignments.
func (s *Store) ProposeExperimentOptimization(ctx context.Context, p domain.Principal, experimentID string) (domain.OptimizationProposal, error) {
	report, err := s.ExperimentReport(ctx, p, experimentID)
	if err != nil {
		return domain.OptimizationProposal{}, err
	}
	if report.WinnerVariant == nil || *report.WinnerVariant == "" {
		return domain.OptimizationProposal{}, ErrOptimizationUnavailable
	}
	snapshot, err := json.Marshal(report)
	if err != nil {
		return domain.OptimizationProposal{}, err
	}
	// Repeated controller runs are idempotent while a proposal is awaiting review.
	var existing domain.OptimizationProposal
	existing, err = scanOptimizationProposal(s.pool.QueryRow(ctx, `SELECT `+optimizationProposalColumns+`
		FROM optimization_proposals
		WHERE tenant_id=$1 AND workspace_id=$2 AND experiment_id=$3 AND status='proposed'
			AND winner_variant=$4
		ORDER BY created_at DESC LIMIT 1`, p.TenantID, p.WorkspaceID, experimentID, *report.WinnerVariant))
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.OptimizationProposal{}, err
	}
	rationale := fmt.Sprintf("Variant %q is a statistically significant positive winner with no guardrail regression.", *report.WinnerVariant)
	return scanOptimizationProposal(s.pool.QueryRow(ctx, `INSERT INTO optimization_proposals
		(tenant_id, workspace_id, experiment_id, kind, report_snapshot, winner_variant, rationale)
		VALUES ($1,$2,$3,'winner',$4,$5,$6)
		RETURNING `+optimizationProposalColumns,
		p.TenantID, p.WorkspaceID, experimentID, snapshot, *report.WinnerVariant, rationale))
}

// ApproveExperimentOptimization converts an advisory proposal into a new,
// immutable assignment snapshot. It never changes the source experiment.
func (s *Store) ApproveExperimentOptimization(ctx context.Context, p domain.Principal, experimentID, proposalID string) (domain.ExperimentVersion, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.ExperimentVersion{}, err
	}
	defer tx.Rollback(ctx)
	var seed string
	var holdout, latest int
	if err := tx.QueryRow(ctx, `SELECT seed, holdout_pct, COALESCE((SELECT MAX(version) FROM experiment_versions WHERE experiment_id=e.id), 0)
		FROM experiments e WHERE e.tenant_id=$1 AND e.workspace_id=$2 AND e.id=$3 FOR UPDATE`, p.TenantID, p.WorkspaceID, experimentID).Scan(&seed, &holdout, &latest); errors.Is(err, pgx.ErrNoRows) {
		return domain.ExperimentVersion{}, ErrNotFound
	} else if err != nil {
		return domain.ExperimentVersion{}, err
	}
	var proposal domain.OptimizationProposal
	if err := tx.QueryRow(ctx, `SELECT `+optimizationProposalColumns+` FROM optimization_proposals
		WHERE tenant_id=$1 AND workspace_id=$2 AND experiment_id=$3 AND id=$4 FOR UPDATE`, p.TenantID, p.WorkspaceID, experimentID, proposalID).Scan(&proposal.ID, &proposal.TenantID, &proposal.WorkspaceID, &proposal.ExperimentID, &proposal.Kind, &proposal.ReportSnapshot, &proposal.ProposedWeights, &proposal.WinnerVariant, &proposal.Rationale, &proposal.Status, &proposal.ApprovedBy, &proposal.ApprovedAt, &proposal.CreatedAt); errors.Is(err, pgx.ErrNoRows) {
		return domain.ExperimentVersion{}, ErrNotFound
	} else if err != nil {
		return domain.ExperimentVersion{}, err
	}
	if proposal.Status != "proposed" {
		return scanExperimentVersion(tx.QueryRow(ctx, `SELECT id, experiment_id, tenant_id, workspace_id, version, seed, holdout_pct, variants, approved_by, created_at FROM experiment_versions WHERE experiment_id=$1 AND proposal_id=$2`, experimentID, proposalID))
	}
	variants, err := s.listExperimentVariants(ctx, p, experimentID)
	if err != nil {
		return domain.ExperimentVersion{}, err
	}
	weights := map[string]int{}
	if len(proposal.ProposedWeights) > 0 {
		if err := json.Unmarshal(proposal.ProposedWeights, &weights); err != nil {
			return domain.ExperimentVersion{}, err
		}
	}
	for i := range variants {
		if proposal.Kind == "winner" {
			variants[i].Weight = 0
			if proposal.WinnerVariant != nil && variants[i].Label == *proposal.WinnerVariant {
				variants[i].Weight = 100
			}
		} else if weight, ok := weights[variants[i].Label]; ok {
			variants[i].Weight = weight
		}
	}
	snapshot, err := json.Marshal(variants)
	if err != nil {
		return domain.ExperimentVersion{}, err
	}
	var out domain.ExperimentVersion
	err = tx.QueryRow(ctx, `INSERT INTO experiment_versions (experiment_id,proposal_id,tenant_id,workspace_id,version,seed,holdout_pct,variants,approved_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id,experiment_id,tenant_id,workspace_id,version,seed,holdout_pct,variants,approved_by,created_at`,
		experimentID, proposalID, p.TenantID, p.WorkspaceID, latest+1, seed, holdout, snapshot, p.UserID).Scan(&out.ID, &out.ExperimentID, &out.TenantID, &out.WorkspaceID, &out.Version, &out.Seed, &out.HoldoutPct, &snapshot, &out.ApprovedBy, &out.CreatedAt)
	if err != nil {
		return domain.ExperimentVersion{}, err
	}
	if err := json.Unmarshal(snapshot, &out.Variants); err != nil {
		return domain.ExperimentVersion{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE optimization_proposals SET status='approved', approved_by=$1, approved_at=now() WHERE id=$2 AND tenant_id=$3 AND workspace_id=$4`, p.UserID, proposalID, p.TenantID, p.WorkspaceID); err != nil {
		return domain.ExperimentVersion{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ExperimentVersion{}, err
	}
	return out, nil
}

func scanExperimentVersion(row pgx.Row) (domain.ExperimentVersion, error) {
	var out domain.ExperimentVersion
	var variants []byte
	err := row.Scan(&out.ID, &out.ExperimentID, &out.TenantID, &out.WorkspaceID, &out.Version, &out.Seed, &out.HoldoutPct, &variants, &out.ApprovedBy, &out.CreatedAt)
	if err == nil {
		err = json.Unmarshal(variants, &out.Variants)
	}
	return out, err
}

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

func (s *Store) RolloutExperiment(ctx context.Context, p domain.Principal, experimentID string) (domain.ExperimentRollout, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return domain.ExperimentRollout{}, err
	}
	defer tx.Rollback(ctx)

	var e domain.Experiment
	err = tx.QueryRow(ctx, `SELECT `+experimentColumns+` FROM experiments WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3 FOR UPDATE`,
		p.TenantID, p.WorkspaceID, experimentID).
		Scan(&e.ID, &e.TenantID, &e.WorkspaceID, &e.Name, &e.Description, &e.SubjectType, &e.Status, &e.Method, &e.Seed, &e.HoldoutPct, &e.PrimaryGoal, &e.GuardrailGoals, &e.WinnerVariant, &e.CreatedAt, &e.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ExperimentRollout{}, ErrNotFound
	}
	if err != nil {
		return domain.ExperimentRollout{}, err
	}
	if e.WinnerVariant == nil || *e.WinnerVariant == "" {
		return domain.ExperimentRollout{}, ErrExperimentWinnerRequired
	}
	winnerVariant := *e.WinnerVariant
	var winnerTemplateID *string
	err = tx.QueryRow(ctx, `SELECT v.template_id
		FROM experiment_variants v JOIN experiments e ON e.id=v.experiment_id
		WHERE e.tenant_id=$1 AND e.workspace_id=$2 AND e.id=$3 AND v.label=$4`,
		p.TenantID, p.WorkspaceID, experimentID, winnerVariant).Scan(&winnerTemplateID)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ExperimentRollout{}, ErrExperimentWinnerRequired
	}
	if err != nil {
		return domain.ExperimentRollout{}, err
	}

	result := domain.ExperimentRollout{
		ExperimentID: experimentID, WinnerVariant: winnerVariant, SubjectType: e.SubjectType,
	}

	switch e.SubjectType {
	case "campaign":
		campaign, err := rolloutCampaign(ctx, tx, p, experimentID, winnerVariant, winnerTemplateID)
		if err != nil {
			return domain.ExperimentRollout{}, err
		}
		result.Campaign = &campaign
	case "journey":
		version, err := rolloutJourney(ctx, tx, p, experimentID, winnerVariant, winnerTemplateID)
		if err != nil {
			return domain.ExperimentRollout{}, err
		}
		result.JourneyVersion = &version
	default:
		return domain.ExperimentRollout{}, fmt.Errorf("unsupported experiment subject type %q", e.SubjectType)
	}

	command, err := tx.Exec(ctx, `UPDATE experiments SET status='completed', updated_at=now()
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`, p.TenantID, p.WorkspaceID, experimentID)
	if err != nil {
		return domain.ExperimentRollout{}, err
	}
	if command.RowsAffected() == 0 {
		return domain.ExperimentRollout{}, ErrNotFound
	}

	if err := tx.Commit(ctx); err != nil {
		return domain.ExperimentRollout{}, err
	}
	_ = s.audit(ctx, p, "experiment.rollout", "experiment", experimentID, map[string]any{
		"winner_variant": winnerVariant, "subject_type": e.SubjectType,
	})
	return result, nil
}

func rolloutCampaign(ctx context.Context, tx pgx.Tx, p domain.Principal, experimentID, winnerVariant string, winnerTemplateID *string) (domain.Campaign, error) {
	var source domain.Campaign
	err := tx.QueryRow(ctx, `SELECT id, name, description, segment_id, template_id, conversion_goal, attribution_window::text
		FROM campaigns WHERE tenant_id=$1 AND workspace_id=$2 AND experiment_id=$3
		ORDER BY created_at DESC, id DESC LIMIT 1 FOR UPDATE`, p.TenantID, p.WorkspaceID, experimentID).
		Scan(&source.ID, &source.Name, &source.Description, &source.SegmentID, &source.TemplateID, &source.ConversionGoal, &source.AttributionWindow)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Campaign{}, ErrNotFound
	}
	if err != nil {
		return domain.Campaign{}, err
	}
	templateID := source.TemplateID
	if winnerTemplateID != nil && *winnerTemplateID != "" {
		templateID = *winnerTemplateID
	}

	var out domain.Campaign
	err = tx.QueryRow(ctx, `INSERT INTO campaigns
		(tenant_id, workspace_id, name, description, segment_id, template_id, experiment_id,
		 conversion_goal, attribution_window, status, scheduled_at)
		VALUES ($1,$2,$3,$4,$5,$6,NULL,$7,NULLIF($8,'')::interval,'scheduled',now())
		RETURNING id, tenant_id, workspace_id, name, description, segment_id, template_id,
		 experiment_id, conversion_goal, attribution_window::text, status, scheduled_at, manifest_key,
		 segment_version, template_version, evaluated_at, recipient_count, created_at, updated_at`,
		p.TenantID, p.WorkspaceID, fmt.Sprintf("%s (%s rollout)", source.Name, winnerVariant), source.Description,
		source.SegmentID, templateID, nullableJSON(source.ConversionGoal), source.AttributionWindow).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Description, &out.SegmentID, &out.TemplateID,
			&out.ExperimentID, &out.ConversionGoal, &out.AttributionWindow, &out.Status, &out.ScheduledAt, &out.ManifestKey,
			&out.SegmentVersion, &out.TemplateVersion, &out.EvaluatedAt, &out.RecipientCount, &out.CreatedAt, &out.UpdatedAt)
	return out, err
}

func rolloutJourney(ctx context.Context, tx pgx.Tx, p domain.Principal, experimentID, winnerVariant string, winnerTemplateID *string) (domain.JourneyVersion, error) {
	var source domain.JourneyVersion
	var latestVersion int
	err := tx.QueryRow(ctx, `SELECT v.id, v.journey_id, v.tenant_id, v.workspace_id, v.version, v.graph,
		v.entry_kind, v.entry_event_type, v.entry_segment_id, v.entry_schedule, v.reentry_policy,
		v.max_reentries, v.late_policy, v.conversion_goal, v.attribution_window::text, j.latest_version
		FROM journeys j JOIN journey_versions v ON v.id=j.current_version_id
		WHERE j.tenant_id=$1 AND j.workspace_id=$2
		AND jsonb_path_exists(v.graph, '$.nodes[*].config ? (@.experiment_id == $experiment)',
			jsonb_build_object('experiment', to_jsonb($3::text)))
		ORDER BY j.updated_at DESC, j.id DESC LIMIT 1 FOR UPDATE OF j`, p.TenantID, p.WorkspaceID, experimentID).
		Scan(&source.ID, &source.JourneyID, &source.TenantID, &source.WorkspaceID, &source.Version, &source.Graph,
			&source.EntryKind, &source.EntryEventType, &source.EntrySegmentID, &source.EntrySchedule, &source.ReentryPolicy,
			&source.MaxReentries, &source.LatePolicy, &source.ConversionGoal, &source.AttributionWindow, &latestVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.JourneyVersion{}, ErrNotFound
	}
	if err != nil {
		return domain.JourneyVersion{}, err
	}

	pinnedGraph, err := pinJourneyExperiment(source.Graph, experimentID, winnerVariant, winnerTemplateID)
	if err != nil {
		return domain.JourneyVersion{}, err
	}
	version := latestVersion + 1
	var out domain.JourneyVersion
	err = tx.QueryRow(ctx, `INSERT INTO journey_versions
		(journey_id,tenant_id,workspace_id,version,graph,manifest_key,entry_kind,entry_event_type,
		 entry_segment_id,entry_schedule,reentry_policy,max_reentries,late_policy,conversion_goal,
		 attribution_window,status,published_by)
		VALUES ($1,$2,$3,$4,$5,NULL,$6,$7,$8,$9,$10,$11,$12,$13,NULLIF($14,'')::interval,'active',NULLIF($15,'')::uuid)
		RETURNING id, journey_id, tenant_id, workspace_id, version, graph, manifest_key, entry_kind,
		 entry_event_type, entry_segment_id, entry_schedule, reentry_policy, max_reentries, late_policy,
		 conversion_goal, attribution_window::text, status, published_by, published_at`,
		source.JourneyID, p.TenantID, p.WorkspaceID, version, pinnedGraph, source.EntryKind,
		source.EntryEventType, source.EntrySegmentID, source.EntrySchedule, source.ReentryPolicy,
		source.MaxReentries, source.LatePolicy, nullableJSON(source.ConversionGoal), source.AttributionWindow, p.UserID).
		Scan(&out.ID, &out.JourneyID, &out.TenantID, &out.WorkspaceID, &out.Version, &out.Graph, &out.ManifestKey,
			&out.EntryKind, &out.EntryEventType, &out.EntrySegmentID, &out.EntrySchedule, &out.ReentryPolicy,
			&out.MaxReentries, &out.LatePolicy, &out.ConversionGoal, &out.AttributionWindow, &out.Status, &out.PublishedBy, &out.PublishedAt)
	if err != nil {
		return domain.JourneyVersion{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE journey_versions SET status='archived'
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`, p.TenantID, p.WorkspaceID, source.ID); err != nil {
		return domain.JourneyVersion{}, err
	}
	command, err := tx.Exec(ctx, `UPDATE journeys SET current_version_id=$1, latest_version=$2, status='published', updated_at=now()
		WHERE tenant_id=$3 AND workspace_id=$4 AND id=$5`, out.ID, out.Version, p.TenantID, p.WorkspaceID, source.JourneyID)
	if err != nil {
		return domain.JourneyVersion{}, err
	}
	if command.RowsAffected() == 0 {
		return domain.JourneyVersion{}, ErrNotFound
	}
	return out, nil
}

func pinJourneyExperiment(data json.RawMessage, experimentID, winnerVariant string, winnerTemplateID *string) (json.RawMessage, error) {
	graph, err := journeygraph.ParseGraph(data)
	if err != nil {
		return nil, err
	}
	pinned := false
	for i := range graph.Nodes {
		node := &graph.Nodes[i]
		switch node.Type {
		case journeygraph.NodeTypeSplit:
			cfgAny, err := journeygraph.DecodeConfig(*node)
			if err != nil {
				return nil, err
			}
			cfg := cfgAny.(journeygraph.SplitConfig)
			if cfg.ExperimentID != experimentID {
				continue
			}
			found := false
			for j := range cfg.Branches {
				cfg.Branches[j].Weight = 0
				if cfg.Branches[j].Label == winnerVariant {
					cfg.Branches[j].Weight = 100
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("winner variant %q is not a split branch", winnerVariant)
			}
			cfg.ExperimentID = ""
			cfg.Mode = "random"
			node.Config, err = json.Marshal(cfg)
			if err != nil {
				return nil, err
			}
			pinned = true
		case journeygraph.NodeTypeMessage:
			cfgAny, err := journeygraph.DecodeConfig(*node)
			if err != nil {
				return nil, err
			}
			cfg := cfgAny.(journeygraph.MessageConfig)
			if cfg.ExperimentID != experimentID {
				continue
			}
			if winnerTemplateID != nil && *winnerTemplateID != "" {
				cfg.TemplateID = *winnerTemplateID
			}
			cfg.ExperimentID = ""
			node.Config, err = json.Marshal(cfg)
			if err != nil {
				return nil, err
			}
			pinned = true
		}
	}
	if !pinned {
		return nil, ErrNotFound
	}
	if err := journeygraph.Validate(graph); err != nil {
		return nil, err
	}
	return json.Marshal(graph)
}
