package campaigns

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	assignment "github.com/buildwithdmytro/openjourney/internal/experiment"
	"github.com/buildwithdmytro/openjourney/internal/policy"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/render"
	"github.com/buildwithdmytro/openjourney/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

type Config struct {
	TrackingSecretKey []byte
	TrackingBaseURL   string
	Adapter           ports.ChannelAdapter
	SESAdapter        ports.ChannelAdapter
	WebhookAdapter    ports.ChannelAdapter
	FakeAdapter       ports.ChannelAdapter
}

func DeliverNext(ctx context.Context, store ports.Store, workerID string, cfg Config) (bool, error) {
	job, found, err := store.ClaimDeliveryJob(ctx, workerID)
	if err != nil {
		return false, fmt.Errorf("claim delivery job: %w", err)
	}
	if !found {
		return false, nil
	}

	slog.Info("processing delivery job", "job_id", job.ID, "campaign_id", job.CampaignID, "tenant_id", job.TenantID)

	// Retrieve actual workspace ID and AppID from campaign safely using GetCampaignSystem
	camp, err := store.GetCampaignSystem(ctx, job.TenantID, job.CampaignID)
	if err != nil {
		slog.Error("failed to get campaign for job", "error", err, "campaign_id", job.CampaignID)
		_ = store.FailDeliveryJob(ctx, job.ID, fmt.Sprintf("failed to get campaign: %v", err))
		return true, nil
	}

	p := domain.Principal{
		TenantID:    camp.TenantID,
		WorkspaceID: camp.WorkspaceID,
		AppID:       "system",
		Scopes:      []string{"*"},
	}

	appID, err := store.GetFirstAppID(ctx, camp.TenantID, camp.WorkspaceID)
	if err != nil {
		appID = "app-1"
	}
	p.AppID = appID

	// Load Template
	template, err := store.GetTemplate(ctx, p, camp.TemplateID)
	if err != nil {
		slog.Error("failed to get template", "error", err, "template_id", camp.TemplateID)
		_ = store.FailDeliveryJob(ctx, job.ID, fmt.Sprintf("failed to get template: %v", err))
		return true, nil
	}

	// Fatigue checks caps from tenant_quotas configuration
	maxSends24h, maxSends7d, err := store.GetTenantFatigueQuotas(ctx, p)
	if err != nil {
		slog.Error("failed to get tenant fatigue quotas", "error", err, "tenant_id", p.TenantID)
		// fall back to default limits if we fail to fetch them
		maxSends24h = 5
		maxSends7d = 20
	}

	var exp domain.Experiment
	if camp.ExperimentID != nil && *camp.ExperimentID != "" {
		exp, err = store.GetExperiment(ctx, p, *camp.ExperimentID)
		if err != nil {
			_ = store.FailDeliveryJob(ctx, job.ID, fmt.Sprintf("failed to get experiment: %v", err))
			return true, nil
		}
	}

	var hasRetryableError bool
	var retryableErrMsg string

	// Process recipients
	baseTemplate := template
	for _, rec := range job.Recipients {
		selectedTemplate := baseTemplate
		variant := ""
		if camp.ExperimentID != nil && *camp.ExperimentID != "" {
			variants := make([]assignment.Variant, 0, len(exp.Variants))
			for _, candidate := range exp.Variants {
				variants = append(variants, assignment.Variant{Label: candidate.Label, Weight: candidate.Weight})
			}
			computed, _ := assignment.Assign(exp.Seed, rec.ProfileID, variants, exp.HoldoutPct)
			stored, assignErr := store.AssignExperiment(ctx, p, exp.ID, rec.ProfileID, computed)
			if assignErr != nil {
				slog.Error("failed to assign experiment", "error", assignErr, "profile_id", rec.ProfileID)
				continue
			}
			variant = stored.Variant
			variantTemplateOK := true
			for _, candidate := range exp.Variants {
				if candidate.Label == variant && candidate.TemplateID != nil && *candidate.TemplateID != "" {
					selectedTemplate, err = store.GetTemplate(ctx, p, *candidate.TemplateID)
					if err != nil {
						slog.Error("failed to get variant template", "error", err, "profile_id", rec.ProfileID)
						variantTemplateOK = false
					}
					break
				}
			}
			if !variantTemplateOK {
				continue
			}
		}

		var identity domain.SendingIdentity
		if selectedTemplate.SendingIdentityID != nil && *selectedTemplate.SendingIdentityID != "" {
			identity, err = store.GetSendingIdentity(ctx, p, *selectedTemplate.SendingIdentityID)
			if err != nil {
				slog.Error("failed to get sending identity", "error", err)
				continue
			}
		} else {
			identity = domain.SendingIdentity{Channel: "email", Provider: "fake", MaxSendRate: 10}
		}
		adapter := adapterFor(identity.Provider, cfg)
		template = selectedTemplate
		caps := policy.Caps{Channel: template.Channel, Topic: "marketing", MaxSends24h: maxSends24h, MaxSends7d: maxSends7d}
		attempt := domain.DeliveryAttempt{
			CampaignID:   camp.ID,
			TenantID:     camp.TenantID,
			ProfileID:    rec.ProfileID,
			Channel:      template.Channel,
			Endpoint:     rec.Endpoint,
			Decision:     "processing",
			ExperimentID: camp.ExperimentID,
			Variant:      variant,
		}
		inserted, err := store.CreateDeliveryAttempt(ctx, attempt)
		if err != nil {
			slog.Error("failed to create delivery attempt", "error", err, "campaign_id", camp.ID, "profile_id", rec.ProfileID)
			continue
		}

		var existing domain.DeliveryAttempt
		if !inserted {
			existing, err = store.GetDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel)
			if err != nil {
				slog.Error("failed to get existing delivery attempt", "error", err, "campaign_id", camp.ID, "profile_id", rec.ProfileID)
				continue
			}

			switch existing.Decision {
			case "sent", "suppressed", "no_consent", "fatigued", "render_failed", "send_failed", "failed":
				// Terminal state, skip
				continue
			case "processing", "provider_sent", "retryable_failed":
				// intermediate state, reconcile
				attempt = existing
			default:
				// other state, skip to be safe
				continue
			}
		}
		if camp.ExperimentID != nil && *camp.ExperimentID != "" {
			if err := store.SetDeliveryAttemptExperiment(ctx, camp.TenantID, camp.ID, rec.ProfileID, template.Channel, *camp.ExperimentID, variant); err != nil {
				slog.Error("failed to stamp experiment on delivery attempt", "error", err)
				continue
			}
			if variant == "holdout" {
				_ = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, "holdout", "experiment holdout", "", nil)
				continue
			}
		}

		prof, err := store.GetProfileByID(ctx, camp.TenantID, p.AppID, rec.ProfileID)
		if err != nil {
			slog.Error("failed to get profile", "error", err, "profile_id", rec.ProfileID)
			_ = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, "failed", fmt.Sprintf("failed to load profile: %v", err), "", nil)
			continue
		}

		policyRec := policy.Recipient{
			ProfileID:  rec.ProfileID,
			ExternalID: prof.ExternalID,
			Endpoint:   rec.Endpoint,
		}

		verdict := policy.Evaluate(ctx, store, p, policyRec, caps)
		var verdictSnapshotBytes []byte
		if len(verdict.Snapshot) > 0 {
			verdictSnapshotBytes, _ = json.Marshal(verdict.Snapshot)
		}
		if verdict.Decision != "sent" {
			telemetry.PolicyRejections.Add(ctx, 1, otelmetric.WithAttributes(
				attribute.String("decision", verdict.Decision),
				attribute.String("campaign_id", camp.ID),
				attribute.String("channel", template.Channel),
			))
			err = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, verdict.Decision, verdict.Reason, "", verdictSnapshotBytes)
			if err != nil {
				slog.Error("failed to update delivery attempt with policy rejection", "error", err)
			}
			continue
		}

		var vars map[string]any
		if len(prof.Attributes) > 0 {
			_ = json.Unmarshal(prof.Attributes, &vars)
		}

		subject := "Campaign"
		if template.SubjectTemplate != nil && *template.SubjectTemplate != "" {
			subject, err = render.Render(*template.SubjectTemplate, vars)
			if err != nil {
				slog.Error("failed to render subject template", "error", err)
				_ = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, "render_failed", fmt.Sprintf("subject render error: %v", err), "", nil)
				continue
			}
		}

		var htmlBody string
		if template.HTMLTemplate != nil && *template.HTMLTemplate != "" {
			htmlBody, err = render.Render(*template.HTMLTemplate, vars)
			if err != nil {
				slog.Error("failed to render HTML template", "error", err)
				_ = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, "render_failed", fmt.Sprintf("html render error: %v", err), "", nil)
				continue
			}
		}

		var textBody string
		if template.TextTemplate != nil && *template.TextTemplate != "" {
			textBody, err = render.Render(*template.TextTemplate, vars)
			if err != nil {
				slog.Error("failed to render text template", "error", err)
				_ = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, "render_failed", fmt.Sprintf("text render error: %v", err), "", nil)
				continue
			}
		}

		var bodyPayload string
		if template.BodyTemplate != nil && *template.BodyTemplate != "" {
			bodyPayload, err = render.Render(*template.BodyTemplate, vars)
			if err != nil {
				slog.Error("failed to render body template", "error", err)
				_ = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, "render_failed", fmt.Sprintf("body render error: %v", err), "", nil)
				continue
			}
		}

		if htmlBody != "" && len(cfg.TrackingSecretKey) > 0 {
			upsertLink := func(originalURL string) (string, error) {
				return store.UpsertTrackedLink(ctx, camp.TenantID, template.ID, originalURL)
			}
			rewritten, err := render.RewriteLinks(htmlBody, camp.TenantID, p.AppID, camp.ID, rec.ProfileID, template.ID, camp.ID, upsertLink, cfg.TrackingSecretKey, cfg.TrackingBaseURL)
			if err != nil {
				slog.Error("failed to rewrite links in HTML body", "error", err)
			} else {
				htmlBody = rewritten
			}

			openToken, err := render.SignOpenToken(camp.TenantID, p.AppID, camp.ID, rec.ProfileID, template.ID, camp.ID, cfg.TrackingSecretKey)
			if err == nil {
				trackingImg := fmt.Sprintf(`<img src="%s/o/%s" width="1" height="1" alt="" />`, strings.TrimSuffix(cfg.TrackingBaseURL, "/"), openToken)
				if strings.Contains(htmlBody, "</body>") {
					htmlBody = strings.Replace(htmlBody, "</body>", trackingImg+"</body>", 1)
				} else {
					htmlBody = htmlBody + trackingImg
				}
			}
		}

		var providerMsgID string
		if attempt.Decision == "provider_sent" {
			providerMsgID = attempt.ProviderMessageID
			slog.Info("reconciling previously sent message", "campaign_id", camp.ID, "profile_id", rec.ProfileID, "provider_msg_id", providerMsgID)
		} else {
			msg := ports.RenderedMessage{
				Channel:        template.Channel,
				Endpoint:       rec.Endpoint,
				Subject:        subject,
				HTML:           htmlBody,
				Text:           textBody,
				Body:           bodyPayload,
				Identity:       identity,
				IdempotencyKey: fmt.Sprintf("sent-%s-%s", camp.ID, rec.ProfileID),
			}

			providerMsgID, err = adapter.Send(ctx, msg)
			if err != nil {
				slog.Error("failed to send message via adapter", "error", err, "profile_id", rec.ProfileID)
				if channels.IsRetryableError(err) {
					updateErr := store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, "retryable_failed", fmt.Sprintf("transient send error: %v", err), "", nil)
					if updateErr != nil {
						slog.Error("failed to update delivery attempt on transient error", "error", updateErr)
					}
					hasRetryableError = true
					retryableErrMsg = err.Error()
				} else {
					_ = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, "send_failed", fmt.Sprintf("adapter send error: %v", err), "", nil)
				}
				continue
			}

			err = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, "provider_sent", "eligible", providerMsgID, verdictSnapshotBytes)
			if err != nil {
				slog.Error("failed to update delivery attempt status to provider_sent", "error", err)
			}
		}

		eventPayload, _ := json.Marshal(map[string]any{
			"campaign_id": camp.ID,
			"channel":     template.Channel,
			"endpoint":    rec.Endpoint,
			"experiment_id": func() string {
				if camp.ExperimentID != nil {
					return *camp.ExperimentID
				}
				return ""
			}(),
			"variant": variant,
		})
		emittedEvent := domain.Event{
			Type:           "message.sent",
			SchemaVersion:  1,
			ExternalID:     prof.ExternalID,
			AnonymousID:    prof.AnonymousID,
			IdempotencyKey: fmt.Sprintf("sent-%s-%s", camp.ID, rec.ProfileID),
			OccurredAt:     time.Now().UTC(),
			Payload:        eventPayload,
		}

		_, err = store.AcceptEvents(ctx, p, []domain.Event{emittedEvent})
		if err != nil {
			slog.Error("failed to emit message.sent event", "error", err, "profile_id", rec.ProfileID)
			continue
		}

		err = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, "sent", "eligible", providerMsgID, verdictSnapshotBytes)
		if err != nil {
			slog.Error("failed to update delivery attempt status to sent", "error", err)
		} else {
			telemetry.MessagesSent.Add(ctx, 1, otelmetric.WithAttributes(
				attribute.String("channel", template.Channel),
				attribute.String("campaign_id", camp.ID),
			))
		}
	}

	if hasRetryableError {
		err = store.FailDeliveryJob(ctx, job.ID, fmt.Sprintf("transient send failure: %s", retryableErrMsg))
		if err != nil {
			slog.Error("failed to fail delivery job on transient error", "error", err, "job_id", job.ID)
			return true, err
		}
		slog.Info("delivery job failed with transient error and marked for retry", "job_id", job.ID)
		return true, fmt.Errorf("transient delivery failure: %s", retryableErrMsg)
	}

	err = store.CompleteDeliveryJob(ctx, job.ID)
	if err != nil {
		slog.Error("failed to complete delivery job", "error", err, "job_id", job.ID)
		return true, err
	}

	slog.Info("delivery job completed successfully", "job_id", job.ID)
	return true, nil
}

func adapterFor(provider string, cfg Config) ports.ChannelAdapter {
	if cfg.Adapter != nil {
		return cfg.Adapter
	}
	switch provider {
	case "ses":
		if cfg.SESAdapter != nil {
			return cfg.SESAdapter
		}
		return channels.NewSESAdapter()
	case "webhook":
		if cfg.WebhookAdapter != nil {
			return cfg.WebhookAdapter
		}
		return channels.NewWebhookAdapter()
	default:
		if cfg.FakeAdapter != nil {
			return cfg.FakeAdapter
		}
		return channels.NewFakeAdapter()
	}
}
