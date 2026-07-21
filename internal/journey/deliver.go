package journey

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/domain"
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
	// Registry is the preferred way to supply adapters (built once per process).
	// When set, adapter resolution delegates to Registry.For(provider).
	Registry *channels.Registry
	// The following fields are kept for backward compatibility with existing tests
	// that inject adapters directly. Registry takes precedence.
	Adapter        ports.ChannelAdapter
	SESAdapter     ports.ChannelAdapter
	WebhookAdapter ports.ChannelAdapter
	FakeAdapter    ports.ChannelAdapter
	Clock          Clock
}

func DeliverNext(ctx context.Context, store ports.Store, workerID string, cfg Config) (bool, error) {
	clk := cfg.Clock
	if clk == nil {
		clk = RealClock{}
	}

	intent, found, err := store.ClaimJourneyMessageIntent(ctx, workerID)
	if err != nil {
		return false, fmt.Errorf("claim journey message intent: %w", err)
	}
	if !found {
		return false, nil
	}

	slog.Info("processing journey message intent", "intent_id", intent.ID, "run_id", intent.RunID, "tenant_id", intent.TenantID)

	p := domain.Principal{
		TenantID:    intent.TenantID,
		WorkspaceID: intent.WorkspaceID,
		AppID:       "system",
		Scopes:      []string{"*"},
	}

	appID, err := store.GetFirstAppID(ctx, intent.TenantID, intent.WorkspaceID)
	if err != nil {
		slog.Error("failed to resolve deterministic app for journey event", "error", err, "intent_id", intent.ID)
		intent.Status = "failed"
		errMsg := fmt.Sprintf("failed to resolve app: %v", err)
		intent.ErrorMessage = &errMsg
		dec := "failed"
		intent.Decision = &dec
		_ = store.UpdateJourneyMessageIntent(ctx, intent)
		return true, nil
	}
	p.AppID = appID

	// Load Template
	template, err := store.GetTemplate(ctx, p, intent.TemplateID)
	if err != nil {
		slog.Error("failed to get template", "error", err, "template_id", intent.TemplateID)
		intent.Status = "failed"
		errMsg := fmt.Sprintf("failed to get template: %v", err)
		intent.ErrorMessage = &errMsg
		dec := "render_failed"
		intent.Decision = &dec
		_ = store.UpdateJourneyMessageIntent(ctx, intent)
		return true, nil
	}

	// Load Sending Identity
	var identity domain.SendingIdentity
	if template.Channel == "push" {
		activeTokens, err := store.ListActiveDeviceTokens(ctx, intent.TenantID, intent.WorkspaceID, intent.ProfileID)
		if err != nil {
			slog.Error("failed to list active device tokens for profile", "error", err, "profile_id", intent.ProfileID)
			intent.Status = "failed"
			errMsg := fmt.Sprintf("failed to list active device tokens: %v", err)
			intent.ErrorMessage = &errMsg
			dec := "failed"
			intent.Decision = &dec
			_ = store.UpdateJourneyMessageIntent(ctx, intent)
			return true, nil
		}
		var matchingToken *domain.DeviceToken
		for _, t := range activeTokens {
			if t.Token == intent.Endpoint {
				matchingToken = &t
				break
			}
		}
		if matchingToken == nil {
			slog.Warn("device token no longer active, skipping send", "token", intent.Endpoint, "profile_id", intent.ProfileID)
			intent.Status = "completed"
			dec := "failed"
			intent.Decision = &dec
			reason := "device token no longer active"
			intent.Reason = &reason
			_ = store.UpdateJourneyMessageIntent(ctx, intent)
			return true, nil
		}

		idents, err := store.ListSendingIdentities(ctx, p)
		if err == nil {
			for _, iden := range idents {
				if iden.Provider == matchingToken.Provider {
					identity = iden
					break
				}
			}
		}
		if identity.ID == "" {
			identity = domain.SendingIdentity{Channel: "push", Provider: matchingToken.Provider, MaxSendRate: 10}
		}
	} else if template.Channel == "in_app" {
		identity = domain.SendingIdentity{Channel: "in_app", Provider: "inapp", MaxSendRate: 10}
	} else {
		if template.SendingIdentityID != nil && *template.SendingIdentityID != "" {
			identity, err = store.GetSendingIdentity(ctx, p, *template.SendingIdentityID)
			if err != nil {
				slog.Error("failed to get sending identity", "error", err, "sending_identity_id", *template.SendingIdentityID)
				intent.Status = "failed"
				errMsg := fmt.Sprintf("failed to get sending identity: %v", err)
				intent.ErrorMessage = &errMsg
				dec := "render_failed"
				intent.Decision = &dec
				_ = store.UpdateJourneyMessageIntent(ctx, intent)
				return true, nil
			}
		} else {
			identity = domain.SendingIdentity{
				Channel:     template.Channel,
				Provider:    "fake",
				MaxSendRate: 10,
			}
		}
	}

	// Resolve appropriate channel adapter.
	// cfg.Adapter overrides everything (unit-test injection path).
	var adapter ports.ChannelAdapter
	if cfg.Adapter != nil {
		adapter = cfg.Adapter
	} else if cfg.Registry != nil {
		// Registry is the preferred production path.
		adapter = cfg.Registry.For(identity.Provider)
	} else {
		// Backward-compatible fallback for tests that set individual adapter fields.
		switch identity.Provider {
		case "ses":
			if cfg.SESAdapter != nil {
				adapter = cfg.SESAdapter
			} else {
				adapter = channels.NewSESAdapter()
			}
		case "webhook":
			if cfg.WebhookAdapter != nil {
				adapter = cfg.WebhookAdapter
			} else {
				adapter = channels.NewWebhookAdapter()
			}
		case "fake", "":
			if cfg.FakeAdapter != nil {
				adapter = cfg.FakeAdapter
			} else {
				adapter = channels.NewFakeAdapter()
			}
		default:
			if cfg.FakeAdapter != nil {
				adapter = cfg.FakeAdapter
			} else {
				adapter = channels.NewFakeAdapter()
			}
		}
	}

	// Fatigue checks caps from tenant_quotas configuration
	maxSends24h, maxSends7d, err := store.GetTenantFatigueQuotas(ctx, p)
	if err != nil {
		slog.Error("failed to get tenant fatigue quotas", "error", err, "tenant_id", p.TenantID)
		maxSends24h = 5
		maxSends7d = 20
	}

	caps := policy.Caps{
		Channel:     template.Channel,
		Topic:       "marketing",
		MaxSends24h: maxSends24h,
		MaxSends7d:  maxSends7d,
	}

	// Check if this intent has an existing, in-progress, or completed decision
	if intent.Decision != nil {
		switch *intent.Decision {
		case "sent", "suppressed", "no_consent", "fatigued", "render_failed", "send_failed", "failed":
			// Already terminal, mark completed to drain the queue
			intent.Status = "completed"
			_ = store.UpdateJourneyMessageIntent(ctx, intent)
			return true, nil
		case "provider_sent":
			// Reconcile previously sent message, avoid duplicate adapter calls!
			slog.Info("reconciling previously sent message", "intent_id", intent.ID, "run_id", intent.RunID, "provider_msg_id", *intent.ProviderMessageID)
			// Proceed to event emission
		}
	}

	prof, err := store.GetProfileByIDSystem(ctx, intent.TenantID, intent.WorkspaceID, intent.ProfileID)
	if err != nil {
		slog.Error("failed to get profile", "error", err, "profile_id", intent.ProfileID)
		intent.Status = "failed"
		errMsg := fmt.Sprintf("failed to load profile: %v", err)
		intent.ErrorMessage = &errMsg
		dec := "failed"
		intent.Decision = &dec
		_ = store.UpdateJourneyMessageIntent(ctx, intent)
		return true, nil
	}
	if intent.Decision != nil && *intent.Decision == "provider_sent" {
		goto emitSentEvent
	}

	{

		// Quiet hours check (marketing/non-transactional messages only)
		if !intent.Transactional {
			start, end, tz, err := store.GetTenantQuietHours(ctx, p)
			if err != nil {
				slog.Error("failed to get tenant quiet hours", "error", err, "tenant_id", p.TenantID)
			} else if start != nil && end != nil {
				inQuiet, nextOpen, err := IsInQuietHours(clk.Now(), prof, start, end, tz)
				if err != nil {
					slog.Error("failed to evaluate quiet hours", "error", err)
				} else if inQuiet {
					slog.Info("quiet hours active; rescheduling message intent", "intent_id", intent.ID, "next_open", nextOpen)
					intent.AvailableAt = nextOpen
					intent.Status = "pending"
					if intent.Attempts > 0 {
						intent.Attempts--
					}
					err = store.UpdateJourneyMessageIntent(ctx, intent)
					if err != nil {
						slog.Error("failed to reschedule intent for quiet hours", "error", err)
					}
					return true, nil
				}
			}
		}

		var verdict policy.Verdict
		if intent.Transactional {
			suppressed, err := store.IsSuppressed(ctx, p, template.Channel, intent.Endpoint)
			if err != nil {
				verdict = policy.Verdict{
					Decision: "send_failed",
					Reason:   fmt.Sprintf("failed to check suppression: %v", err),
				}
			} else if suppressed {
				verdict = policy.Verdict{
					Decision: "suppressed",
					Reason:   "endpoint is suppressed",
				}
			} else {
				verdict = policy.Verdict{
					Decision: "sent",
					Reason:   "eligible",
				}
			}
		} else {
			policyRec := policy.Recipient{
				ProfileID:  intent.ProfileID,
				ExternalID: prof.ExternalID,
				Endpoint:   intent.Endpoint,
			}
			verdict = policy.Evaluate(ctx, store, p, policyRec, caps)
		}

		var snapshotBytes []byte
		if len(verdict.Snapshot) > 0 {
			snapshotBytes, _ = json.Marshal(verdict.Snapshot)
			intent.PolicySnapshot = snapshotBytes
		}

		if verdict.Decision != "sent" {
			telemetry.PolicyRejections.Add(ctx, 1, otelmetric.WithAttributes(
				attribute.String("decision", verdict.Decision),
				attribute.String("channel", template.Channel),
			))
			telemetry.JourneyPolicyRejections.Add(ctx, 1, otelmetric.WithAttributes(
				attribute.String("tenant_id", intent.TenantID),
				attribute.String("journey_id", intent.JourneyID),
				attribute.String("decision", verdict.Decision),
				attribute.String("channel", template.Channel),
			))
			intent.Status = "completed"
			intent.Decision = &verdict.Decision
			intent.Reason = &verdict.Reason
			err = store.UpdateJourneyMessageIntent(ctx, intent)
			if err != nil {
				slog.Error("failed to update journey message intent with policy rejection", "error", err)
			}
			return true, nil
		}

		// Check if we need to render & send (if not already "provider_sent")
		var providerMsgID string
		var costMicros int64
		var hasRetryableError bool
		var retryableErrMsg string

		if intent.Decision != nil && *intent.Decision == "provider_sent" {
			if intent.ProviderMessageID != nil {
				providerMsgID = *intent.ProviderMessageID
			}
		} else {
			var vars map[string]any
			if len(prof.Attributes) > 0 {
				_ = json.Unmarshal(prof.Attributes, &vars)
			}

			subject := "Journey Message"
			if template.SubjectTemplate != nil && *template.SubjectTemplate != "" {
				deps := render.RenderDeps{Store: store, Principal: p, Fetcher: nil}
				subject, err = render.RenderWithContext(ctx, *template.SubjectTemplate, vars, deps)
				if err != nil {
					slog.Error("failed to render subject template", "error", err)
					intent.Status = "completed"
					dec := "render_failed"
					intent.Decision = &dec
					reason := fmt.Sprintf("subject render error: %v", err)
					intent.Reason = &reason
					_ = store.UpdateJourneyMessageIntent(ctx, intent)
					return true, nil
				}
			}

			var htmlBody string
			if template.HTMLTemplate != nil && *template.HTMLTemplate != "" {
				deps := render.RenderDeps{Store: store, Principal: p, Fetcher: nil}
				htmlBody, err = render.RenderWithContext(ctx, *template.HTMLTemplate, vars, deps)
				if err != nil {
					slog.Error("failed to render HTML template", "error", err)
					intent.Status = "completed"
					dec := "render_failed"
					intent.Decision = &dec
					reason := fmt.Sprintf("html render error: %v", err)
					intent.Reason = &reason
					_ = store.UpdateJourneyMessageIntent(ctx, intent)
					return true, nil
				}
			}

			var textBody string
			if template.TextTemplate != nil && *template.TextTemplate != "" {
				deps := render.RenderDeps{Store: store, Principal: p, Fetcher: nil}
				textBody, err = render.RenderWithContext(ctx, *template.TextTemplate, vars, deps)
				if err != nil {
					slog.Error("failed to render text template", "error", err)
					intent.Status = "completed"
					dec := "render_failed"
					intent.Decision = &dec
					reason := fmt.Sprintf("text render error: %v", err)
					intent.Reason = &reason
					_ = store.UpdateJourneyMessageIntent(ctx, intent)
					return true, nil
				}
			}

			var bodyPayload string
			if template.BodyTemplate != nil && *template.BodyTemplate != "" {
				deps := render.RenderDeps{Store: store, Principal: p, Fetcher: nil}
				bodyPayload, err = render.RenderWithContext(ctx, *template.BodyTemplate, vars, deps)
				if err != nil {
					slog.Error("failed to render body template", "error", err)
					intent.Status = "completed"
					dec := "render_failed"
					intent.Decision = &dec
					reason := fmt.Sprintf("body render error: %v", err)
					intent.Reason = &reason
					_ = store.UpdateJourneyMessageIntent(ctx, intent)
					return true, nil
				}
			}

			if htmlBody != "" && len(cfg.TrackingSecretKey) > 0 {
				upsertLink := func(originalURL string) (string, error) {
					return store.UpsertTrackedLink(ctx, intent.TenantID, template.ID, originalURL)
				}
				rewritten, err := render.RewriteLinks(htmlBody, intent.TenantID, p.AppID, intent.JourneyID, intent.ProfileID, template.ID, intent.ID, upsertLink, cfg.TrackingSecretKey, cfg.TrackingBaseURL)
				if err != nil {
					slog.Error("failed to rewrite links in HTML body", "error", err)
				} else {
					htmlBody = rewritten
				}

				openToken, err := render.SignOpenToken(intent.TenantID, p.AppID, intent.JourneyID, intent.ProfileID, template.ID, intent.ID, cfg.TrackingSecretKey)
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
				Channel:        template.Channel,
				Endpoint:       intent.Endpoint,
				Subject:        subject,
				HTML:           htmlBody,
				Text:           textBody,
				Body:           bodyPayload,
				Identity:       identity,
				IdempotencyKey: fmt.Sprintf("sent-%s-%s", intent.RunID, intent.NodeID),
			}

			providerMsgID, costMicros, err = adapter.Send(ctx, msg)
			if err != nil {
				slog.Error("failed to send journey message via adapter", "error", err, "profile_id", intent.ProfileID)
				if channels.IsInvalidTokenError(err) {
					appID, errApp := store.GetFirstAppID(ctx, intent.TenantID, intent.WorkspaceID)
					if errApp != nil {
						appID = "app-1"
					}
					retireErr := store.RetireDeviceToken(ctx, intent.TenantID, appID, intent.Endpoint)
					if retireErr != nil {
						slog.Error("failed to retire invalid device token", "error", retireErr, "token", intent.Endpoint)
					} else {
						telemetry.PushTokensRetired.Add(ctx, 1)
					}
					intent.Status = "completed"
					dec := "failed"
					intent.Decision = &dec
					reason := fmt.Sprintf("invalid token retired: %v", err)
					intent.Reason = &reason
					intent.ErrorMessage = &reason
					_ = store.UpdateJourneyMessageIntent(ctx, intent)
				} else if channels.IsRetryableError(err) {
					intent.Status = "failed"
					dec := "retryable_failed"
					if intent.Attempts >= 3 {
						intent.Status = "dead"
						dec = "send_failed"
					}
					intent.Decision = &dec
					reason := fmt.Sprintf("transient send error: %v", err)
					intent.Reason = &reason
					intent.ErrorMessage = &reason
					updateErr := store.UpdateJourneyMessageIntent(ctx, intent)
					if updateErr != nil {
						slog.Error("failed to update journey message intent on transient error", "error", updateErr)
					}
					if intent.Status == "dead" {
						telemetry.JourneyDeadLettered.Add(ctx, 1, otelmetric.WithAttributes(
							attribute.String("tenant_id", intent.TenantID),
							attribute.String("type", "intent"),
						))
					}
					hasRetryableError = true
					retryableErrMsg = err.Error()
				} else {
					intent.Status = "completed"
					dec := "send_failed"
					intent.Decision = &dec
					reason := fmt.Sprintf("adapter send error: %v", err)
					intent.Reason = &reason
					intent.ErrorMessage = &reason
					_ = store.UpdateJourneyMessageIntent(ctx, intent)
				}
				if hasRetryableError {
					return true, fmt.Errorf("transient delivery failure: %s", retryableErrMsg)
				}
				return true, nil
			}

			// Update decision to provider_sent in database before emitting event
			intent.ProviderMessageID = &providerMsgID
			intent.CostMicros = costMicros
			dec := "provider_sent"
			intent.Decision = &dec
			reason := "eligible"
			intent.Reason = &reason
			err = store.UpdateJourneyMessageIntent(ctx, intent)
			if err != nil {
				slog.Error("failed to update journey message intent to provider_sent", "error", err)
				return true, fmt.Errorf("persist provider_sent decision: %w", err)
			}
		}
	}

	// 2. Emit message.sent event
emitSentEvent:
	eventPayload, _ := json.Marshal(map[string]any{
		"journey_id":         intent.JourneyID,
		"journey_version_id": intent.JourneyVersionID,
		"node_id":            intent.NodeID,
		"run_id":             intent.RunID,
		"channel":            intent.Channel,
		"endpoint":           intent.Endpoint,
		"experiment_id":      intent.ExperimentID,
		"variant":            intent.Variant,
	})
	emittedEvent := domain.Event{
		Type:           "message.sent",
		SchemaVersion:  1,
		ExternalID:     prof.ExternalID,
		AnonymousID:    prof.AnonymousID,
		IdempotencyKey: fmt.Sprintf("sent-%s-%s", intent.RunID, intent.NodeID),
		OccurredAt:     clk.Now().UTC(),
		Payload:        eventPayload,
	}

	_, err = store.AcceptEvents(ctx, p, []domain.Event{emittedEvent})
	if err != nil {
		slog.Error("failed to emit message.sent event", "error", err, "profile_id", intent.ProfileID)
		// The provider has already accepted the message. Keep that fact durable and
		// make the intent claimable again so reconciliation retries only publication.
		intent.Status = "pending"
		if intent.Attempts >= 3 {
			intent.Status = "dead"
			errMsg := fmt.Sprintf("message.sent emission failed after %d attempts: %v", intent.Attempts, err)
			intent.ErrorMessage = &errMsg
			telemetry.JourneyDeadLettered.Add(ctx, 1, otelmetric.WithAttributes(
				attribute.String("tenant_id", intent.TenantID),
				attribute.String("type", "intent"),
			))
		}
		dec := "provider_sent"
		intent.Decision = &dec
		if updateErr := store.UpdateJourneyMessageIntent(ctx, intent); updateErr != nil {
			return true, fmt.Errorf("emit message.sent event: %v; requeue provider_sent intent: %w", err, updateErr)
		}
		return true, fmt.Errorf("emit message.sent event: %w", err)
	}

	// 3. Mark intent completed & sent
	intent.Status = "completed"
	dec := "sent"
	intent.Decision = &dec
	err = store.UpdateJourneyMessageIntent(ctx, intent)
	if err != nil {
		slog.Error("failed to update journey message intent to sent", "error", err)
		return true, err
	}

	telemetry.MessagesSent.Add(ctx, 1, otelmetric.WithAttributes(
		attribute.String("channel", template.Channel),
		attribute.String("journey_id", intent.JourneyID),
	))
	telemetry.JourneyMessagesSent.Add(ctx, 1, otelmetric.WithAttributes(
		attribute.String("tenant_id", intent.TenantID),
		attribute.String("journey_id", intent.JourneyID),
		attribute.String("channel", template.Channel),
	))

	slog.Info("journey message intent completed successfully", "intent_id", intent.ID)
	return true, nil
}
