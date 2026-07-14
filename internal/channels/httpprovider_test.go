package channels_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/channels"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// makeHTTPMsg builds a minimal RenderedMessage for adapter tests.
func makeHTTPMsg(endpoint string, identConfig []byte) ports.RenderedMessage {
	return ports.RenderedMessage{
		Channel:        "sms",
		Endpoint:       endpoint,
		Body:           "Hello test",
		IdempotencyKey: "key-abc",
		Identity: domain.SendingIdentity{
			Channel:  "sms",
			Provider: "http",
			Config:   identConfig,
		},
	}
}

func mustJSONBytes(v interface{}) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// -----------------------------------------------------------------------
// FakeProviderProfile — ParseResponse classification tests
// -----------------------------------------------------------------------

func TestFakeProviderProfile_200_Success(t *testing.T) {
	profile := channels.NewFakeProviderProfile()
	resp := &http.Response{StatusCode: 200}
	body := mustJSONBytes(map[string]string{"endpoint": "+15005550001", "body": "hi"})

	providerID, err := profile.ParseResponse(resp, body)
	if err != nil {
		t.Fatalf("expected success on 200, got: %v", err)
	}
	if providerID == "" {
		t.Error("expected non-empty provider ID on 200")
	}
}

func TestFakeProviderProfile_5xx_IsRetryable(t *testing.T) {
	profile := channels.NewFakeProviderProfile()
	for _, status := range []int{500, 502, 503, 504} {
		resp := &http.Response{StatusCode: status}
		_, err := profile.ParseResponse(resp, nil)
		if err == nil {
			t.Fatalf("status %d: expected error", status)
		}
		if !channels.IsRetryableError(err) {
			t.Errorf("status %d: 5xx should be retryable, got %v", status, err)
		}
	}
}

func TestFakeProviderProfile_4xx_IsPermanent(t *testing.T) {
	profile := channels.NewFakeProviderProfile()
	for _, status := range []int{400, 401, 403, 404, 422} {
		resp := &http.Response{StatusCode: status}
		_, err := profile.ParseResponse(resp, nil)
		if err == nil {
			t.Fatalf("status %d: expected error", status)
		}
		if channels.IsRetryableError(err) {
			t.Errorf("status %d: 4xx should NOT be retryable, got %v", status, err)
		}
	}
}

func TestFakeProviderProfile_IsInvalidToken_AlwaysFalse(t *testing.T) {
	profile := channels.NewFakeProviderProfile()
	if profile.IsInvalidToken(nil, nil) {
		t.Error("FakeProviderProfile.IsInvalidToken must always return false")
	}
}

func TestFakeProviderProfile_BuildRequest_RecordsCall(t *testing.T) {
	profile := channels.NewFakeProviderProfile()
	msg := makeHTTPMsg("+15005550001", nil)

	req, err := profile.BuildRequest(context.Background(), msg)
	if err != nil {
		t.Fatalf("BuildRequest: %v", err)
	}
	if req == nil {
		t.Fatal("expected non-nil request")
	}
	if len(profile.Requests) != 1 {
		t.Errorf("expected 1 captured request, got %d", len(profile.Requests))
	}
}

// -----------------------------------------------------------------------
// HTTPGenericProfile — ParseResponse classification (no HTTP server needed)
// -----------------------------------------------------------------------

func TestHTTPGenericProfile_ParseResponse_2xx_Success(t *testing.T) {
	profile := &channels.HTTPGenericProfile{}
	for _, status := range []int{200, 201, 204} {
		resp := &http.Response{StatusCode: status}
		id, err := profile.ParseResponse(resp, nil)
		if err != nil {
			t.Errorf("status %d: expected success, got: %v", status, err)
		}
		if id == "" {
			t.Errorf("status %d: expected non-empty provider ID", status)
		}
	}
}

func TestHTTPGenericProfile_ParseResponse_5xx_Retryable(t *testing.T) {
	profile := &channels.HTTPGenericProfile{}
	for _, status := range []int{500, 502, 503, 504} {
		resp := &http.Response{StatusCode: status}
		_, err := profile.ParseResponse(resp, []byte("gateway error"))
		if err == nil {
			t.Fatalf("status %d: expected error", status)
		}
		if !channels.IsRetryableError(err) {
			t.Errorf("status %d: 5xx should be retryable, got: %v", status, err)
		}
	}
}

func TestHTTPGenericProfile_ParseResponse_4xx_Permanent(t *testing.T) {
	profile := &channels.HTTPGenericProfile{}
	for _, status := range []int{400, 401, 403, 404, 422} {
		resp := &http.Response{StatusCode: status}
		_, err := profile.ParseResponse(resp, []byte("bad request"))
		if err == nil {
			t.Fatalf("status %d: expected error", status)
		}
		if channels.IsRetryableError(err) {
			t.Errorf("status %d: 4xx should NOT be retryable, got: %v", status, err)
		}
	}
}

func TestHTTPGenericProfile_IsInvalidToken_AlwaysFalse(t *testing.T) {
	profile := &channels.HTTPGenericProfile{}
	if profile.IsInvalidToken(nil, nil) {
		t.Error("HTTPGenericProfile.IsInvalidToken must always return false for SMS")
	}
}

// TestHTTPGenericProfile_BuildRequest_MissingEndpoint verifies that a missing endpoint
// is caught at BuildRequest time (permanent error, no request sent).
func TestHTTPGenericProfile_BuildRequest_MissingEndpoint(t *testing.T) {
	profile := &channels.HTTPGenericProfile{}
	msg := makeHTTPMsg("+15005550001", mustJSONBytes(map[string]string{})) // no endpoint key

	_, err := profile.BuildRequest(context.Background(), msg)
	if err == nil {
		t.Error("expected error when endpoint is missing from config")
	}
}
