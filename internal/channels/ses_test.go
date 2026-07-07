package channels

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sesv2"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

type mockSESClient struct {
	SendEmailFunc func(ctx context.Context, params *sesv2.SendEmailInput) (*sesv2.SendEmailOutput, error)
}

func (m *mockSESClient) SendEmail(ctx context.Context, params *sesv2.SendEmailInput, optFns ...func(*sesv2.Options)) (*sesv2.SendEmailOutput, error) {
	if m.SendEmailFunc != nil {
		return m.SendEmailFunc(ctx, params)
	}
	msgID := "mock-msg-id"
	return &sesv2.SendEmailOutput{MessageId: &msgID}, nil
}

func TestSESAdapter_ValidateConfig(t *testing.T) {
	s := NewSESAdapter()

	from := "sender@example.com"
	valid := domain.SendingIdentity{
		Channel:     "email",
		Verified:    true,
		FromAddress: &from,
	}

	if err := s.ValidateConfig(valid); err != nil {
		t.Fatalf("expected valid identity to pass config validation, got: %v", err)
	}

	invalidUnverified := valid
	invalidUnverified.Verified = false
	if err := s.ValidateConfig(invalidUnverified); err == nil {
		t.Fatal("expected unverified identity to fail validation")
	}

	invalidChannel := valid
	invalidChannel.Channel = "webhook"
	if err := s.ValidateConfig(invalidChannel); err == nil {
		t.Fatal("expected webhook channel to fail SES validation")
	}
}

func TestSESAdapter_Send_Success(t *testing.T) {
	s := NewSESAdapter()
	var capturedInput *sesv2.SendEmailInput
	s.newClient = func(ctx context.Context, region string) (SESClient, error) {
		return &mockSESClient{
			SendEmailFunc: func(ctx context.Context, params *sesv2.SendEmailInput) (*sesv2.SendEmailOutput, error) {
				capturedInput = params
				id := "test-ses-id"
				return &sesv2.SendEmailOutput{MessageId: &id}, nil
			},
		}, nil
	}

	from := "sender@example.com"
	name := "Sender Team"
	reply := "reply@example.com"
	msg := ports.RenderedMessage{
		Channel:  "email",
		Endpoint: "recipient@example.com",
		Subject:  "Hello!",
		HTML:     "<p>Body</p>",
		Identity: domain.SendingIdentity{
			ID:          "iden-1",
			Channel:     "email",
			Verified:    true,
			FromAddress: &from,
			FromName:    &name,
			ReplyTo:     &reply,
			MaxSendRate: 10,
		},
	}

	id, err := s.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected Send error: %v", err)
	}
	if id != "test-ses-id" {
		t.Errorf("expected msg ID 'test-ses-id', got: %s", id)
	}

	if capturedInput == nil {
		t.Fatal("expected SendEmail to be called")
	}
	if *capturedInput.FromEmailAddress != "Sender Team <sender@example.com>" {
		t.Errorf("expected formatted from address, got: %s", *capturedInput.FromEmailAddress)
	}
	if capturedInput.Destination.ToAddresses[0] != "recipient@example.com" {
		t.Errorf("expected recipient email, got: %s", capturedInput.Destination.ToAddresses[0])
	}
}

func TestSESAdapter_MapErrors(t *testing.T) {
	s := NewSESAdapter()

	// Test permanent error
	permErr := errors.New("invalid address")
	mappedPerm := s.mapError(permErr)
	if IsRetryableError(mappedPerm) {
		t.Fatal("expected 'invalid address' error to be permanent (not retryable)")
	}

	// Test retryable throttling error
	throttleErr := errors.New("Throttling: Rate exceeded")
	mappedThrottle := s.mapError(throttleErr)
	if !IsRetryableError(mappedThrottle) {
		t.Fatal("expected 'Throttling: Rate exceeded' error to be retryable")
	}
}
