package campaigns

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/audience"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type BlobStore interface {
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Get(ctx context.Context, key string) ([]byte, error)
}

func DispatchNext(ctx context.Context, store ports.Store, blobStore BlobStore) (bool, error) {
	camp, found, err := store.ClaimScheduledCampaign(ctx)
	if err != nil {
		return false, fmt.Errorf("claim scheduled campaign: %w", err)
	}
	if !found {
		return false, nil
	}

	slog.Info("dispatching campaign", "campaign_id", camp.ID, "tenant_id", camp.TenantID)

	// In background dispatcher, retrieve any application ID belonging to this workspace to evaluate consent correctly.
	appID, err := store.GetFirstAppID(ctx, camp.TenantID, camp.WorkspaceID)
	if err != nil {
		slog.Warn("could not retrieve app ID for workspace, falling back to app-1", "error", err, "tenant_id", camp.TenantID)
		appID = "app-1"
	}

	p := domain.Principal{
		TenantID:    camp.TenantID,
		WorkspaceID: camp.WorkspaceID,
		AppID:       appID,
		Scopes:      []string{"*"},
	}

	// 1. Resolve Segment profiles
	profileIDs, err := store.ResolveSegment(ctx, p, camp.SegmentID)
	if err != nil {
		return true, fmt.Errorf("resolve segment: %w", err)
	}

	// 2. Fetch profiles' emails to keep only profiles with an email endpoint
	profileEmails, err := store.GetProfileEmails(ctx, camp.TenantID, profileIDs)
	if err != nil {
		return true, fmt.Errorf("get profile emails: %w", err)
	}

	// Keep only profiles that have an email endpoint
	var recipients []domain.Recipient
	for _, pID := range profileIDs {
		if email, exists := profileEmails[pID]; exists && email != "" {
			recipients = append(recipients, domain.Recipient{
				ProfileID: pID,
				Endpoint:  email,
			})
		}
	}

	// 3. Compile the segment DSL for the manifest
	seg, err := store.GetSegment(ctx, p, camp.SegmentID)
	if err != nil {
		return true, fmt.Errorf("get segment: %w", err)
	}

	var dslStr string
	if len(seg.DSL) > 0 {
		dslStr = string(seg.DSL)
	}

	var compiledSQL string
	node, err := audience.Parse(seg.DSL)
	if err == nil {
		if sql, _, err := audience.CompileProfile(node); err == nil {
			compiledSQL = sql
		}
	}

	// Build Manifest
	type ManifestRecipient struct {
		ProfileID string `json:"profile_id"`
		Endpoint  string `json:"endpoint"`
	}

	manifestRecipients := make([]ManifestRecipient, len(recipients))
	for i, r := range recipients {
		manifestRecipients[i] = ManifestRecipient{
			ProfileID: r.ProfileID,
			Endpoint:  r.Endpoint,
		}
	}

	manifest := struct {
		SegmentID       string              `json:"segment_id"`
		SegmentVersion  int                 `json:"segment_version"`
		DSL             string              `json:"dsl"`
		CompiledSQL     string              `json:"compiled_sql"`
		TemplateID      string              `json:"template_id"`
		TemplateVersion int                 `json:"template_version"`
		EvaluatedAt     time.Time           `json:"evaluated_at"`
		Recipients      []ManifestRecipient `json:"recipients"`
	}{
		SegmentID:       camp.SegmentID,
		SegmentVersion:  camp.SegmentVersion,
		DSL:             dslStr,
		CompiledSQL:     compiledSQL,
		TemplateID:      camp.TemplateID,
		TemplateVersion: camp.TemplateVersion,
		EvaluatedAt:     time.Now().UTC(),
		Recipients:      manifestRecipients,
	}

	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return true, fmt.Errorf("marshal manifest: %w", err)
	}

	// Upload Manifest to MinIO
	manifestKey := fmt.Sprintf("manifests/%s/%s-manifest.json", camp.TenantID, camp.ID)
	err = blobStore.Put(ctx, manifestKey, manifestData, "application/json")
	if err != nil {
		return true, fmt.Errorf("upload manifest to blob store: %w", err)
	}

	// 4. Shard recipients into delivery jobs
	var jobs []domain.DeliveryJob
	shardSize := 500
	shardIndex := 0
	for i := 0; i < len(recipients); i += shardSize {
		end := i + shardSize
		if end > len(recipients) {
			end = len(recipients)
		}
		jobRecipients := make([]domain.Recipient, 0, end-i)
		for _, r := range recipients[i:end] {
			jobRecipients = append(jobRecipients, r)
		}
		jobs = append(jobs, domain.DeliveryJob{
			CampaignID: camp.ID,
			TenantID:   camp.TenantID,
			Shard:      shardIndex,
			Status:     "pending",
			Recipients: jobRecipients,
		})
		shardIndex++
	}

	err = store.SaveCampaignManifestAndJobs(ctx, camp.ID, manifestKey, len(recipients), jobs)
	if err != nil {
		return true, fmt.Errorf("save manifest and jobs: %w", err)
	}

	slog.Info("campaign dispatched successfully", "campaign_id", camp.ID, "recipients_count", len(recipients), "shards_count", len(jobs))
	return true, nil
}
