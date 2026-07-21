package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/experiment"
	"github.com/jackc/pgx/v5"
)

// CampaignReport reads only campaign dispositions and projection-maintained fact
// tables. Total is the number of rows at a stage; unique is COUNT(DISTINCT
// profile_id). Bounce and complaint rates divide total facts by total sent.
// When query is empty (zero value), returns today's point-in-time report
// (backward-compatible).
func (s *Store) CampaignReport(ctx context.Context, p domain.Principal, campaignID string, query domain.ReportQuery) (domain.CampaignReport, error) {
	if err := s.requireCampaignSource(ctx, p, campaignID); err != nil {
		return domain.CampaignReport{}, err
	}

	report := domain.CampaignReport{CampaignID: campaignID}

	// Build WHERE clause for time filtering when query provides a range
	whereClause := `WHERE c.tenant_id=$1 AND c.workspace_id=$2 AND c.id=$3
		AND d.tenant_id=$1 AND d.campaign_id=$3`
	args := []interface{}{p.TenantID, p.WorkspaceID, campaignID}
	argIdx := 4

	if !query.Start.IsZero() && !query.End.IsZero() {
		whereClause += fmt.Sprintf(` AND d.attempted_at BETWEEN $%d AND $%d`, argIdx, argIdx+1)
		args = append(args, query.Start, query.End)
		argIdx += 2
	}

	sql := fmt.Sprintf(`
		SELECT
			COUNT(*), COUNT(DISTINCT d.profile_id),
			COUNT(*) FILTER (WHERE d.decision='sent'), COUNT(DISTINCT d.profile_id) FILTER (WHERE d.decision='sent'),
			COUNT(*) FILTER (WHERE d.decision='suppressed'), COUNT(DISTINCT d.profile_id) FILTER (WHERE d.decision='suppressed'),
			COUNT(*) FILTER (WHERE d.decision='no_consent'), COUNT(DISTINCT d.profile_id) FILTER (WHERE d.decision='no_consent'),
			COUNT(*) FILTER (WHERE d.decision='fatigued'), COUNT(DISTINCT d.profile_id) FILTER (WHERE d.decision='fatigued'),
			COUNT(*) FILTER (WHERE d.decision='render_failed'), COUNT(DISTINCT d.profile_id) FILTER (WHERE d.decision='render_failed'),
			COUNT(*) FILTER (WHERE d.decision='send_failed'), COUNT(DISTINCT d.profile_id) FILTER (WHERE d.decision='send_failed'),
			COUNT(*) FILTER (WHERE d.decision='failed'), COUNT(DISTINCT d.profile_id) FILTER (WHERE d.decision='failed'),
			COUNT(*) FILTER (WHERE d.decision='holdout'), COUNT(DISTINCT d.profile_id) FILTER (WHERE d.decision='holdout')
		FROM delivery_attempts d
		JOIN campaigns c ON c.id=d.campaign_id AND c.tenant_id=d.tenant_id
		%s`, whereClause)

	if err := s.pool.QueryRow(ctx, sql, args...).Scan(
		&report.Funnel.Targeted.Total, &report.Funnel.Targeted.Unique,
		&report.Funnel.Sent.Total, &report.Funnel.Sent.Unique,
		&report.Funnel.Suppressed.Total, &report.Funnel.Suppressed.Unique,
		&report.Funnel.NoConsent.Total, &report.Funnel.NoConsent.Unique,
		&report.Funnel.Fatigued.Total, &report.Funnel.Fatigued.Unique,
		&report.Funnel.RenderFailed.Total, &report.Funnel.RenderFailed.Unique,
		&report.Funnel.SendFailed.Total, &report.Funnel.SendFailed.Unique,
		&report.Funnel.Failed.Total, &report.Funnel.Failed.Unique,
		&report.Funnel.Holdout.Total, &report.Funnel.Holdout.Unique,
	); err != nil {
		return domain.CampaignReport{}, err
	}
	if err := s.readReportFacts(ctx, p, "campaign", campaignID, query, &report.Funnel, &report.Deliverability); err != nil {
		return domain.CampaignReport{}, err
	}
	setDeliverabilityRates(report.Funnel.Sent.Total, &report.Deliverability)
	return report, nil
}

