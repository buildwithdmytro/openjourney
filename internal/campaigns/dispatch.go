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

type ManifestRecipient struct {
	ProfileID string `json:"profile_id"`
	Endpoint  string `json:"endpoint"`
}

func DispatchNext(ctx context.Context, store ports.Store, blobStore BlobStore) (processed bool, err error) {
	camp, found, err := store.ClaimScheduledCampaign(ctx)
	if err != nil {
		return false, fmt.Errorf("claim scheduled campaign: %w", err)
	}
	if !found {
		return false, nil
	}

	slog.Info("dispatching campaign", "campaign_id", camp.ID, "tenant_id", camp.TenantID)

	p := domain.Principal{
		TenantID:    camp.TenantID,
		WorkspaceID: camp.WorkspaceID,
		AppID:       "app-1",
		Scopes:      []string{"*"},
	}
	success := false
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("dispatch campaign %s panic: %v", camp.ID, recovered)
			processed = true
		}
		if !success {
			camp.Status = "failed"
			if _, updateErr := store.UpdateCampaign(ctx, p, camp); updateErr != nil {
				slog.Error("failed to mark campaign as failed", "campaign_id", camp.ID, "error", updateErr)
				if err == nil {
					err = updateErr
				}
			}
		}
	}()

	// In background dispatcher, retrieve any application ID belonging to this workspace to evaluate consent correctly.
	appID, err := store.GetFirstAppID(ctx, camp.TenantID, camp.WorkspaceID)
	if err != nil {
		slog.Warn("could not retrieve app ID for workspace, falling back to app-1", "error", err, "tenant_id", camp.TenantID)
		appID = "app-1"
	}

	p.AppID = appID

	// 1. Resolve Segment profiles
	profileIDs, err := store.ResolveSegment(ctx, p, camp.SegmentID)
	if err != nil {
		return true, fmt.Errorf("resolve segment: %w", err)
	}

	template, err := store.GetTemplate(ctx, p, camp.TemplateID)
	if err != nil {
		return true, fmt.Errorf("get template: %w", err)
	}

	// 2. Fetch profiles' endpoints based on channel
	var recipients []domain.Recipient
	if template.Channel == "sms" {
		profilePhones, err := store.GetProfilePhones(ctx, camp.TenantID, profileIDs)
		if err != nil {
			return true, fmt.Errorf("get profile phones: %w", err)
		}
		for _, pID := range profileIDs {
			if phone, exists := profilePhones[pID]; exists && phone != "" {
				recipients = append(recipients, domain.Recipient{
					ProfileID: pID,
					Endpoint:  phone,
				})
			}
		}
	} else if template.Channel == "push" {
		for _, pID := range profileIDs {
			tokens, err := store.ListActiveDeviceTokens(ctx, camp.TenantID, camp.WorkspaceID, pID)
			if err != nil {
				slog.Error("failed to list active device tokens for profile in dispatch", "error", err, "profile_id", pID)
				continue
			}
			for _, tok := range tokens {
				recipients = append(recipients, domain.Recipient{
					ProfileID: pID,
					Endpoint:  tok.Token,
				})
			}
		}
	} else if template.Channel == "in_app" {
		for _, pID := range profileIDs {
			recipients = append(recipients, domain.Recipient{
				ProfileID: pID,
				Endpoint:  pID,
			})
		}
	} else {
		profileEmails, err := store.GetProfileEmails(ctx, camp.TenantID, profileIDs)
		if err != nil {
			return true, fmt.Errorf("get profile emails: %w", err)
		}
		for _, pID := range profileIDs {
			if email, exists := profileEmails[pID]; exists && email != "" {
				recipients = append(recipients, domain.Recipient{
					ProfileID: pID,
					Endpoint:  email,
				})
			}
		}
	}

	// 3. Compile the segment DSL for the manifest
	seg, err := store.GetSegment(ctx, p, camp.SegmentID)
	if err != nil {
		return true, fmt.Errorf("get segment: %w", err)
	}

	var conversionGoal json.RawMessage
	var attributionWindow string
	if camp.ExperimentID != nil {
		exp, err := store.GetExperiment(ctx, p, *camp.ExperimentID)
		if err != nil {
			return true, fmt.Errorf("get campaign experiment goal: %w", err)
		}
		conversionGoal = append(json.RawMessage(nil), exp.PrimaryGoal...)
		if len(conversionGoal) > 0 {
			var goal struct {
				Window string `json:"window"`
			}
			if err := json.Unmarshal(conversionGoal, &goal); err != nil {
				return true, fmt.Errorf("decode campaign conversion goal: %w", err)
			}
			attributionWindow = goal.Window
		}
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

	profileLegs, consentLegs, clickhouseLegs := compileSegmentLegs(node)

	manifestRecipients := make([]ManifestRecipient, len(recipients))
	for i, r := range recipients {
		manifestRecipients[i] = ManifestRecipient{
			ProfileID: r.ProfileID,
			Endpoint:  r.Endpoint,
		}
	}

	manifest := struct {
		SegmentID              string              `json:"segment_id"`
		SegmentVersion         int                 `json:"segment_version"`
		DSL                    string              `json:"dsl"`
		CompiledSQL            string              `json:"compiled_sql"`
		CompiledProfileLegs    []string            `json:"compiled_profile_legs"`
		CompiledConsentLegs    []string            `json:"compiled_consent_legs"`
		CompiledClickHouseLegs []string            `json:"compiled_clickhouse_legs"`
		TemplateID             string              `json:"template_id"`
		TemplateVersion        int                 `json:"template_version"`
		ConversionGoal         json.RawMessage     `json:"conversion_goal,omitempty"`
		AttributionWindow      string              `json:"attribution_window,omitempty"`
		EvaluatedAt            time.Time           `json:"evaluated_at"`
		Recipients             []ManifestRecipient `json:"recipients"`
	}{
		SegmentID:              camp.SegmentID,
		SegmentVersion:         seg.Version,
		DSL:                    dslStr,
		CompiledSQL:            compiledSQL,
		CompiledProfileLegs:    profileLegs,
		CompiledConsentLegs:    consentLegs,
		CompiledClickHouseLegs: clickhouseLegs,
		TemplateID:             camp.TemplateID,
		TemplateVersion:        template.Version,
		ConversionGoal:         conversionGoal,
		AttributionWindow:      attributionWindow,
		EvaluatedAt:            time.Now().UTC(),
		Recipients:             manifestRecipients,
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

	err = store.SaveCampaignManifestAndJobs(ctx, camp.ID, manifestKey, len(recipients), seg.Version, template.Version, conversionGoal, attributionWindow, jobs)
	if err != nil {
		return true, fmt.Errorf("save manifest and jobs: %w", err)
	}

	slog.Info("campaign dispatched successfully", "campaign_id", camp.ID, "recipients_count", len(recipients), "shards_count", len(jobs))
	success = true
	return true, nil
}

func compileSegmentLegs(n audience.Node) ([]string, []string, []string) {
	var profiles []string
	var consents []string
	var clickhouses []string

	if n == nil {
		return nil, nil, nil
	}

	switch nodeType := n.(type) {
	case *audience.And:
		for _, cond := range nodeType.Conditions {
			p, c, ch := compileSegmentLegs(cond)
			profiles = append(profiles, p...)
			consents = append(consents, c...)
			clickhouses = append(clickhouses, ch...)
		}
	case *audience.Or:
		for _, cond := range nodeType.Conditions {
			p, c, ch := compileSegmentLegs(cond)
			profiles = append(profiles, p...)
			consents = append(consents, c...)
			clickhouses = append(clickhouses, ch...)
		}
	case *audience.Not:
		p, c, ch := compileSegmentLegs(nodeType.Condition)
		profiles = append(profiles, p...)
		consents = append(consents, c...)
		clickhouses = append(clickhouses, ch...)
	case *audience.ProfileAttribute:
		if sql, _, err := audience.CompileProfile(nodeType); err == nil {
			profiles = append(profiles, sql)
		}
	case *audience.Consent:
		sql, _ := audience.CompileConsent(nodeType, "", "")
		consents = append(consents, sql)
	case *audience.EventHistory:
		sql, _ := audience.CompileClickHouse(nodeType, "")
		clickhouses = append(clickhouses, sql)
	}

	return profiles, consents, clickhouses
}

// RedispatchFromManifest reads the campaign's manifest file from BlobStore,
// decodes the frozen recipient list, and Recreates the sharded delivery jobs.
func RedispatchFromManifest(ctx context.Context, store ports.Store, blobStore BlobStore, tenantID, campaignID string) ([]domain.Recipient, error) {
	camp, err := store.GetCampaignSystem(ctx, tenantID, campaignID)
	if err != nil {
		return nil, fmt.Errorf("get campaign: %w", err)
	}
	if camp.ManifestKey == nil || *camp.ManifestKey == "" {
		return nil, fmt.Errorf("campaign manifest key is not set")
	}

	manifestData, err := blobStore.Get(ctx, *camp.ManifestKey)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest from blob store: %w", err)
	}

	var manifest struct {
		CampaignID        string              `json:"campaign_id"`
		SegmentID         string              `json:"segment_id"`
		SegmentVersion    int                 `json:"segment_version"`
		TemplateVersion   int                 `json:"template_version"`
		ConversionGoal    json.RawMessage     `json:"conversion_goal"`
		AttributionWindow string              `json:"attribution_window"`
		Recipients        []ManifestRecipient `json:"recipients"`
	}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("unmarshal manifest: %w", err)
	}

	recipients := make([]domain.Recipient, len(manifest.Recipients))
	for i, r := range manifest.Recipients {
		recipients[i] = domain.Recipient{
			ProfileID: r.ProfileID,
			Endpoint:  r.Endpoint,
		}
	}

	// Shard recipients into delivery jobs
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

	err = store.SaveCampaignManifestAndJobs(ctx, camp.ID, *camp.ManifestKey, len(recipients), manifest.SegmentVersion, manifest.TemplateVersion, manifest.ConversionGoal, manifest.AttributionWindow, jobs)
	if err != nil {
		return nil, fmt.Errorf("save manifest and jobs: %w", err)
	}

	return recipients, nil
}
