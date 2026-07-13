package postgres

import (
	"context"
	"errors"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

// CampaignReport reads only campaign dispositions and projection-maintained fact
// tables. Total is the number of rows at a stage; unique is COUNT(DISTINCT
// profile_id). Bounce and complaint rates divide total facts by total sent.
func (s *Store) CampaignReport(ctx context.Context, p domain.Principal, campaignID string) (domain.CampaignReport, error) {
	if err := s.requireCampaignSource(ctx, p, campaignID); err != nil {
		return domain.CampaignReport{}, err
	}

	report := domain.CampaignReport{CampaignID: campaignID}
	if err := s.pool.QueryRow(ctx, `
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
		WHERE c.tenant_id=$1 AND c.workspace_id=$2 AND c.id=$3
			AND d.tenant_id=$1 AND d.campaign_id=$3`, p.TenantID, p.WorkspaceID, campaignID).Scan(
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
	if err := s.readReportFacts(ctx, p, "campaign", campaignID, &report.Funnel, &report.Deliverability); err != nil {
		return domain.CampaignReport{}, err
	}
	setDeliverabilityRates(report.Funnel.Sent.Total, &report.Deliverability)
	return report, nil
}

// JourneyReport uses the same definitions as CampaignReport, with message
// intents as targeted dispositions and journey projection facts as later stages.
func (s *Store) JourneyReport(ctx context.Context, p domain.Principal, journeyID string) (domain.JourneyReport, error) {
	if err := s.requireJourneySource(ctx, p, journeyID); err != nil {
		return domain.JourneyReport{}, err
	}

	report := domain.JourneyReport{JourneyID: journeyID}
	if err := s.pool.QueryRow(ctx, `
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
		WHERE tenant_id=$1 AND workspace_id=$2 AND journey_id=$3`, p.TenantID, p.WorkspaceID, journeyID).Scan(
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
	if err := s.readReportFacts(ctx, p, "journey", journeyID, &report.Funnel, &report.Deliverability); err != nil {
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

func (s *Store) readReportFacts(ctx context.Context, p domain.Principal, sourceType, sourceID string, funnel *domain.ReportFunnel, deliverability *domain.ReportDeliverability) error {
	if err := s.pool.QueryRow(ctx, `
		SELECT
			COUNT(*) FILTER (WHERE event_type='delivered'), COUNT(DISTINCT profile_id) FILTER (WHERE event_type='delivered'),
			COUNT(*) FILTER (WHERE event_type='opened'), COUNT(DISTINCT profile_id) FILTER (WHERE event_type='opened'),
			COUNT(*) FILTER (WHERE event_type='clicked'), COUNT(DISTINCT profile_id) FILTER (WHERE event_type='clicked'),
			COUNT(*) FILTER (WHERE event_type='bounced'), COUNT(DISTINCT profile_id) FILTER (WHERE event_type='bounced'),
			COUNT(*) FILTER (WHERE event_type='complained'), COUNT(DISTINCT profile_id) FILTER (WHERE event_type='complained')
		FROM engagement_facts
		WHERE tenant_id=$1 AND workspace_id=$2 AND source_id=$3 AND source_type=$4`,
		p.TenantID, p.WorkspaceID, sourceID, sourceType).Scan(
		&funnel.Delivered.Total, &funnel.Delivered.Unique,
		&funnel.Opened.Total, &funnel.Opened.Unique,
		&funnel.Clicked.Total, &funnel.Clicked.Unique,
		&deliverability.Bounced.Total, &deliverability.Bounced.Unique,
		&deliverability.Complained.Total, &deliverability.Complained.Unique,
	); err != nil {
		return err
	}
	return s.pool.QueryRow(ctx, `
		SELECT COUNT(*), COUNT(DISTINCT profile_id)
		FROM conversion_facts
		WHERE tenant_id=$1 AND workspace_id=$2 AND source_id=$3 AND source_type=$4`,
		p.TenantID, p.WorkspaceID, sourceID, sourceType).Scan(&funnel.Converted.Total, &funnel.Converted.Unique)
}

func setDeliverabilityRates(sent int64, deliverability *domain.ReportDeliverability) {
	if sent == 0 {
		return
	}
	deliverability.BounceRate = float64(deliverability.Bounced.Total) / float64(sent)
	deliverability.ComplaintRate = float64(deliverability.Complained.Total) / float64(sent)
}
