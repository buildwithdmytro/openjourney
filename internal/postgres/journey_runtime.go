package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/audience"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/telemetry"
	"github.com/jackc/pgx/v5"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

func (s *Store) CreateJourneyRun(ctx context.Context, run domain.JourneyRun) (bool, error) {
	if run.TenantID == "" {
		return false, errors.New("tenant_id is required")
	}
	if run.Status == "" {
		run.Status = "active"
	}
	if len(run.State) == 0 {
		run.State = json.RawMessage("{}")
	}
	if run.EnteredAt.IsZero() {
		run.EnteredAt = time.Now()
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = time.Now()
	}

	res, err := s.pool.Exec(ctx, `INSERT INTO journey_runs (
			tenant_id, workspace_id, journey_id, journey_version_id, profile_id,
			subject_external_id, entry_key, reentry_sequence, status,
			current_node_id, state, wait_event_type, wait_until, goal_reached,
			entered_at, updated_at, completed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (journey_version_id, profile_id, entry_key, reentry_sequence) DO NOTHING`,
		run.TenantID, run.WorkspaceID, run.JourneyID, run.JourneyVersionID, run.ProfileID,
		run.SubjectExternalID, run.EntryKey, run.ReentrySequence, run.Status,
		run.CurrentNodeID, run.State, run.WaitEventType, run.WaitUntil, run.GoalReached,
		run.EnteredAt, run.UpdatedAt, run.CompletedAt)
	if err != nil {
		return false, err
	}
	inserted := res.RowsAffected() > 0
	if inserted {
		telemetry.JourneyEnrollments.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("tenant_id", run.TenantID),
			attribute.String("journey_id", run.JourneyID),
		))
	}
	return inserted, nil
}

