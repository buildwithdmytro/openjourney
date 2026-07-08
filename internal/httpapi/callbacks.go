package httpapi

import (
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

var snsHostRegex = regexp.MustCompile(`^sns\.[a-z0-9-]+\.amazonaws\.com$`)


// SNSMessage represents the standard JSON envelope sent by AWS SNS.
type SNSMessage struct {
	Type             string `json:"Type"`
	MessageId        string `json:"MessageId"`
	Token            string `json:"Token,omitempty"`
	TopicArn         string `json:"TopicArn"`
	Subject          string `json:"Subject,omitempty"`
	Message          string `json:"Message"`
	Timestamp        string `json:"Timestamp"`
	SignatureVersion string `json:"SignatureVersion"`
	Signature        string `json:"Signature"`
	SigningCertURL   string `json:"SigningCertURL"`
	SubscribeURL     string `json:"SubscribeURL,omitempty"`
}

// SESNotification represents the nested message payload sent by Amazon SES inside SNS.
type SESNotification struct {
	EventType string `json:"eventType"`
	Bounce    *struct {
		BounceType        string `json:"bounceType"`
		BounceSubType     string `json:"bounceSubType"`
		BouncedRecipients []struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"bouncedRecipients"`
	} `json:"bounce,omitempty"`
	Complaint *struct {
		ComplainedRecipients []struct {
			EmailAddress string `json:"emailAddress"`
		} `json:"complainedRecipients"`
	} `json:"complaint,omitempty"`
	Delivery *struct {
		Timestamp  string   `json:"timestamp"`
		Recipients []string `json:"recipients"`
	} `json:"delivery,omitempty"`
	Mail struct {
		MessageId string `json:"messageId"`
		Headers   []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
	} `json:"mail"`
}

// Cert cache to avoid heavy and slow HTTP fetches on every callback invocation.
var certCache sync.Map // string -> *x509.Certificate

// handleSESCallback processes asynchronous SES bounces, complaints, and delivery SNS notifications.
func (s *Server) handleSESCallback(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var snsMsg SNSMessage
	if err := json.Unmarshal(bodyBytes, &snsMsg); err != nil {
		http.Error(w, "invalid sns payload json", http.StatusBadRequest)
		return
	}

	// 1. Verify SNS signature (critical security safeguard)
	if err := s.snsVerifier.Verify(snsMsg); err != nil {
		http.Error(w, fmt.Sprintf("signature verification failed: %v", err), http.StatusBadRequest)
		return
	}

	// 1b. Verify Topic ARN matches the allowlist if configured
	if len(s.allowedTopicARNs) > 0 {
		allowed := false
		for _, arn := range s.allowedTopicARNs {
			if snsMsg.TopicArn == arn {
				allowed = true
				break
			}
		}
		if !allowed {
			http.Error(w, fmt.Sprintf("forbidden: topic ARN %q not in allowlist", snsMsg.TopicArn), http.StatusForbidden)
			return
		}
	}

	// 2. Handle SubscriptionConfirmation
	if snsMsg.Type == "SubscriptionConfirmation" {
		if err := confirmSNSSubscription(snsMsg.SubscribeURL); err != nil {
			http.Error(w, fmt.Sprintf("failed to confirm subscription: %v", err), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("SubscriptionConfirmed"))
		return
	}

	// 3. Handle SES Notification
	if snsMsg.Type == "Notification" {
		var sesNotify SESNotification
		if err := json.Unmarshal([]byte(snsMsg.Message), &sesNotify); err != nil {
			http.Error(w, "failed to parse inner SES notification", http.StatusBadRequest)
			return
		}

		// Extract Tenant, Workspace, and Campaign IDs from mail headers or URL query parameters
		tenantID := ""
		workspaceID := ""
		appID := "default"
		campaignID := ""

		for _, header := range sesNotify.Mail.Headers {
			switch strings.ToLower(header.Name) {
			case "x-tenant-id":
				tenantID = header.Value
			case "x-workspace-id":
				workspaceID = header.Value
			case "x-app-id":
				appID = header.Value
			case "x-campaign-id":
				campaignID = header.Value
			}
		}

		// Query overrides
		if qTenant := r.URL.Query().Get("tenant_id"); qTenant != "" {
			tenantID = qTenant
		}
		if qWorkspace := r.URL.Query().Get("workspace_id"); qWorkspace != "" {
			workspaceID = qWorkspace
		}
		if qApp := r.URL.Query().Get("app_id"); qApp != "" {
			appID = qApp
		}
		if qCampaign := r.URL.Query().Get("campaign_id"); qCampaign != "" {
			campaignID = qCampaign
		}

		if tenantID == "" || workspaceID == "" {
			http.Error(w, "tenant_id and workspace_id must be provided in callback URL or message headers", http.StatusBadRequest)
			return
		}

		if campaignID == "" {
			campaignID = "ses-callback-campaign"
		}

		principal := domain.Principal{
			TenantID:    tenantID,
			WorkspaceID: workspaceID,
			AppID:       appID,
			Scopes:      []string{"events:write"},
		}

		// Map to standard OpenJourney events and ingest them into our core store
		switch sesNotify.EventType {
		case "Bounce":
			if sesNotify.Bounce != nil {
				for _, bRec := range sesNotify.Bounce.BouncedRecipients {
					payload, _ := json.Marshal(map[string]any{
						"campaign_id": campaignID,
						"endpoint":    bRec.EmailAddress,
						"bounce_type": sesNotify.Bounce.BounceType,
					})
					event := domain.Event{
						Type:           "message.bounced",
						SchemaVersion:  1,
						ExternalID:     bRec.EmailAddress,
						IdempotencyKey: fmt.Sprintf("bounce-%s-%s-%s", campaignID, bRec.EmailAddress, sesNotify.Mail.MessageId),
						OccurredAt:     time.Now(),
						Payload:        payload,
					}
					if _, err := s.store.AcceptEvents(r.Context(), principal, []domain.Event{event}); err != nil {
						slog.Error("failed to accept bounce event", "error", err, "recipient", bRec.EmailAddress)
					}
				}
			}

		case "Complaint":
			if sesNotify.Complaint != nil {
				for _, cRec := range sesNotify.Complaint.ComplainedRecipients {
					payload, _ := json.Marshal(map[string]any{
						"campaign_id": campaignID,
						"endpoint":    cRec.EmailAddress,
					})
					event := domain.Event{
						Type:           "message.complained",
						SchemaVersion:  1,
						ExternalID:     cRec.EmailAddress,
						IdempotencyKey: fmt.Sprintf("complaint-%s-%s-%s", campaignID, cRec.EmailAddress, sesNotify.Mail.MessageId),
						OccurredAt:     time.Now(),
						Payload:        payload,
					}
					if _, err := s.store.AcceptEvents(r.Context(), principal, []domain.Event{event}); err != nil {
						slog.Error("failed to accept complaint event", "error", err, "recipient", cRec.EmailAddress)
					}
				}
			}

		case "Delivery":
			if sesNotify.Delivery != nil {
				for _, dRec := range sesNotify.Delivery.Recipients {
					payload, _ := json.Marshal(map[string]any{
						"campaign_id": campaignID,
						"endpoint":    dRec,
					})
					event := domain.Event{
						Type:           "message.delivered",
						SchemaVersion:  1,
						ExternalID:     dRec,
						IdempotencyKey: fmt.Sprintf("delivered-%s-%s-%s", campaignID, dRec, sesNotify.Mail.MessageId),
						OccurredAt:     time.Now(),
						Payload:        payload,
					}
					if _, err := s.store.AcceptEvents(r.Context(), principal, []domain.Event{event}); err != nil {
						slog.Error("failed to accept delivery event", "error", err, "recipient", dRec)
					}
				}
			}
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("NotificationProcessed"))
		return
	}

	w.WriteHeader(http.StatusOK)
}

type snsSignatureVerifier interface {
	Verify(msg SNSMessage) error
}

type realSNSSignatureVerifier struct{}

func (v realSNSSignatureVerifier) Verify(msg SNSMessage) error {
	return verifySNSSignature(msg)
}

// verifySNSSignature validates the cryptographical origin of the SNS notification payload.
func verifySNSSignature(msg SNSMessage) error {
	if msg.Signature == "" {
		return errors.New("missing signature")
	}

	// 1. Verify URL domain (crucial to block SSRF and rogue cert sources)
	certURL, err := url.Parse(msg.SigningCertURL)
	if err != nil {
		return fmt.Errorf("failed to parse SigningCertURL: %w", err)
	}

	if certURL.Scheme != "https" || !snsHostRegex.MatchString(certURL.Host) {
		return fmt.Errorf("invalid cert host: %s (must match sns.<region>.amazonaws.com)", certURL.Host)
	}

	// 2. Fetch/Retrieve Certificate
	var cert *x509.Certificate
	if cached, ok := certCache.Load(msg.SigningCertURL); ok {
		cert = cached.(*x509.Certificate)
	} else {
		resp, err := http.Get(msg.SigningCertURL)
		if err != nil {
			return fmt.Errorf("failed to fetch signing certificate: %w", err)
		}
		defer resp.Body.Close()

		pemBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read certificate body: %w", err)
		}

		block, _ := pem.Decode(pemBytes)
		if block == nil {
			return errors.New("failed to decode certificate PEM")
		}

		parsed, err := x509.ParseCertificate(block.Bytes)
		if err != nil {
			return fmt.Errorf("failed to parse x509 certificate: %w", err)
		}

		certCache.Store(msg.SigningCertURL, parsed)
		cert = parsed
	}

	// 3. Reconstruct signature payload format
	var sb strings.Builder
	if msg.Type == "Notification" {
		sb.WriteString("Message\n")
		sb.WriteString(msg.Message)
		sb.WriteString("\n")
		sb.WriteString("MessageId\n")
		sb.WriteString(msg.MessageId)
		sb.WriteString("\n")
		if msg.Subject != "" {
			sb.WriteString("Subject\n")
			sb.WriteString(msg.Subject)
			sb.WriteString("\n")
		}
		sb.WriteString("Timestamp\n")
		sb.WriteString(msg.Timestamp)
		sb.WriteString("\n")
		sb.WriteString("TopicArn\n")
		sb.WriteString(msg.TopicArn)
		sb.WriteString("\n")
		sb.WriteString("Type\n")
		sb.WriteString(msg.Type)
		sb.WriteString("\n")
	} else if msg.Type == "SubscriptionConfirmation" || msg.Type == "UnsubscribeConfirmation" {
		sb.WriteString("Message\n")
		sb.WriteString(msg.Message)
		sb.WriteString("\n")
		sb.WriteString("MessageId\n")
		sb.WriteString(msg.MessageId)
		sb.WriteString("\n")
		sb.WriteString("SubscribeURL\n")
		sb.WriteString(msg.SubscribeURL)
		sb.WriteString("\n")
		sb.WriteString("Timestamp\n")
		sb.WriteString(msg.Timestamp)
		sb.WriteString("\n")
		sb.WriteString("Token\n")
		sb.WriteString(msg.Token)
		sb.WriteString("\n")
		sb.WriteString("TopicArn\n")
		sb.WriteString(msg.TopicArn)
		sb.WriteString("\n")
		sb.WriteString("Type\n")
		sb.WriteString(msg.Type)
		sb.WriteString("\n")
	} else {
		return fmt.Errorf("unsupported SNS type for signature validation: %s", msg.Type)
	}

	sigBytes, err := base64.StdEncoding.DecodeString(msg.Signature)
	if err != nil {
		return fmt.Errorf("failed to decode signature: %w", err)
	}

	// 4. Cryptographically check signature
	if msg.SignatureVersion == "2" {
		return cert.CheckSignature(x509.SHA256WithRSA, []byte(sb.String()), sigBytes)
	}
	return cert.CheckSignature(x509.SHA1WithRSA, []byte(sb.String()), sigBytes)
}

// confirmSNSSubscription confirms the HTTP hook subscription via AWS SNS.
func confirmSNSSubscription(subscribeURL string) error {
	u, err := url.Parse(subscribeURL)
	if err != nil {
		return err
	}

	if u.Scheme != "https" || !snsHostRegex.MatchString(u.Host) {
		return fmt.Errorf("invalid subscription target host: %s (must match sns.<region>.amazonaws.com)", u.Host)
	}

	resp, err := http.Get(subscribeURL)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("subscription HTTP confirmation failed with code %d", resp.StatusCode)
	}

	return nil
}
