package channels

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"
	"golang.org/x/time/rate"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// DeliveryError wraps an inner error and categorizes whether it's safe to retry.
type DeliveryError struct {
	Err          error
	Retryable    bool
	InvalidToken bool
}

func (e *DeliveryError) Error() string {
	if e.Err == nil {
		return "delivery error"
	}
	return e.Err.Error()
}

func (e *DeliveryError) Unwrap() error {
	return e.Err
}

// IsRetryable returns true if the error is temporary, such as throttling or service overload.
func (e *DeliveryError) IsRetryable() bool {
	return e.Retryable
}

func (e *DeliveryError) IsInvalidToken() bool {
	return e.InvalidToken
}

// IsRetryableError is a convenience helper to check if a delivery error is retryable.
func IsRetryableError(err error) bool {
	var de *DeliveryError
	if errors.As(err, &de) {
		return de.IsRetryable()
	}
	return false
}

// IsInvalidTokenError is a convenience helper to check if an error is an invalid device token.
func IsInvalidTokenError(err error) bool {
	type invalidTokenError interface {
		IsInvalidToken() bool
	}
	var ite invalidTokenError
	if errors.As(err, &ite) {
		return ite.IsInvalidToken()
	}
	return false
}

// SESConfig holds optional overrides for regional deployment and configuration sets.
type SESConfig struct {
	Region               string `json:"region"`
	ConfigurationSetName string `json:"ses_configuration_set,omitempty"`
}

// SESClient specifies the subset of the AWS SES SDK we interact with (for mockability in tests).
type SESClient interface {
	SendEmail(ctx context.Context, params *sesv2.SendEmailInput, optFns ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error)
}

// SESAdapter implements ports.ChannelAdapter for Amazon SES.
type SESAdapter struct {
	mu        sync.Mutex
	limiters  map[string]*rate.Limiter
	newClient func(ctx context.Context, region string) (SESClient, error)
}

// NewSESAdapter creates an initialized SESAdapter.
func NewSESAdapter() *SESAdapter {
	return &SESAdapter{
		limiters: make(map[string]*rate.Limiter),
		newClient: func(ctx context.Context, region string) (SESClient, error) {
			cfg, err := config.LoadDefaultConfig(ctx, config.WithRegion(region))
			if err != nil {
				return nil, err
			}
			return sesv2.NewFromConfig(cfg), nil
		},
	}
}

// Send rate-limits and sends an email via SES.
func (s *SESAdapter) Send(ctx context.Context, msg ports.RenderedMessage) (string, error) {
	if msg.Identity.Channel != "email" {
		return "", &DeliveryError{Err: fmt.Errorf("invalid channel for SES: %s", msg.Identity.Channel), Retryable: false}
	}
	if !msg.Identity.Verified {
		return "", &DeliveryError{Err: errors.New("cannot send via unverified identity"), Retryable: false}
	}

	// Retrieve or create token-bucket rate limiter for this sending identity
	limiter := s.getLimiter(msg.Identity.ID, msg.Identity.MaxSendRate)
	if err := limiter.Wait(ctx); err != nil {
		return "", &DeliveryError{Err: fmt.Errorf("rate limiter wait aborted: %w", err), Retryable: true}
	}

	// Parse custom config
	var sesCfg SESConfig
	if len(msg.Identity.Config) > 0 && string(msg.Identity.Config) != "{}" {
		if err := json.Unmarshal(msg.Identity.Config, &sesCfg); err != nil {
			return "", &DeliveryError{Err: fmt.Errorf("failed to parse SES config: %w", err), Retryable: false}
		}
	}

	region := sesCfg.Region
	if region == "" {
		region = "us-east-1"
	}

	// Initialize SES client
	client, err := s.newClient(ctx, region)
	if err != nil {
		return "", &DeliveryError{Err: fmt.Errorf("failed to initialize SES client: %w", err), Retryable: true}
	}

	// Construct input payload
	from := s.formatFromAddress(msg.Identity)
	input := &sesv2.SendEmailInput{
		FromEmailAddress: aws.String(from),
		Destination: &types.Destination{
			ToAddresses: []string{msg.Endpoint},
		},
		Content: &types.EmailContent{
			Simple: &types.Message{
				Subject: &types.Content{
					Data: aws.String(msg.Subject),
				},
				Body: &types.Body{},
			},
		},
	}

	if msg.HTML != "" {
		input.Content.Simple.Body.Html = &types.Content{
			Data: aws.String(msg.HTML),
		}
	}
	if msg.Text != "" {
		input.Content.Simple.Body.Text = &types.Content{
			Data: aws.String(msg.Text),
		}
	}

	if sesCfg.ConfigurationSetName != "" {
		input.ConfigurationSetName = aws.String(sesCfg.ConfigurationSetName)
	}

	if msg.Identity.ReplyTo != nil && *msg.Identity.ReplyTo != "" {
		input.ReplyToAddresses = []string{*msg.Identity.ReplyTo}
	}

	if msg.IdempotencyKey != "" {
		input.EmailTags = []types.MessageTag{
			{
				Name:  aws.String("IdempotencyKey"),
				Value: aws.String(msg.IdempotencyKey),
			},
		}
	}

	output, err := client.SendEmail(ctx, input)
	if err != nil {
		return "", s.mapError(err)
	}

	if output.MessageId == nil {
		return "ses-fallback-id", nil
	}
	return *output.MessageId, nil
}

