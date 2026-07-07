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
	"github.com/buildwithdmytro/openjourney/internal/policy"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/render"
)

type Config struct {
	TrackingSecretKey []byte
	TrackingBaseURL   string
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

	p := domain.Principal{
		TenantID:    job.TenantID,
		WorkspaceID: "workspace-1", // default fallback
		AppID:       "system",
		Scopes:      []string{"*"},
	}

	// Retrieve actual workspace ID and AppID from campaign
	camp, err := store.GetCampaign(ctx, p, job.CampaignID)
	if err != nil {
		slog.Error("failed to get campaign for job", "error", err, "campaign_id", job.CampaignID)
		_ = store.FailDeliveryJob(ctx, job.ID, fmt.Sprintf("failed to get campaign: %v", err))
		return true, nil
	}

	p.WorkspaceID = camp.WorkspaceID
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

	// Load Sending Identity
	var identity domain.SendingIdentity
	if template.SendingIdentityID != nil && *template.SendingIdentityID != "" {
		identity, err = store.GetSendingIdentity(ctx, p, *template.SendingIdentityID)
		if err != nil {
			slog.Error("failed to get sending identity", "error", err, "sending_identity_id", *template.SendingIdentityID)
			_ = store.FailDeliveryJob(ctx, job.ID, fmt.Sprintf("failed to get sending identity: %v", err))
			return true, nil
		}
	} else {
		identity = domain.SendingIdentity{
			Channel:     "email",
			Provider:    "fake",
			MaxSendRate: 10,
		}
	}

	// Resolve appropriate channel adapter
	var adapter ports.ChannelAdapter
	switch identity.Provider {
	case "ses":
		adapter = channels.NewSESAdapter()
	case "webhook":
		adapter = channels.NewWebhookAdapter()
	case "fake", "":
		adapter = channels.NewFakeAdapter()
	default:
		adapter = channels.NewFakeAdapter()
	}

	// Fatigue checks caps
	caps := policy.Caps{
		Channel:     template.Channel,
		Topic:       "marketing",
		MaxSends24h: 5,
		MaxSends7d:  20,
	}

	// Process recipients
	for _, rec := range job.Recipients {
		attempt := domain.DeliveryAttempt{
			CampaignID: camp.ID,
			ProfileID:  rec.ProfileID,
			Channel:    template.Channel,
			Endpoint:   rec.Endpoint,
			Decision:   "failed",
		}
		inserted, err := store.CreateDeliveryAttempt(ctx, attempt)
		if err != nil {
			slog.Error("failed to create delivery attempt", "error", err, "campaign_id", camp.ID, "profile_id", rec.ProfileID)
			continue
		}
		if !inserted {
			// Already handled or currently being processed (effectively-once skip)
			continue
		}

		prof, err := store.GetProfileByID(ctx, camp.TenantID, p.AppID, rec.ProfileID)
		if err != nil {
			slog.Error("failed to get profile", "error", err, "profile_id", rec.ProfileID)
			_ = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, "failed", fmt.Sprintf("failed to load profile: %v", err), "")
			continue
		}

		policyRec := policy.Recipient{
			ProfileID:  rec.ProfileID,
			ExternalID: prof.ExternalID,
			Endpoint:   rec.Endpoint,
		}

		verdict := policy.Evaluate(ctx, store, p, policyRec, caps)
		if verdict.Decision != "sent" {
			err = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, verdict.Decision, verdict.Reason, "")
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
				_ = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, "failed", fmt.Sprintf("subject render error: %v", err), "")
				continue
			}
		}

		var htmlBody string
		if template.HTMLTemplate != nil && *template.HTMLTemplate != "" {
			htmlBody, err = render.Render(*template.HTMLTemplate, vars)
			if err != nil {
				slog.Error("failed to render HTML template", "error", err)
				_ = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, "failed", fmt.Sprintf("html render error: %v", err), "")
				continue
			}
		}

		var textBody string
		if template.TextTemplate != nil && *template.TextTemplate != "" {
			textBody, err = render.Render(*template.TextTemplate, vars)
			if err != nil {
				slog.Error("failed to render text template", "error", err)
				_ = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, "failed", fmt.Sprintf("text render error: %v", err), "")
				continue
			}
		}

		var bodyPayload string
		if template.BodyTemplate != nil && *template.BodyTemplate != "" {
			bodyPayload, err = render.Render(*template.BodyTemplate, vars)
			if err != nil {
				slog.Error("failed to render body template", "error", err)
				_ = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, "failed", fmt.Sprintf("body render error: %v", err), "")
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

		msg := ports.RenderedMessage{
			Channel:  template.Channel,
			Endpoint: rec.Endpoint,
			Subject:  subject,
			HTML:     htmlBody,
			Text:     textBody,
			Body:     bodyPayload,
			Identity: identity,
		}

		providerMsgID, err := adapter.Send(ctx, msg)
		if err != nil {
			slog.Error("failed to send message via adapter", "error", err, "profile_id", rec.ProfileID)
			_ = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, "failed", fmt.Sprintf("adapter send error: %v", err), "")
			continue
		}

		err = store.UpdateDeliveryAttempt(ctx, camp.ID, rec.ProfileID, template.Channel, "sent", "eligible", providerMsgID)
		if err != nil {
			slog.Error("failed to update delivery attempt status to sent", "error", err)
		}

		eventPayload, _ := json.Marshal(map[string]any{
			"template_id": template.ID,
			"dispatch_id": camp.ID,
			"channel":     template.Channel,
		})
		emittedEvent := domain.Event{
			Type:           "email.sent",
			SchemaVersion:  1,
			ExternalID:     prof.ExternalID,
			AnonymousID:    prof.AnonymousID,
			IdempotencyKey: fmt.Sprintf("sent-%s-%s", camp.ID, rec.ProfileID),
			OccurredAt:     time.Now().UTC(),
			Payload:        eventPayload,
		}

		_, err = store.AcceptEvents(ctx, p, []domain.Event{emittedEvent})
		if err != nil {
			slog.Error("failed to emit email.sent event", "error", err, "profile_id", rec.ProfileID)
		}
	}

	err = store.CompleteDeliveryJob(ctx, job.ID)
	if err != nil {
		slog.Error("failed to complete delivery job", "error", err, "job_id", job.ID)
		return true, err
	}

	slog.Info("delivery job completed successfully", "job_id", job.ID)
	return true, nil
}
