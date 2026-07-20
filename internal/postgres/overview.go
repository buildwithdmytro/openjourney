package postgres

import (
	"context"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func (s *Store) GetOverview(ctx context.Context, p domain.Principal) (domain.Overview, error) {
	overview := domain.Overview{}

	err := s.pool.QueryRow(ctx, `
		SELECT
			(SELECT COUNT(*) FROM profiles WHERE tenant_id = $1 AND workspace_id = $2) AS profiles,
			(SELECT COUNT(*) FROM journeys WHERE tenant_id = $1 AND workspace_id = $2) AS journeys,
			(SELECT COUNT(*) FROM campaigns WHERE tenant_id = $1 AND workspace_id = $2) AS campaigns,
			(SELECT COUNT(*) FROM delivery_attempts WHERE tenant_id = $1 AND workspace_id = $2) AS delivery_attempts,
			(SELECT COUNT(*) FROM inapp_messages WHERE tenant_id = $1 AND workspace_id = $2) AS inapp_messages,
			(SELECT COUNT(*) FROM connector_runs WHERE tenant_id = $1 AND workspace_id = $2) AS connector_runs
	`, p.TenantID, p.WorkspaceID).
		Scan(
			&overview.Profiles,
			&overview.Journeys,
			&overview.Campaigns,
			&overview.DeliveryAttempts,
			&overview.InAppMessages,
			&overview.ConnectorRuns,
		)
	if err != nil {
		return domain.Overview{}, err
	}

	return overview, nil
}