// EnrollJourneyRun atomically creates a run and its initial runnable step. The
// returned boolean is false when the enrollment's uniqueness key already exists.
func (s *Store) EnrollJourneyRun(ctx context.Context, run domain.JourneyRun, initialStep domain.JourneyStep) (string, bool, error) {
	if run.TenantID == "" {
		return "", false, errors.New("tenant_id is required")
	}
	if run.Status == "" {
		run.Status = "active"
	}
	if len(run.State) == 0 {
		run.State = json.RawMessage("{}")
	}
	if run.EnteredAt.IsZero() {
		run.EnteredAt = time.Now()
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = time.Now()
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var runID string
	err = tx.QueryRow(ctx, `INSERT INTO journey_runs (
			tenant_id, workspace_id, journey_id, journey_version_id, profile_id,
			subject_external_id, entry_key, reentry_sequence, status,
			current_node_id, state, wait_event_type, wait_until, goal_reached,
			entered_at, updated_at, completed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		ON CONFLICT (journey_version_id, profile_id, entry_key, reentry_sequence) DO NOTHING
		RETURNING id`,
		run.TenantID, run.WorkspaceID, run.JourneyID, run.JourneyVersionID, run.ProfileID,
		run.SubjectExternalID, run.EntryKey, run.ReentrySequence, run.Status,
		run.CurrentNodeID, run.State, run.WaitEventType, run.WaitUntil, run.GoalReached,
		run.EnteredAt, run.UpdatedAt, run.CompletedAt).Scan(&runID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}

	if initialStep.TenantID == "" {
		initialStep.TenantID = run.TenantID
	}
	if initialStep.Status == "" {
		initialStep.Status = "pending"
	}
	if initialStep.Kind == "" {
		initialStep.Kind = "advance"
	}
	if initialStep.AvailableAt.IsZero() {
		initialStep.AvailableAt = time.Now()
	}
	_, err = tx.Exec(ctx, `INSERT INTO journey_steps
		(run_id, tenant_id, node_id, kind, status, available_at, attempts)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`, runID, initialStep.TenantID,
		initialStep.NodeID, initialStep.Kind, initialStep.Status, initialStep.AvailableAt,
		initialStep.Attempts)
	if err != nil {
		return "", false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return "", false, err
	}
	telemetry.JourneyEnrollments.Add(ctx, 1, otelmetric.WithAttributes(
		attribute.String("tenant_id", run.TenantID), attribute.String("journey_id", run.JourneyID)))
	return runID, true, nil
}

func (s *Store) GetJourneyRun(ctx context.Context, p domain.Principal, runID string) (domain.JourneyRun, error) {
	var out domain.JourneyRun
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, journey_id, journey_version_id, profile_id,
			subject_external_id, entry_key, reentry_sequence, status, current_node_id,
			state, wait_event_type, wait_until, goal_reached, entered_at, updated_at, completed_at
		FROM journey_runs
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`,
		p.TenantID, p.WorkspaceID, runID).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.JourneyID, &out.JourneyVersionID, &out.ProfileID,
			&out.SubjectExternalID, &out.EntryKey, &out.ReentrySequence, &out.Status, &out.CurrentNodeID,
			&out.State, &out.WaitEventType, &out.WaitUntil, &out.GoalReached, &out.EnteredAt, &out.UpdatedAt, &out.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.JourneyRun{}, ErrNotFound
	}
	return out, err
}

func (s *Store) UpdateJourneyRun(ctx context.Context, p domain.Principal, run domain.JourneyRun) (domain.JourneyRun, error) {
	var out domain.JourneyRun
	err := s.pool.QueryRow(ctx, `UPDATE journey_runs SET
			status=$4,
			current_node_id=$5,
			state=$6,
			wait_event_type=$7,
			wait_until=$8,
			goal_reached=$9,
			updated_at=now(),
			completed_at=$10
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3
		RETURNING id, tenant_id, workspace_id, journey_id, journey_version_id, profile_id,
		          subject_external_id, entry_key, reentry_sequence, status, current_node_id,
		          state, wait_event_type, wait_until, goal_reached, entered_at, updated_at, completed_at`,
		p.TenantID, p.WorkspaceID, run.ID, run.Status, run.CurrentNodeID, run.State,
		run.WaitEventType, run.WaitUntil, run.GoalReached, run.CompletedAt).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.JourneyID, &out.JourneyVersionID, &out.ProfileID,
			&out.SubjectExternalID, &out.EntryKey, &out.ReentrySequence, &out.Status, &out.CurrentNodeID,
			&out.State, &out.WaitEventType, &out.WaitUntil, &out.GoalReached, &out.EnteredAt, &out.UpdatedAt, &out.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.JourneyRun{}, ErrNotFound
	}
	return out, err
}

func (s *Store) CancelJourneyRun(ctx context.Context, p domain.Principal, journeyID string, runID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// 1. Update run status to 'canceled'
	res, err := tx.Exec(ctx, `UPDATE journey_runs SET status = 'canceled', completed_at = now(), updated_at = now()
		WHERE tenant_id = $1 AND workspace_id = $2 AND journey_id = $3 AND id = $4 AND status NOT IN ('completed', 'canceled')`,
		p.TenantID, p.WorkspaceID, journeyID, runID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		var exists bool
		err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM journey_runs WHERE tenant_id = $1 AND workspace_id = $2 AND journey_id = $3 AND id = $4)`,
			p.TenantID, p.WorkspaceID, journeyID, runID).Scan(&exists)
		if err != nil {
			return err
		}
		if !exists {
			return ErrNotFound
		}
		return nil
	}

	// 2. Delete non-completed steps of this run
	_, err = tx.Exec(ctx, `DELETE FROM journey_steps WHERE tenant_id = $1 AND run_id = $2 AND status != 'completed'`,
		p.TenantID, runID)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Store) ClaimJourneyStep(ctx context.Context) (domain.JourneyStep, bool, error) {
	// 1. Mark timed-out processing steps that exceeded max attempts as dead
	_, err := s.pool.Exec(ctx, `
		UPDATE journey_steps
		SET status = 'dead', locked_until = NULL, error_message = 'poison pill: max processing attempts exceeded', updated_at = now()
		WHERE status = 'processing' AND locked_until <= now() AND attempts >= 10
	`)
	if err != nil {
		return domain.JourneyStep{}, false, err
	}

	var out domain.JourneyStep
	err = s.pool.QueryRow(ctx, `UPDATE journey_steps SET
			status='processing',
			attempts=attempts+1,
			locked_until=now() + INTERVAL '5 minutes',
			updated_at=now()
		WHERE id = (
			SELECT js.id FROM journey_steps js
			JOIN journey_runs jr ON jr.id = js.run_id
			JOIN journey_versions jv ON jv.id = jr.journey_version_id
			WHERE jv.status = 'active'
			  AND js.tenant_id = jr.tenant_id
			  AND js.node_id = jr.current_node_id
			  AND (
				(js.kind = 'advance' AND jr.status = 'active')
				OR (js.kind = 'timeout' AND jr.status = 'waiting')
			  )
			  AND (
				(js.status IN ('pending', 'failed') AND js.attempts < 10 AND js.available_at <= now())
				OR (js.status='processing' AND js.locked_until <= now() AND js.attempts < 10)
			)
			ORDER BY js.available_at ASC
			FOR UPDATE OF js SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, run_id, tenant_id, node_id, kind, status, attempts, available_at, locked_until, error_message, created_at, updated_at`).
		Scan(&out.ID, &out.RunID, &out.TenantID, &out.NodeID, &out.Kind, &out.Status, &out.Attempts, &out.AvailableAt, &out.LockedUntil, &out.ErrorMessage, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.JourneyStep{}, false, nil
	}
	if err != nil {
		return domain.JourneyStep{}, false, err
	}
	return out, true, nil
}

func (s *Store) CompleteJourneyStep(ctx context.Context, stepID string) error {
	_, err := s.pool.Exec(ctx, `UPDATE journey_steps SET status='completed', locked_until=NULL, updated_at=now() WHERE id=$1`, stepID)
	return err
}

