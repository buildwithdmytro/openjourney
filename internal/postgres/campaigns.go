package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Store) CreateCampaign(ctx context.Context, p domain.Principal, c domain.Campaign) (domain.Campaign, error) {
	if c.Name == "" {
		return domain.Campaign{}, errors.New("campaign name is required")
	}
	if c.SegmentID == "" {
		return domain.Campaign{}, errors.New("segment_id is required")
	}
	if c.TemplateID == "" {
		return domain.Campaign{}, errors.New("template_id is required")
	}
	if c.Status == "" {
		c.Status = "draft"
	}

	var out domain.Campaign
	err := s.pool.QueryRow(ctx, `INSERT INTO campaigns (tenant_id, workspace_id, name, description, segment_id, template_id, status, scheduled_at, segment_version, template_version, recipient_count)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id, tenant_id, workspace_id, name, description, segment_id, template_id, status, scheduled_at, manifest_key, segment_version, template_version, evaluated_at, recipient_count, created_at, updated_at`,
		p.TenantID, p.WorkspaceID, c.Name, c.Description, c.SegmentID, c.TemplateID, c.Status, c.ScheduledAt, c.SegmentVersion, c.TemplateVersion, c.RecipientCount).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Description, &out.SegmentID, &out.TemplateID, &out.Status, &out.ScheduledAt, &out.ManifestKey, &out.SegmentVersion, &out.TemplateVersion, &out.EvaluatedAt, &out.RecipientCount, &out.CreatedAt, &out.UpdatedAt)
	if err != nil {
		return domain.Campaign{}, err
	}

	_ = s.audit(ctx, p, "campaign.create", "campaign", out.ID, map[string]any{"name": out.Name})
	return out, nil
}