// JourneyReport uses the same definitions as CampaignReport, with message
// intents as targeted dispositions and journey projection facts as later stages.
// When query is empty (zero value), returns today's point-in-time report
// (backward-compatible).
func (s *Store) JourneyReport(ctx context.Context, p domain.Principal, journeyID string, query domain.ReportQuery) (domain.JourneyReport, error) {
	if err := s.requireJourneySource(ctx, p, journeyID); err != nil {
		return domain.JourneyReport{}, err
	}

	report := domain.JourneyReport{JourneyID: journeyID}

	// Build WHERE clause for time filtering when query provides a range
	whereClause := `WHERE tenant_id=$1 AND workspace_id=$2 AND journey_id=$3`
	args := []interface{}{p.TenantID, p.WorkspaceID, journeyID}
	argIdx := 4

	if !query.Start.IsZero() && !query.End.IsZero() {
		whereClause += fmt.Sprintf(` AND updated_at BETWEEN $%d AND $%d`, argIdx, argIdx+1)
		args = append(args, query.Start, query.End)
		argIdx += 2
	}

	sql := fmt.Sprintf(`
		SELECT
			COUNT(*), COUNT(DISTINCT profile_id),
			COUNT(*) FILTER (WHERE decision='sent'), COUNT(DISTINCT profile_id) FILTER (WHERE decision='sent'),
			COUNT(*) FILTER (WHERE decision='suppressed'), COUNT(DISTINCT profile_id) FILTER (WHERE decision='suppressed'),
			COUNT(*) FILTER (WHERE decision='no_consent'), COUNT(DISTINCT profile_id) FILTER (WHERE decision='no_consent'),
			COUNT(*) FILTER (WHERE decision='fatigued'), COUNT(DISTINCT profile_id) FILTER (WHERE decision='fatigued'),
			COUNT(*) FILTER (WHERE decision='render_failed'), COUNT(DISTINCT profile_id) FILTER (WHERE decision='render_failed'),
			COUNT(*) FILTER (WHERE decision='send_failed'), COUNT(DISTINCT profile_id) FILTER (WHERE decision='send_failed'),
			COUNT(*) FILTER (WHERE decision='failed'), COUNT(DISTINCT profile_id) FILTER (WHERE decision='failed'),
			COUNT(*) FILTER (WHERE decision='holdout'), COUNT(DISTINCT profile_id) FILTER (WHERE decision='holdout')
		FROM journey_message_intents
		%s`, whereClause)

	if err := s.pool.QueryRow(ctx, sql, args...).Scan(
		&report.Funnel.Targeted.Total, &report.Funnel.Targeted.Unique,
		&report.Funnel.Sent.Total, &report.Funnel.Sent.Unique,
		&report.Funnel.Suppressed.Total, &report.Funnel.Suppressed.Unique,
		&report.Funnel.NoConsent.Total, &report.Funnel.NoConsent.Unique,
		&report.Funnel.Fatigued.Total, &report.Funnel.Fatigued.Unique,
		&report.Funnel.RenderFailed.Total, &report.Funnel.RenderFailed.Unique,
		&report.Funnel.SendFailed.Total, &report.Funnel.SendFailed.Unique,
		&report.Funnel.Failed.Total, &report.Funnel.Failed.Unique,
		&report.Funnel.Holdout.Total, &report.Funnel.Holdout.Unique,
	); err != nil {
		return domain.JourneyReport{}, err
	}
	if err := s.readReportFacts(ctx, p, "journey", journeyID, query, &report.Funnel, &report.Deliverability); err != nil {
		return domain.JourneyReport{}, err
	}
	setDeliverabilityRates(report.Funnel.Sent.Total, &report.Deliverability)
	return report, nil
}