func (s *Store) FailJourneyStep(ctx context.Context, stepID string, errMsg string) error {
	_, err := s.pool.Exec(ctx, `UPDATE journey_steps SET
			status=CASE WHEN attempts >= 10 THEN 'dead'::text ELSE 'failed'::text END,
			error_message=$2,
			available_at=now() + INTERVAL '1 minute',
			locked_until=NULL,
			updated_at=now()
		WHERE id=$1`, stepID, errMsg)
	return err
}

func (s *Store) RescheduleJourneyStep(ctx context.Context, stepID string, availableAt time.Time) error {
	_, err := s.pool.Exec(ctx, `UPDATE journey_steps SET
			status='pending',
			attempts=0,
			locked_until=NULL,
			available_at=$2,
			updated_at=now()
		WHERE id=$1`, stepID, availableAt)
	return err
}

func (s *Store) InsertJourneyStep(ctx context.Context, step domain.JourneyStep) error {
	if step.Kind == "" {
		step.Kind = "advance"
	}
	if step.Status == "" {
		step.Status = "pending"
	}
	if step.AvailableAt.IsZero() {
		step.AvailableAt = time.Now()
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO journey_steps (
			run_id, tenant_id, node_id, kind, status, attempts, available_at, locked_until, error_message
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		step.RunID, step.TenantID, step.NodeID, step.Kind, step.Status, step.Attempts, step.AvailableAt, step.LockedUntil, step.ErrorMessage)
	return err
}

func (s *Store) RecordTransition(ctx context.Context, trans domain.JourneyTransition) error {
	if len(trans.Detail) == 0 {
		trans.Detail = json.RawMessage("{}")
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO journey_transitions (
			run_id, tenant_id, from_node, to_node, node_type, outcome, detail
		) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		trans.RunID, trans.TenantID, trans.FromNode, trans.ToNode, trans.NodeType, trans.Outcome, trans.Detail)
	return err
}

func (s *Store) AdvanceRunTx(ctx context.Context, runID string, run domain.JourneyRun, stepID string, nextStep *domain.JourneyStep, trans domain.JourneyTransition, messageIntent *domain.JourneyMessageIntent) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// All transition paths lock run before step. resolveWaitingRuns uses the same
	// order, preventing a run<->step deadlock with event-driven wait resolution.
	var dbRunStatus, dbCurrentNodeID string
	err = tx.QueryRow(ctx, `SELECT status, current_node_id
		FROM journey_runs WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, runID, run.TenantID).
		Scan(&dbRunStatus, &dbCurrentNodeID)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("advance lock run: %w", err)
	}

	var claimedStep domain.JourneyStep
	err = tx.QueryRow(ctx, `SELECT id, run_id, tenant_id, node_id, kind, status
		FROM journey_steps WHERE id=$1 FOR UPDATE`, stepID).
		Scan(&claimedStep.ID, &claimedStep.RunID, &claimedStep.TenantID, &claimedStep.NodeID, &claimedStep.Kind, &claimedStep.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("advance lock step: %w", err)
	}
	if claimedStep.RunID != runID || claimedStep.RunID != run.ID || claimedStep.TenantID != run.TenantID {
		return errors.New("advance step does not match run")
	}
	if claimedStep.Status == "completed" {
		return tx.Commit(ctx)
	}
	if claimedStep.Status != "processing" {
		return fmt.Errorf("advance step %s is %s, expected processing", stepID, claimedStep.Status)
	}

	expectedStatus := "active"
	if claimedStep.Kind == "timeout" {
		expectedStatus = "waiting"
	}
	if dbRunStatus != expectedStatus || dbCurrentNodeID != claimedStep.NodeID {
		_, err = tx.Exec(ctx, `UPDATE journey_steps
			SET status='completed', locked_until=NULL, updated_at=now()
			WHERE id=$1 AND run_id=$2 AND tenant_id=$3`,
			stepID, runID, run.TenantID)
		if err != nil {
			return fmt.Errorf("advance complete stale step: %w", err)
		}
		return tx.Commit(ctx)
	}

	if len(run.State) == 0 {
		run.State = json.RawMessage("{}")
	}
	res, err := tx.Exec(ctx, `UPDATE journey_runs SET
			status=$3,
			current_node_id=$4,
			state=$5,
			wait_event_type=$6,
			wait_until=$7,
			goal_reached=$8,
			updated_at=now(),
			completed_at=$9
		WHERE id=$1 AND tenant_id=$2 AND status=$10 AND current_node_id=$11`,
		runID, run.TenantID, run.Status, run.CurrentNodeID, run.State,
		run.WaitEventType, run.WaitUntil, run.GoalReached, run.CompletedAt, dbRunStatus, dbCurrentNodeID)
	if err != nil {
		return fmt.Errorf("advance update run: %w", err)
	}
	if res.RowsAffected() == 0 {
		return errors.New("advance run state changed concurrently")
	}

	_, err = tx.Exec(ctx, `UPDATE journey_steps SET status='completed', locked_until=NULL, updated_at=now() WHERE id=$1`, stepID)
	if err != nil {
		return fmt.Errorf("advance complete step: %w", err)
	}

	if nextStep != nil {
		if nextStep.Kind == "" {
			nextStep.Kind = "advance"
		}
		if nextStep.Status == "" {
			nextStep.Status = "pending"
		}
		if nextStep.AvailableAt.IsZero() {
			nextStep.AvailableAt = time.Now()
		}
		_, err = tx.Exec(ctx, `INSERT INTO journey_steps (
				run_id, tenant_id, node_id, kind, status, attempts, available_at, locked_until, error_message
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			nextStep.RunID, nextStep.TenantID, nextStep.NodeID, nextStep.Kind, nextStep.Status, nextStep.Attempts, nextStep.AvailableAt, nextStep.LockedUntil, nextStep.ErrorMessage)
		if err != nil {
			return fmt.Errorf("advance insert step: %w", err)
		}
	}

	if len(trans.Detail) == 0 {
		trans.Detail = json.RawMessage("{}")
	}
	_, err = tx.Exec(ctx, `INSERT INTO journey_transitions (
			run_id, tenant_id, from_node, to_node, node_type, outcome, detail
		) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		trans.RunID, trans.TenantID, trans.FromNode, trans.ToNode, trans.NodeType, trans.Outcome, trans.Detail)
	if err != nil {
		return fmt.Errorf("advance record transition: %w", err)
	}

	if messageIntent != nil {
		if len(messageIntent.PolicySnapshot) == 0 {
			messageIntent.PolicySnapshot = json.RawMessage("{}")
		}
		_, err = tx.Exec(ctx, `INSERT INTO journey_message_intents (
				run_id, tenant_id, workspace_id, journey_id, journey_version_id, node_id, profile_id,
				experiment_id, variant, template_id, channel, endpoint, transactional, status, attempts,
				available_at, locked_until, decision, reason, policy_snapshot
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
			ON CONFLICT (run_id, node_id) DO NOTHING`,
			messageIntent.RunID, messageIntent.TenantID, messageIntent.WorkspaceID, messageIntent.JourneyID,
			messageIntent.JourneyVersionID, messageIntent.NodeID, messageIntent.ProfileID, messageIntent.ExperimentID,
			messageIntent.Variant, messageIntent.TemplateID, messageIntent.Channel, messageIntent.Endpoint,
			messageIntent.Transactional, messageIntent.Status, messageIntent.Attempts, messageIntent.AvailableAt,
			messageIntent.LockedUntil, messageIntent.Decision, messageIntent.Reason, messageIntent.PolicySnapshot)
		if err != nil {
			return fmt.Errorf("advance insert message intent: %w", err)
		}
	}

	return tx.Commit(ctx)
}

func (s *Store) GetJourneyRunSystem(ctx context.Context, tenantID, runID string) (domain.JourneyRun, error) {
	var out domain.JourneyRun
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, journey_id, journey_version_id, profile_id,
			subject_external_id, entry_key, reentry_sequence, status, current_node_id,
			state, wait_event_type, wait_until, goal_reached, entered_at, updated_at, completed_at
		FROM journey_runs
		WHERE tenant_id=$1 AND id=$2`,
		tenantID, runID).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.JourneyID, &out.JourneyVersionID, &out.ProfileID,
			&out.SubjectExternalID, &out.EntryKey, &out.ReentrySequence, &out.Status, &out.CurrentNodeID,
			&out.State, &out.WaitEventType, &out.WaitUntil, &out.GoalReached, &out.EnteredAt, &out.UpdatedAt, &out.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.JourneyRun{}, ErrNotFound
	}
	return out, err
}