func (s *Store) GetCampaign(ctx context.Context, p domain.Principal, id string) (domain.Campaign, error) {
	var out domain.Campaign
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, name, description, segment_id, template_id, status, scheduled_at, manifest_key, segment_version, template_version, evaluated_at, recipient_count, created_at, updated_at
		FROM campaigns WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3`,
		p.TenantID, p.WorkspaceID, id).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Description, &out.SegmentID, &out.TemplateID, &out.Status, &out.ScheduledAt, &out.ManifestKey, &out.SegmentVersion, &out.TemplateVersion, &out.EvaluatedAt, &out.RecipientCount, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Campaign{}, ErrNotFound
	}
	return out, err
}

func (s *Store) GetCampaignSystem(ctx context.Context, tenantID, id string) (domain.Campaign, error) {
	var out domain.Campaign
	err := s.pool.QueryRow(ctx, `SELECT id, tenant_id, workspace_id, name, description, segment_id, template_id, status, scheduled_at, manifest_key, segment_version, template_version, evaluated_at, recipient_count, created_at, updated_at
		FROM campaigns WHERE tenant_id=$1 AND id=$2`,
		tenantID, id).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Description, &out.SegmentID, &out.TemplateID, &out.Status, &out.ScheduledAt, &out.ManifestKey, &out.SegmentVersion, &out.TemplateVersion, &out.EvaluatedAt, &out.RecipientCount, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Campaign{}, ErrNotFound
	}
	return out, err
}

func (s *Store) UpdateCampaign(ctx context.Context, p domain.Principal, c domain.Campaign) (domain.Campaign, error) {
	var out domain.Campaign
	err := s.pool.QueryRow(ctx, `UPDATE campaigns SET name=$4, description=$5, segment_id=$6, template_id=$7, status=$8, scheduled_at=$9, manifest_key=$10, segment_version=$11, template_version=$12, evaluated_at=$13, recipient_count=$14, updated_at=now()
		WHERE tenant_id=$1 AND workspace_id=$2 AND id=$3
		RETURNING id, tenant_id, workspace_id, name, description, segment_id, template_id, status, scheduled_at, manifest_key, segment_version, template_version, evaluated_at, recipient_count, created_at, updated_at`,
		p.TenantID, p.WorkspaceID, c.ID, c.Name, c.Description, c.SegmentID, c.TemplateID, c.Status, c.ScheduledAt, c.ManifestKey, c.SegmentVersion, c.TemplateVersion, c.EvaluatedAt, c.RecipientCount).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Description, &out.SegmentID, &out.TemplateID, &out.Status, &out.ScheduledAt, &out.ManifestKey, &out.SegmentVersion, &out.TemplateVersion, &out.EvaluatedAt, &out.RecipientCount, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Campaign{}, ErrNotFound
	}
	if err != nil {
		return domain.Campaign{}, err
	}

	_ = s.audit(ctx, p, "campaign.update", "campaign", out.ID, map[string]any{"status": out.Status})
	return out, nil
}

func (s *Store) ListCampaigns(ctx context.Context, p domain.Principal) ([]domain.Campaign, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, tenant_id, workspace_id, name, description, segment_id, template_id, status, scheduled_at, manifest_key, segment_version, template_version, evaluated_at, recipient_count, created_at, updated_at
		FROM campaigns WHERE tenant_id=$1 AND workspace_id=$2 ORDER BY created_at DESC`,
		p.TenantID, p.WorkspaceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Campaign
	for rows.Next() {
		var c domain.Campaign
		err := rows.Scan(&c.ID, &c.TenantID, &c.WorkspaceID, &c.Name, &c.Description, &c.SegmentID, &c.TemplateID, &c.Status, &c.ScheduledAt, &c.ManifestKey, &c.SegmentVersion, &c.TemplateVersion, &c.EvaluatedAt, &c.RecipientCount, &c.CreatedAt, &c.UpdatedAt)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) ClaimScheduledCampaign(ctx context.Context) (domain.Campaign, bool, error) {
	var out domain.Campaign
	err := s.pool.QueryRow(ctx, `UPDATE campaigns SET status='building', updated_at=now()
		WHERE id = (
			SELECT id FROM campaigns
			WHERE status='scheduled' AND (scheduled_at IS NULL OR scheduled_at <= now())
			ORDER BY scheduled_at ASC NULLS LAST
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, tenant_id, workspace_id, name, description, segment_id, template_id, status, scheduled_at, manifest_key, segment_version, template_version, evaluated_at, recipient_count, created_at, updated_at`).
		Scan(&out.ID, &out.TenantID, &out.WorkspaceID, &out.Name, &out.Description, &out.SegmentID, &out.TemplateID, &out.Status, &out.ScheduledAt, &out.ManifestKey, &out.SegmentVersion, &out.TemplateVersion, &out.EvaluatedAt, &out.RecipientCount, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Campaign{}, false, nil
	}
	if err != nil {
		return domain.Campaign{}, false, err
	}
	return out, true, nil
}

func (s *Store) SaveCampaignManifestAndJobs(ctx context.Context, campaignID string, manifestKey string, recipientCount int, segmentVersion int, templateVersion int, jobs []domain.DeliveryJob) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	_, err = tx.Exec(ctx, `UPDATE campaigns SET status='sending', manifest_key=$1, recipient_count=$2, segment_version=$3, template_version=$4, evaluated_at=now(), updated_at=now() WHERE id=$5`,
		manifestKey, recipientCount, segmentVersion, templateVersion, campaignID)
	if err != nil {
		return err
	}

	for _, job := range jobs {
		recipientsJSON, err := json.Marshal(job.Recipients)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO delivery_jobs (campaign_id, tenant_id, shard, status, recipients) VALUES ($1, $2, $3, $4, $5)`,
			campaignID, job.TenantID, job.Shard, "pending", recipientsJSON)
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *Store) ClaimDeliveryJob(ctx context.Context, workerID string) (domain.DeliveryJob, bool, error) {
	var out domain.DeliveryJob
	var recipientsBytes []byte
	// Claim next available job: either pending or failed with attempts < 3,
	// or processing but lease expired (locked_until <= now()).
	err := s.pool.QueryRow(ctx, `UPDATE delivery_jobs SET
			status='processing',
			attempts=attempts+1,
			locked_until=now() + INTERVAL '5 minutes',
			updated_at=now()
		WHERE id = (
			SELECT id FROM delivery_jobs
			WHERE (
				(status IN ('pending', 'failed') AND attempts < 3 AND available_at <= now())
				OR (status='processing' AND locked_until <= now())
			)
			ORDER BY available_at ASC
			FOR UPDATE SKIP LOCKED
			LIMIT 1
		)
		RETURNING id, campaign_id, tenant_id, shard, status, recipients, attempts, available_at, locked_until, error_message, created_at, updated_at`).
		Scan(&out.ID, &out.CampaignID, &out.TenantID, &out.Shard, &out.Status, &recipientsBytes, &out.Attempts, &out.AvailableAt, &out.LockedUntil, &out.ErrorMessage, &out.CreatedAt, &out.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.DeliveryJob{}, false, nil
	}
	if err != nil {
		return domain.DeliveryJob{}, false, err
	}

	if len(recipientsBytes) > 0 {
		_ = json.Unmarshal(recipientsBytes, &out.Recipients)
	}
	return out, true, nil
}

func (s *Store) CompleteDeliveryJob(ctx context.Context, jobID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var campaignID string
	err = tx.QueryRow(ctx, `UPDATE delivery_jobs SET status='completed', locked_until=NULL, updated_at=now() WHERE id=$1 RETURNING campaign_id`, jobID).Scan(&campaignID)
	if err != nil {
		return err
	}

	var activeCount int
	err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM delivery_jobs WHERE campaign_id=$1 AND (status IN ('pending', 'processing') OR (status='failed' AND attempts < 3))`, campaignID).Scan(&activeCount)
	if err != nil {
		return err
	}

	if activeCount == 0 {
		var failedCount int
		err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM delivery_jobs WHERE campaign_id=$1 AND status IN ('failed', 'dead')`, campaignID).Scan(&failedCount)
		if err != nil {
			return err
		}

		if failedCount > 0 {
			_, err = tx.Exec(ctx, `UPDATE campaigns SET status='failed', updated_at=now() WHERE id=$1`, campaignID)
		} else {
			_, err = tx.Exec(ctx, `UPDATE campaigns SET status='completed', updated_at=now() WHERE id=$1`, campaignID)
		}
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *Store) FailDeliveryJob(ctx context.Context, jobID string, errMsg string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var campaignID string
	var attempts int
	var status string
	err = tx.QueryRow(ctx, `UPDATE delivery_jobs SET
			status=CASE WHEN attempts >= 3 THEN 'dead'::text ELSE 'failed'::text END,
			error_message=$2,
			available_at=now() + INTERVAL '1 minute',
			locked_until=NULL,
			updated_at=now()
		WHERE id=$1
		RETURNING campaign_id, attempts, status`, jobID, errMsg).Scan(&campaignID, &attempts, &status)
	if err != nil {
		return err
	}

	var activeCount int
	err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM delivery_jobs WHERE campaign_id=$1 AND (status IN ('pending', 'processing') OR (status='failed' AND attempts < 3))`, campaignID).Scan(&activeCount)
	if err != nil {
		return err
	}

	if activeCount == 0 {
		var failedCount int
		err = tx.QueryRow(ctx, `SELECT COUNT(*) FROM delivery_jobs WHERE campaign_id=$1 AND status IN ('failed', 'dead')`, campaignID).Scan(&failedCount)
		if err != nil {
			return err
		}

		if failedCount > 0 {
			_, err = tx.Exec(ctx, `UPDATE campaigns SET status='failed', updated_at=now() WHERE id=$1`, campaignID)
		} else {
			_, err = tx.Exec(ctx, `UPDATE campaigns SET status='completed', updated_at=now() WHERE id=$1`, campaignID)
		}
		if err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *Store) CreateDeliveryAttempt(ctx context.Context, attempt domain.DeliveryAttempt) (bool, error) {
	if attempt.TenantID == "" {
		return false, errors.New("tenant_id is required for delivery attempt")
	}
	if attempt.AttemptedAt.IsZero() {
		attempt.AttemptedAt = time.Now().UTC()
	}
	policySnapshot := attempt.PolicySnapshot
	if len(policySnapshot) == 0 {
		policySnapshot = []byte("{}")
	}
	res, err := s.pool.Exec(ctx, `INSERT INTO delivery_attempts (campaign_id, tenant_id, profile_id, channel, endpoint, decision, reason, provider_message_id, policy_snapshot, attempted_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (campaign_id, profile_id, channel) DO NOTHING`,
		attempt.CampaignID, attempt.TenantID, attempt.ProfileID, attempt.Channel, attempt.Endpoint, attempt.Decision, attempt.Reason, attempt.ProviderMessageID, policySnapshot, attempt.AttemptedAt)
	if err != nil {
		return false, err
	}
	return res.RowsAffected() > 0, nil
}

func (s *Store) UpdateDeliveryAttempt(ctx context.Context, campaignID, profileID, channel, decision, reason, providerMsgID string, policySnapshot []byte) error {
	if len(policySnapshot) == 0 {
		policySnapshot = []byte("{}")
	}
	_, err := s.pool.Exec(ctx, `UPDATE delivery_attempts SET decision=$4, reason=$5, provider_message_id=$6, policy_snapshot=$7 WHERE campaign_id=$1 AND profile_id=$2 AND channel=$3`,
		campaignID, profileID, channel, decision, reason, providerMsgID, policySnapshot)
	return err
}

func (s *Store) DeleteDeliveryAttempt(ctx context.Context, tenantID, campaignID, profileID, channel string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM delivery_attempts WHERE tenant_id=$1 AND campaign_id=$2 AND profile_id=$3 AND channel=$4`,
		tenantID, campaignID, profileID, channel)
	return err
}

func (s *Store) GetProfileEmails(ctx context.Context, tenantID string, profileIDs []string) (map[string]string, error) {
	if len(profileIDs) == 0 {
		return map[string]string{}, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT id, attributes->>'email' FROM profiles WHERE tenant_id=$1 AND id=ANY($2)`, tenantID, profileIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var id string
		var email *string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		if email != nil && *email != "" {
			out[id] = *email
		}
	}
	return out, rows.Err()
}

func (s *Store) GetFirstAppID(ctx context.Context, tenantID, workspaceID string) (string, error) {
	var appID string
	err := s.pool.QueryRow(ctx, `SELECT id FROM applications WHERE tenant_id = $1 AND workspace_id = $2 LIMIT 1`, tenantID, workspaceID).Scan(&appID)
	if err != nil {
		return "", err
	}
	return appID, nil
}