// ValidateConfig enforces that the identity is verified and properly configured for SES.
func (s *SESAdapter) ValidateConfig(iden domain.SendingIdentity) error {
	if iden.Channel != "email" {
		return fmt.Errorf("SES channel must be email, got: %s", iden.Channel)
	}
	if !iden.Verified {
		return errors.New("sending identity must be verified")
	}
	if iden.FromAddress == nil || *iden.FromAddress == "" {
		return errors.New("from_address is required")
	}
	return nil
}

func (s *SESAdapter) getLimiter(id string, maxRate int) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()

	if maxRate <= 0 {
		maxRate = 14 // standard default rate limit for SES sandboxes
	}

	limiter, exists := s.limiters[id]
	if !exists {
		// Create a rate limiter allowing up to maxRate per second, with a burst size matching maxRate
		limiter = rate.NewLimiter(rate.Limit(maxRate), maxRate)
		s.limiters[id] = limiter
	}
	return limiter
}

func (s *SESAdapter) formatFromAddress(iden domain.SendingIdentity) string {
	addr := ""
	if iden.FromAddress != nil {
		addr = *iden.FromAddress
	}
	if iden.FromName != nil && *iden.FromName != "" {
		return fmt.Sprintf("%s <%s>", *iden.FromName, addr)
	}
	return addr
}

func (s *SESAdapter) mapError(err error) error {
	if err == nil {
		return nil
	}

	retryable := false

	// 1. Check for explicit AWS SDK throttling / server exception types
	var tooManyReq *types.TooManyRequestsException
	var limitExceeded *types.LimitExceededException
	var internalErr *types.InternalServiceErrorException

	if errors.As(err, &tooManyReq) || errors.As(err, &limitExceeded) || errors.As(err, &internalErr) {
		retryable = true
	} else {
		// 2. Fall back to network issues, timeouts, DNS failures
		var netErr net.Error
		if errors.As(err, &netErr) {
			if netErr.Timeout() || netErr.Temporary() {
				retryable = true
			}
		} else {
			// 3. Simple text signature matchers
			errStr := strings.ToLower(err.Error())
			if strings.Contains(errStr, "throttling") ||
				strings.Contains(errStr, "rate exceeded") ||
				strings.Contains(errStr, "temporary") ||
				strings.Contains(errStr, "timeout") ||
				strings.Contains(errStr, "503") ||
				strings.Contains(errStr, "502") ||
				strings.Contains(errStr, "500") {
				retryable = true
			}
		}
	}

	return &DeliveryError{
		Err:       err,
		Retryable: retryable,
	}
}