func (s *Store) GetJourneyRunsForProfile(ctx context.Context, tenantID, versionID, profileID string) ([]domain.JourneyRun, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, workspace_id, journey_id, journey_version_id, profile_id,
			subject_external_id, entry_key, reentry_sequence, status, current_node_id,
			state, wait_event_type, wait_until, goal_reached, entered_at, updated_at, completed_at
		FROM journey_runs
		WHERE tenant_id=$1 AND journey_version_id=$2 AND profile_id=$3
		ORDER BY reentry_sequence DESC`,
		tenantID, versionID, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.JourneyRun
	for rows.Next() {
		var r domain.JourneyRun
		err := rows.Scan(&r.ID, &r.TenantID, &r.WorkspaceID, &r.JourneyID, &r.JourneyVersionID, &r.ProfileID,
			&r.SubjectExternalID, &r.EntryKey, &r.ReentrySequence, &r.Status, &r.CurrentNodeID,
			&r.State, &r.WaitEventType, &r.WaitUntil, &r.GoalReached, &r.EnteredAt, &r.UpdatedAt, &r.CompletedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Store) GetJourneyRuns(ctx context.Context, p domain.Principal, journeyID string) ([]domain.JourneyRun, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, workspace_id, journey_id, journey_version_id, profile_id,
			subject_external_id, entry_key, reentry_sequence, status, current_node_id,
			state, wait_event_type, wait_until, goal_reached, entered_at, updated_at, completed_at
		FROM journey_runs
		WHERE tenant_id=$1 AND workspace_id=$2 AND journey_id=$3
		ORDER BY entered_at DESC`,
		p.TenantID, p.WorkspaceID, journeyID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.JourneyRun
	for rows.Next() {
		var r domain.JourneyRun
		err := rows.Scan(&r.ID, &r.TenantID, &r.WorkspaceID, &r.JourneyID, &r.JourneyVersionID, &r.ProfileID,
			&r.SubjectExternalID, &r.EntryKey, &r.ReentrySequence, &r.Status, &r.CurrentNodeID,
			&r.State, &r.WaitEventType, &r.WaitUntil, &r.GoalReached, &r.EnteredAt, &r.UpdatedAt, &r.CompletedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

func (s *Store) GetJourneyTransitions(ctx context.Context, p domain.Principal, runID string) ([]domain.JourneyTransition, error) {
	rows, err := s.pool.Query(ctx, `SELECT t.id, t.run_id, t.tenant_id, t.from_node, t.to_node, t.node_type, t.outcome, t.detail, t.occurred_at
		FROM journey_transitions t
		JOIN journey_runs r ON t.run_id = r.id
		WHERE t.tenant_id=$1 AND r.workspace_id=$2 AND t.run_id=$3
		ORDER BY t.occurred_at ASC`,
		p.TenantID, p.WorkspaceID, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.JourneyTransition
	for rows.Next() {
		var t domain.JourneyTransition
		err := rows.Scan(&t.ID, &t.RunID, &t.TenantID, &t.FromNode, &t.ToNode, &t.NodeType, &t.Outcome, &t.Detail, &t.OccurredAt)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func (s *Store) EvaluateAudience(ctx context.Context, p domain.Principal, profileID string, dsl json.RawMessage) (bool, error) {
	if len(dsl) == 0 || string(dsl) == "{}" || string(dsl) == "null" {
		return true, nil
	}
	node, err := audience.Parse(dsl)
	if err != nil {
		return false, fmt.Errorf("parse audience dsl: %w", err)
	}
	return audience.Matches(ctx, s, p.TenantID, p.WorkspaceID, p.AppID, profileID, node)
}

func (s *Store) QueryProfileMatches(ctx context.Context, sql string, args []any) (bool, error) {
	var val int
	err := s.pool.QueryRow(ctx, sql, args...).Scan(&val)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) QueryConsentMatches(ctx context.Context, sql string, args []any) (bool, error) {
	var val int
	err := s.pool.QueryRow(ctx, sql, args...).Scan(&val)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) QueryClickHouseMatches(ctx context.Context, sql string, args []any) (bool, error) {
	if s.chConn == nil {
		return false, errors.New("ClickHouse connection not available")
	}
	rows, err := s.chConn.Query(ctx, sql, args...)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	return rows.Next(), rows.Err()
}

func (s *Store) GetProfileExternalID(ctx context.Context, tenantID, workspaceID, profileID string) (string, error) {
	var extID string
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(external_id, '') FROM profiles WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`, tenantID, workspaceID, profileID).Scan(&extID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return extID, nil
}

func (s *Store) GetProfileByIDSystem(ctx context.Context, tenantID, workspaceID, profileID string) (domain.Profile, error) {
	var profile domain.Profile
	err := s.pool.QueryRow(ctx, `SELECT id, COALESCE(external_id, ''), COALESCE(anonymous_id, ''), attributes, version, updated_at
		FROM profiles WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`,
		tenantID, workspaceID, profileID).
		Scan(&profile.ID, &profile.ExternalID, &profile.AnonymousID, &profile.Attributes, &profile.Version, &profile.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Profile{}, ErrNotFound
	}
	return profile, err
}

func (s *Store) IsProfileInSegment(ctx context.Context, p domain.Principal, segmentID string, profileID string) (bool, error) {
	seg, err := s.GetSegment(ctx, p, segmentID)
	if err != nil {
		return false, err
	}

	matched := false
	if len(seg.DSL) > 0 && string(seg.DSL) != "{}" && string(seg.DSL) != "null" {
		matched, err = s.EvaluateAudience(ctx, p, profileID, seg.DSL)
		if err != nil {
			return false, err
		}
	}

	var membership string
	err = s.pool.QueryRow(ctx, `SELECT membership FROM segment_members WHERE tenant_id=$1 AND segment_id=$2 AND profile_id=$3`, p.TenantID, segmentID, profileID).Scan(&membership)
	if err == nil {
		if membership == "include" {
			matched = true
		} else if membership == "exclude" {
			matched = false
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}

	return matched, nil
}

func (s *Store) UpdateProfileAttributes(ctx context.Context, p domain.Principal, profileID string, attrs map[string]any) error {
	attrsJSON, err := json.Marshal(attrs)
	if err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `UPDATE profiles SET attributes = attributes || $4, updated_at=now(), version=version+1
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`,
		p.TenantID, p.WorkspaceID, profileID, attrsJSON)
	return err
}

func (s *Store) ListActiveScheduledJourneyVersions(ctx context.Context) ([]domain.JourneyVersion, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, journey_id, tenant_id, workspace_id, version, graph, manifest_key,
			entry_kind, entry_event_type, entry_segment_id, entry_schedule,
			reentry_policy, max_reentries, late_policy, status, published_by, published_at
		FROM journey_versions
		WHERE status='active' AND entry_kind='scheduled'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.JourneyVersion
	for rows.Next() {
		var v domain.JourneyVersion
		err := rows.Scan(&v.ID, &v.JourneyID, &v.TenantID, &v.WorkspaceID, &v.Version, &v.Graph, &v.ManifestKey,
			&v.EntryKind, &v.EntryEventType, &v.EntrySegmentID, &v.EntrySchedule,
			&v.ReentryPolicy, &v.MaxReentries, &v.LatePolicy, &v.Status, &v.PublishedBy, &v.PublishedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Store) enrollEventTriggered(ctx context.Context, tx pgx.Tx, event domain.AcceptedEvent, profileID string) error {
	rows, err := tx.Query(ctx, `
		SELECT id, journey_id, reentry_policy, max_reentries, graph
		FROM journey_versions
		WHERE tenant_id = $1 AND workspace_id = $2 AND entry_kind = 'event' AND entry_event_type = $3 AND status = 'active'
	`, event.Principal.TenantID, event.Principal.WorkspaceID, event.Type)
	if err != nil {
		return err
	}
	defer rows.Close()

	type item struct {
		id            string
		journeyID     string
		reentryPolicy string
		maxReentries  int
		graph         []byte
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.journeyID, &it.reentryPolicy, &it.maxReentries, &it.graph); err != nil {
			return err
		}
		items = append(items, it)
	}

	for _, it := range items {
		var runs []struct {
			status          string
			reentrySequence int
		}
		runRows, err := tx.Query(ctx, `
			SELECT status, reentry_sequence
			FROM journey_runs
			WHERE journey_version_id = $1 AND profile_id = $2
			ORDER BY reentry_sequence DESC
		`, it.id, profileID)
		if err != nil {
			return err
		}
		for runRows.Next() {
			var r struct {
				status          string
				reentrySequence int
			}
			if err := runRows.Scan(&r.status, &r.reentrySequence); err != nil {
				runRows.Close()
				return err
			}
			runs = append(runs, r)
		}
		runRows.Close()

		var reentrySeq int
		if len(runs) == 0 {
			reentrySeq = 0
		} else {
			if it.reentryPolicy == "once" {
				continue
			}
			if it.reentryPolicy == "after_exit" {
				isActiveOrWaiting := false
				for _, r := range runs {
					if r.status == "active" || r.status == "waiting" {
						isActiveOrWaiting = true
						break
					}
				}
				if isActiveOrWaiting {
					continue
				}
			}
			reentrySeq = runs[0].reentrySequence + 1
			if reentrySeq > it.maxReentries {
				continue
			}
		}

		var graphObj struct {
			EntryNodeID string `json:"entry_node_id"`
		}
		if err := json.Unmarshal(it.graph, &graphObj); err != nil {
			return fmt.Errorf("decode entry_node_id from graph: %w", err)
		}
		if graphObj.EntryNodeID == "" {
			continue
		}

		var runID string
		err = tx.QueryRow(ctx, `
			INSERT INTO journey_runs (
				tenant_id, workspace_id, journey_id, journey_version_id, profile_id,
				subject_external_id, entry_key, reentry_sequence, status, current_node_id, state
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'active', $9, '{}'::jsonb)
			ON CONFLICT (journey_version_id, profile_id, entry_key, reentry_sequence) DO NOTHING
			RETURNING id
		`, event.Principal.TenantID, event.Principal.WorkspaceID, it.journeyID, it.id, profileID,
			event.ExternalID, event.ID, reentrySeq, graphObj.EntryNodeID).Scan(&runID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}

		telemetry.JourneyEnrollments.Add(ctx, 1, otelmetric.WithAttributes(
			attribute.String("tenant_id", event.Principal.TenantID),
			attribute.String("journey_id", it.journeyID),
		))

		_, err = tx.Exec(ctx, `
			INSERT INTO journey_steps (run_id, tenant_id, node_id, kind, status, available_at)
			VALUES ($1, $2, $3, 'advance', 'pending', now())
		`, runID, event.Principal.TenantID, graphObj.EntryNodeID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) resolveWaitingRuns(ctx context.Context, tx pgx.Tx, event domain.AcceptedEvent) error {
	if event.ExternalID == "" {
		return nil
	}

	rows, err := tx.Query(ctx, `
		SELECT r.id, r.current_node_id, v.graph
		FROM journey_runs r
		JOIN journey_versions v ON r.journey_version_id = v.id
		JOIN profiles p ON r.profile_id = p.id
		WHERE r.tenant_id = $1 AND r.workspace_id = $2 AND p.app_id = $3 AND r.status = 'waiting' AND r.wait_event_type = $4 AND r.subject_external_id = $5
	`, event.Principal.TenantID, event.Principal.WorkspaceID, event.Principal.AppID, event.Type, event.ExternalID)
	if err != nil {
		return err
	}
	defer rows.Close()

	type runItem struct {
		id            string
		currentNodeID string
		graph         []byte
	}
	var items []runItem
	for rows.Next() {
		var it runItem
		if err := rows.Scan(&it.id, &it.currentNodeID, &it.graph); err != nil {
			return err
		}
		items = append(items, it)
	}

	for _, it := range items {
		var graphObj struct {
			Edges []struct {
				From   string `json:"from"`
				To     string `json:"to"`
				Branch string `json:"branch"`
			} `json:"edges"`
		}
		if err := json.Unmarshal(it.graph, &graphObj); err != nil {
			return err
		}

		var successor string
		for _, edge := range graphObj.Edges {
			if edge.From == it.currentNodeID && edge.Branch == "success" {
				successor = edge.To
				break
			}
		}
		if successor == "" {
			continue
		}

		res, err := tx.Exec(ctx, `
			UPDATE journey_runs
			SET status = 'active', wait_event_type = NULL, wait_until = NULL, current_node_id = $1, updated_at = now()
			WHERE id = $2 AND status = 'waiting'
		`, successor, it.id)
		if err != nil {
			return err
		}
		affected := res.RowsAffected()
		if affected == 0 {
			continue
		}

		_, err = tx.Exec(ctx, `
			UPDATE journey_steps
			SET status = 'completed', updated_at = now()
			WHERE run_id = $1 AND status IN ('pending', 'processing', 'failed') AND kind = 'timeout'
		`, it.id)
		if err != nil {
			return err
		}

		_, err = tx.Exec(ctx, `
			INSERT INTO journey_steps (run_id, tenant_id, node_id, kind, status, available_at)
			VALUES ($1, $2, $3, 'advance', 'pending', now())
		`, it.id, event.Principal.TenantID, successor)
		if err != nil {
			return err
		}

		fromNode := it.currentNodeID
		toNode := successor
		_, err = tx.Exec(ctx, `
			INSERT INTO journey_transitions (run_id, tenant_id, from_node, to_node, node_type, outcome, detail)
			VALUES ($1, $2, $3, $4, $5, $6, '{}'::jsonb)
		`, it.id, event.Principal.TenantID, &fromNode, &toNode, "wait_event", "success")
		if err != nil {
			return err
		}
	}

	return nil
}

func (s *Store) ClaimJourneyMessageIntent(ctx context.Context, workerID string) (domain.JourneyMessageIntent, bool, error) {
	var out domain.JourneyMessageIntent
	err := s.pool.QueryRow(ctx, `UPDATE journey_message_intents SET
			status='processing',
			attempts=attempts+1,
			locked_until=now() + INTERVAL '5 minutes',
			updated_at=now()
		WHERE id = (
			SELECT jmi.id FROM journey_message_intents jmi
			JOIN journey_versions jv ON jv.id = jmi.journey_version_id
			WHERE jv.status = 'active'
			  AND (
				(jmi.status IN ('pending', 'failed') AND jmi.attempts < 3 AND jmi.available_at <= now())
				OR (jmi.status='processing' AND jmi.locked_until <= now())
			)
			AND (
				SELECT COUNT(*) FROM journey_message_intents
				WHERE tenant_id = jmi.tenant_id AND status = 'processing' AND locked_until > now()
			) < 10
			ORDER BY jmi.transactional DESC, jmi.available_at ASC
			FOR UPDATE OF jmi SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, run_id, tenant_id, workspace_id, journey_id, journey_version_id, node_id, profile_id, experiment_id, variant, template_id, channel, endpoint, transactional, status, attempts, available_at, locked_until, decision, reason, provider_message_id, policy_snapshot, error_message, created_at, updated_at`).
		Scan(&out.ID, &out.RunID, &out.TenantID, &out.WorkspaceID, &out.JourneyID, &out.JourneyVersionID, &out.NodeID, &out.ProfileID, &out.ExperimentID, &out.Variant, &out.TemplateID, &out.Channel, &out.Endpoint, &out.Transactional, &out.Status, &out.Attempts, &out.AvailableAt, &out.LockedUntil, &out.Decision, &out.Reason, &out.ProviderMessageID, &out.PolicySnapshot, &out.ErrorMessage, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.JourneyMessageIntent{}, false, nil
	}
	if err != nil {
		return domain.JourneyMessageIntent{}, false, err
	}
	return out, true, nil
}

func (s *Store) UpdateJourneyMessageIntent(ctx context.Context, intent domain.JourneyMessageIntent) error {
	if len(intent.PolicySnapshot) == 0 {
		intent.PolicySnapshot = json.RawMessage("{}")
	}
	_, err := s.pool.Exec(ctx, `UPDATE journey_message_intents SET
		status = $1,
		attempts = $2,
		available_at = $3,
		locked_until = $4,
		decision = $5,
		reason = $6,
		provider_message_id = $7,
		policy_snapshot = $8,
		error_message = $9,
		updated_at = now()
	WHERE id = $10`,
		intent.Status,
		intent.Attempts,
		intent.AvailableAt,
		intent.LockedUntil,
		intent.Decision,
		intent.Reason,
		intent.ProviderMessageID,
		intent.PolicySnapshot,
		intent.ErrorMessage,
		intent.ID,
	)
	return err
}

func (s *Store) GetJourneyDLQ(ctx context.Context, p domain.Principal) ([]domain.JourneyStep, []domain.JourneyMessageIntent, error) {
	steps := []domain.JourneyStep{}
	rowsSteps, err := s.pool.Query(ctx, `SELECT js.id, js.run_id, js.tenant_id, js.node_id, js.kind, js.status,
			js.attempts, js.available_at, js.locked_until, js.error_message, js.created_at, js.updated_at
		FROM journey_steps js
		JOIN journey_runs jr ON jr.id = js.run_id
		WHERE js.tenant_id = $1 AND jr.workspace_id = $2 AND js.status = 'dead'
		ORDER BY js.created_at DESC`, p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, nil, err
	}
	defer rowsSteps.Close()

	for rowsSteps.Next() {
		var step domain.JourneyStep
		err := rowsSteps.Scan(&step.ID, &step.RunID, &step.TenantID, &step.NodeID, &step.Kind, &step.Status,
			&step.Attempts, &step.AvailableAt, &step.LockedUntil, &step.ErrorMessage, &step.CreatedAt, &step.UpdatedAt)
		if err != nil {
			return nil, nil, err
		}
		steps = append(steps, step)
	}

	intents := []domain.JourneyMessageIntent{}
	rowsIntents, err := s.pool.Query(ctx, `SELECT id, run_id, tenant_id, workspace_id, journey_id, journey_version_id,
			node_id, profile_id, template_id, channel, endpoint, transactional, status, attempts,
			available_at, locked_until, decision, reason, provider_message_id, policy_snapshot, error_message,
			created_at, updated_at
		FROM journey_message_intents
		WHERE tenant_id = $1 AND workspace_id = $2 AND status = 'dead'
		ORDER BY created_at DESC`, p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, nil, err
	}
	defer rowsIntents.Close()

	for rowsIntents.Next() {
		var intent domain.JourneyMessageIntent
		err := rowsIntents.Scan(&intent.ID, &intent.RunID, &intent.TenantID, &intent.WorkspaceID, &intent.JourneyID, &intent.JourneyVersionID,
			&intent.NodeID, &intent.ProfileID, &intent.TemplateID, &intent.Channel, &intent.Endpoint, &intent.Transactional, &intent.Status, &intent.Attempts,
			&intent.AvailableAt, &intent.LockedUntil, &intent.Decision, &intent.Reason, &intent.ProviderMessageID, &intent.PolicySnapshot, &intent.ErrorMessage,
			&intent.CreatedAt, &intent.UpdatedAt)
		if err != nil {
			return nil, nil, err
		}
		intents = append(intents, intent)
	}

	return steps, intents, nil
}

func (s *Store) RetryJourneyStep(ctx context.Context, p domain.Principal, stepID string) error {
	res, err := s.pool.Exec(ctx, `UPDATE journey_steps SET
			status = 'pending',
			attempts = 0,
			available_at = now(),
			locked_until = NULL,
			error_message = NULL,
			updated_at = now()
		WHERE id = $3 AND tenant_id = $1 AND EXISTS (
			SELECT 1 FROM journey_runs WHERE id = run_id AND workspace_id = $2
		)`, p.TenantID, p.WorkspaceID, stepID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RetryJourneyMessageIntent(ctx context.Context, p domain.Principal, intentID string) error {
	res, err := s.pool.Exec(ctx, `UPDATE journey_message_intents SET
			status = 'pending',
			attempts = 0,
			available_at = now(),
			locked_until = NULL,
			error_message = NULL,
			updated_at = now()
		WHERE id = $3 AND tenant_id = $1 AND workspace_id = $2`, p.TenantID, p.WorkspaceID, intentID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