func (s *Store) requireCampaignSource(ctx context.Context, p domain.Principal, sourceID string) error {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT id FROM campaigns
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`, p.TenantID, p.WorkspaceID, sourceID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (s *Store) requireJourneySource(ctx context.Context, p domain.Principal, sourceID string) error {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT id FROM journeys
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`, p.TenantID, p.WorkspaceID, sourceID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (s *Store) readReportFacts(ctx context.Context, p domain.Principal, sourceType, sourceID string, query domain.ReportQuery, funnel *domain.ReportFunnel, deliverability *domain.ReportDeliverability) error {
	// Build WHERE clause for engagement_facts
	whereClauseEng := `WHERE tenant_id=$1 AND workspace_id=$2 AND source_id=$3 AND source_type=$4`
	argsEng := []interface{}{p.TenantID, p.WorkspaceID, sourceID, sourceType}
	argIdxEng := 5

	if !query.Start.IsZero() && !query.End.IsZero() {
		whereClauseEng += fmt.Sprintf(` AND occurred_at BETWEEN $%d AND $%d`, argIdxEng, argIdxEng+1)
		argsEng = append(argsEng, query.Start, query.End)
		argIdxEng += 2
	}

	sqlEng := fmt.Sprintf(`
		SELECT
			COUNT(*) FILTER (WHERE event_type='delivered'), COUNT(DISTINCT profile_id) FILTER (WHERE event_type='delivered'),
			COUNT(*) FILTER (WHERE event_type='opened'), COUNT(DISTINCT profile_id) FILTER (WHERE event_type='opened'),
			COUNT(*) FILTER (WHERE event_type='clicked'), COUNT(DISTINCT profile_id) FILTER (WHERE event_type='clicked'),
			COUNT(*) FILTER (WHERE event_type='bounced'), COUNT(DISTINCT profile_id) FILTER (WHERE event_type='bounced'),
			COUNT(*) FILTER (WHERE event_type='complained'), COUNT(DISTINCT profile_id) FILTER (WHERE event_type='complained')
		FROM engagement_facts
		%s`, whereClauseEng)

	if err := s.pool.QueryRow(ctx, sqlEng, argsEng...).Scan(
		&funnel.Delivered.Total, &funnel.Delivered.Unique,
		&funnel.Opened.Total, &funnel.Opened.Unique,
		&funnel.Clicked.Total, &funnel.Clicked.Unique,
		&deliverability.Bounced.Total, &deliverability.Bounced.Unique,
		&deliverability.Complained.Total, &deliverability.Complained.Unique,
	); err != nil {
		return err
	}

	// Build WHERE clause for conversion_facts
	whereClauseConv := `WHERE tenant_id=$1 AND workspace_id=$2 AND source_id=$3 AND source_type=$4`
	argsConv := []interface{}{p.TenantID, p.WorkspaceID, sourceID, sourceType}
	argIdxConv := 5

	if !query.Start.IsZero() && !query.End.IsZero() {
		whereClauseConv += fmt.Sprintf(` AND occurred_at BETWEEN $%d AND $%d`, argIdxConv, argIdxConv+1)
		argsConv = append(argsConv, query.Start, query.End)
		argIdxConv += 2
	}

	sqlConv := fmt.Sprintf(`
		SELECT COUNT(*), COUNT(DISTINCT profile_id)
		FROM conversion_facts
		%s`, whereClauseConv)

	return s.pool.QueryRow(ctx, sqlConv, argsConv...).Scan(&funnel.Converted.Total, &funnel.Converted.Unique)
}

func setDeliverabilityRates(sent int64, deliverability *domain.ReportDeliverability) {
	if sent == 0 {
		return
	}
	deliverability.BounceRate = float64(deliverability.Bounced.Total) / float64(sent)
	deliverability.ComplaintRate = float64(deliverability.Complained.Total) / float64(sent)
}

// ExperimentReport generates a statistical report for an experiment, comparing
// each variant to the control variant on the primary goal and reporting guardrail rates.
func (s *Store) ExperimentReport(ctx context.Context, p domain.Principal, experimentID string, reportQuery domain.ReportQuery) (domain.ExperimentReport, error) {
	e, err := s.GetExperiment(ctx, p, experimentID)
	if err != nil {
		return domain.ExperimentReport{}, err
	}

	var query string
	if e.SubjectType == "campaign" {
		query = `SELECT d.variant, COUNT(*)
			FROM delivery_attempts d
			JOIN campaigns c ON c.id = d.campaign_id AND c.tenant_id = d.tenant_id
			WHERE c.tenant_id = $1 AND c.workspace_id = $2 AND d.experiment_id = $3
				AND (d.decision = 'sent' OR d.decision = 'holdout') AND d.variant IS NOT NULL
			GROUP BY d.variant`
	} else {
		query = `SELECT variant, COUNT(*)
			FROM journey_message_intents
			WHERE tenant_id = $1 AND workspace_id = $2 AND experiment_id = $3
				AND (decision = 'sent' OR decision = 'holdout') AND variant IS NOT NULL
			GROUP BY variant`
	}

	sends := make(map[string]int64)
	rows, err := s.pool.Query(ctx, query, p.TenantID, p.WorkspaceID, experimentID)
	if err != nil {
		return domain.ExperimentReport{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var variant string
		var count int64
		if err := rows.Scan(&variant, &count); err != nil {
			return domain.ExperimentReport{}, err
		}
		sends[variant] = count
	}
	if err := rows.Err(); err != nil {
		return domain.ExperimentReport{}, err
	}

	conversions := make(map[string]map[string]int64)
	cRows, err := s.pool.Query(ctx, `
		SELECT variant, goal_name, COUNT(*)
		FROM conversion_facts
		WHERE tenant_id = $1 AND workspace_id = $2 AND experiment_id = $3 AND variant IS NOT NULL
		GROUP BY variant, goal_name`, p.TenantID, p.WorkspaceID, experimentID)
	if err != nil {
		return domain.ExperimentReport{}, err
	}
	defer cRows.Close()
	for cRows.Next() {
		var variant, goalName string
		var count int64
		if err := cRows.Scan(&variant, &goalName, &count); err != nil {
			return domain.ExperimentReport{}, err
		}
		if conversions[variant] == nil {
			conversions[variant] = make(map[string]int64)
		}
		conversions[variant][goalName] = count
	}
	if err := cRows.Err(); err != nil {
		return domain.ExperimentReport{}, err
	}

	var primaryGoal struct {
		EventType string `json:"event_type"`
		Name      string `json:"name"`
	}
	if len(e.PrimaryGoal) > 0 {
		_ = json.Unmarshal(e.PrimaryGoal, &primaryGoal)
	}
	primaryGoalName := primaryGoal.Name
	if primaryGoalName == "" {
		primaryGoalName = primaryGoal.EventType
	}

	var guardrailGoals []struct {
		EventType string `json:"event_type"`
		Name      string `json:"name"`
	}
	if len(e.GuardrailGoals) > 0 {
		_ = json.Unmarshal(e.GuardrailGoals, &guardrailGoals)
	}

	var controlVariant *domain.ExperimentVariant
	for i := range e.Variants {
		if e.Variants[i].IsControl {
			controlVariant = &e.Variants[i]
			break
		}
	}

	var allLabels []string
	labelToIsControl := make(map[string]bool)
	for _, v := range e.Variants {
		allLabels = append(allLabels, v.Label)
		labelToIsControl[v.Label] = v.IsControl
	}

	hasHoldout := e.HoldoutPct > 0
	if !hasHoldout {
		if sends["holdout"] > 0 {
			hasHoldout = true
		} else {
			for _, g := range conversions["holdout"] {
				if g > 0 {
					hasHoldout = true
					break
				}
			}
		}
	}
	if hasHoldout {
		allLabels = append(allLabels, "holdout")
		labelToIsControl["holdout"] = false
	}

	report := domain.ExperimentReport{
		ExperimentID: experimentID,
		Variants:     []domain.ExperimentVariantReport{},
	}

	for _, label := range allLabels {
		sent := sends[label]
		conv := conversions[label][primaryGoalName]

		var stats experiment.VariantStats
		if controlVariant != nil {
			controlLabel := controlVariant.Label
			controlSent := sends[controlLabel]
			controlConv := conversions[controlLabel][primaryGoalName]
			stats = experiment.CompareProportions(controlSent, controlConv, sent, conv)
		} else {
			stats = experiment.CompareProportions(0, 0, sent, conv)
		}

		vr := domain.ExperimentVariantReport{
			Label:       label,
			IsControl:   labelToIsControl[label],
			Sent:        sent,
			Conversions: conv,
			Rate:        stats.Rate,
			Uplift:      stats.Uplift,
			ZScore:      stats.ZScore,
			PValue:      stats.PValue,
			CILow:       stats.CILow,
			CIHigh:      stats.CIHigh,
			Guardrails:  []domain.ExperimentGuardrail{},
		}

		for _, gGoal := range guardrailGoals {
			gName := gGoal.Name
			if gName == "" {
				gName = gGoal.EventType
			}
			gConv := conversions[label][gName]
			var gRate float64
			if sent > 0 {
				gRate = float64(gConv) / float64(sent)
			}
			vr.Guardrails = append(vr.Guardrails, domain.ExperimentGuardrail{
				GoalName:    gName,
				Conversions: gConv,
				Rate:        gRate,
			})
		}

		report.Variants = append(report.Variants, vr)
	}

	winner := recommendWinner(report.Variants)
	if (winner == nil && e.WinnerVariant != nil) || (winner != nil && (e.WinnerVariant == nil || *winner != *e.WinnerVariant)) {
		result, err := s.pool.Exec(ctx, `UPDATE experiments SET winner_variant=$1, updated_at=now()
			WHERE tenant_id=$2 AND workspace_id=$3 AND id=$4`, winner, p.TenantID, p.WorkspaceID, e.ID)
		if err != nil {
			return domain.ExperimentReport{}, err
		}
		if result.RowsAffected() == 0 {
			return domain.ExperimentReport{}, ErrNotFound
		}
	}
	report.WinnerVariant = winner

	return report, nil
}

func recommendWinner(variants []domain.ExperimentVariantReport) *string {
	var control *domain.ExperimentVariantReport
	for i := range variants {
		if variants[i].IsControl {
			control = &variants[i]
			break
		}
	}
	if control == nil || control.Sent == 0 {
		return nil
	}

	var bestVariant *domain.ExperimentVariantReport
	for i := range variants {
		v := &variants[i]
		if v.IsControl {
			continue
		}

		// A recommendation requires statistically significant positive uplift on
		// the primary goal.
		if v.PValue >= 0.05 || v.Uplift <= 0 {
			continue
		}

		// Guardrail facts represent adverse outcomes (for example churn or
		// complaints), so a statistically significant increase is a regression.
		hasRegression := false
		for _, vg := range v.Guardrails {
			// Find control's matching guardrail
			var cg *domain.ExperimentGuardrail
			for _, cgCand := range control.Guardrails {
				if cgCand.GoalName == vg.GoalName {
					cg = &cgCand
					break
				}
			}
			if cg == nil {
				continue
			}

			if vg.Rate > cg.Rate {
				stats := experiment.CompareProportions(control.Sent, cg.Conversions, v.Sent, vg.Conversions)
				if stats.PValue < 0.05 && stats.ZScore > 0 {
					hasRegression = true
					break
				}
			}
		}

		if hasRegression {
			continue
		}

		if bestVariant == nil || v.Rate > bestVariant.Rate {
			bestVariant = v
		}
	}

	if bestVariant != nil {
		ret := bestVariant.Label
		return &ret
	}
	return nil
}
