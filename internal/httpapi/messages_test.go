package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

type mockMessageStore struct {
	ports.Store
	getProfileIDBySubjectFn func(ctx context.Context, tenantID, appID, subject string) (string, error)
	getInAppMessageFn       func(ctx context.Context, tenantID, msgID string) (domain.InAppMessage, error)
	acceptEventsFn          func(ctx context.Context, p domain.Principal, events []domain.Event) ([]string, error)
}

func (m *mockMessageStore) GetProfileIDBySubject(ctx context.Context, tenantID, appID, subject string) (string, error) {
	if m.getProfileIDBySubjectFn != nil {
		return m.getProfileIDBySubjectFn(ctx, tenantID, appID, subject)
	}
	return "", postgres.ErrNotFound
}

func (m *mockMessageStore) GetInAppMessage(ctx context.Context, tenantID, msgID string) (domain.InAppMessage, error) {
	if m.getInAppMessageFn != nil {
		return m.getInAppMessageFn(ctx, tenantID, msgID)
	}
	return domain.InAppMessage{}, postgres.ErrNotFound
}

func (m *mockMessageStore) AcceptEvents(ctx context.Context, p domain.Principal, events []domain.Event) ([]string, error) {
	if m.acceptEventsFn != nil {
		return m.acceptEventsFn(ctx, p, events)
	}
	return nil, nil
}

func TestSignInAppToken(t *testing.T) {
	secret := []byte("test-secret")
	tenantID := "tenant1"
	appID := "app1"
	subject := "user1"
	expiresAt := time.Now().Add(1 * time.Hour)

	token, err := SignInAppToken(tenantID, appID, subject, expiresAt, secret)
	if err != nil {
		t.Fatalf("SignInAppToken failed: %v", err)
	}

	verified, err := VerifyInAppToken(token, tenantID, appID, secret, time.Now())
	if err != nil {
		t.Fatalf("VerifyInAppToken failed: %v", err)
	}

	if verified.TenantID != tenantID {
		t.Errorf("expected tenantID %s, got %s", tenantID, verified.TenantID)
	}
	if verified.AppID != appID {
		t.Errorf("expected appID %s, got %s", appID, verified.AppID)
	}
	if verified.Subject != subject {
		t.Errorf("expected subject %s, got %s", subject, verified.Subject)
	}
}

func TestSignInAppTokenValidation(t *testing.T) {
	secret := []byte("test-secret")
	otherSecret := []byte("other-secret")

	tests := []struct {
		name   string
		fn     func() (string, error)
		verify func(token string) (InAppToken, error)
		expect error
	}{
		{
			name: "valid token",
			fn: func() (string, error) {
				return SignInAppToken("tenant1", "app1", "user1", time.Now().Add(1*time.Hour), secret)
			},
			verify: func(token string) (InAppToken, error) {
				return VerifyInAppToken(token, "tenant1", "app1", secret, time.Now())
			},
			expect: nil,
		},
		{
			name: "expired token",
			fn: func() (string, error) {
				return SignInAppToken("tenant1", "app1", "user1", time.Now().Add(-1*time.Hour), secret)
			},
			verify: func(token string) (InAppToken, error) {
				return VerifyInAppToken(token, "tenant1", "app1", secret, time.Now())
			},
			expect: ErrExpiredInAppToken,
		},
		{
			name: "wrong secret",
			fn: func() (string, error) {
				return SignInAppToken("tenant1", "app1", "user1", time.Now().Add(1*time.Hour), secret)
			},
			verify: func(token string) (InAppToken, error) {
				return VerifyInAppToken(token, "tenant1", "app1", otherSecret, time.Now())
			},
			expect: ErrInvalidInAppToken,
		},
		{
			name: "wrong tenant",
			fn: func() (string, error) {
				return SignInAppToken("tenant1", "app1", "user1", time.Now().Add(1*time.Hour), secret)
			},
			verify: func(token string) (InAppToken, error) {
				return VerifyInAppToken(token, "tenant2", "app1", secret, time.Now())
			},
			expect: ErrInvalidInAppToken,
		},
		{
			name: "wrong app",
			fn: func() (string, error) {
				return SignInAppToken("tenant1", "app1", "user1", time.Now().Add(1*time.Hour), secret)
			},
			verify: func(token string) (InAppToken, error) {
				return VerifyInAppToken(token, "tenant1", "app2", secret, time.Now())
			},
			expect: ErrInvalidInAppToken,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token, err := test.fn()
			if err != nil {
				t.Fatalf("SignInAppToken failed: %v", err)
			}

			_, err = test.verify(token)
			if err != test.expect {
				t.Errorf("expected error %v, got %v", test.expect, err)
			}
		})
	}
}

func TestReportMessageEngagementWithValidToken(t *testing.T) {
	secret := []byte("test-secret")
	tenantID := "tenant-123"
	appID := "app-456"
	externalID := "user-789"
	messageID := "msg-001"
	profileID := "profile-123"

	mockStore := &mockMessageStore{
		getProfileIDBySubjectFn: func(ctx context.Context, tid, aid, subj string) (string, error) {
			if tid == tenantID && aid == appID && subj == externalID {
				return profileID, nil
			}
			return "", postgres.ErrNotFound
		},
		getInAppMessageFn: func(ctx context.Context, tid, mid string) (domain.InAppMessage, error) {
			if tid == tenantID && mid == messageID {
				return domain.InAppMessage{
					ID:        messageID,
					TenantID:  tenantID,
					ProfileID: profileID,
					Status:    "delivered",
				}, nil
			}
			return domain.InAppMessage{}, postgres.ErrNotFound
		},
		acceptEventsFn: func(ctx context.Context, p domain.Principal, events []domain.Event) ([]string, error) {
			if len(events) != 1 {
				t.Errorf("expected 1 event, got %d", len(events))
			}
			if events[0].Type != "message.impression" {
				t.Errorf("expected message.impression event, got %s", events[0].Type)
			}
			return []string{"event-001"}, nil
		},
	}

	server := &Server{
		store:              mockStore,
		trackingSecretKey:  secret,
		trustedProxy:       false,
		publicLimiter:      nil,
	}

	token, err := SignInAppToken(tenantID, appID, externalID, time.Now().Add(1*time.Hour), secret)
	if err != nil {
		t.Fatalf("SignInAppToken failed: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/messages/"+messageID+"/impression?tenant="+tenantID+"&app="+appID+"&external_id="+externalID+"&token="+token, nil)
	req.SetPathValue("id", messageID)
	req.SetPathValue("action", "impression")
	w := httptest.NewRecorder()

	server.reportMessageEngagement(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Errorf("failed to unmarshal response: %v", err)
	}
	if resp["status"] != "accepted" {
		t.Errorf("expected status 'accepted', got %v", resp["status"])
	}
}

func TestReportMessageEngagementIDORProtection(t *testing.T) {
	secret := []byte("test-secret")
	tenantID := "tenant-123"
	appID := "app-456"
	externalID := "user-789"
	otherExternalID := "other-user"
	messageID := "msg-001"
	profileID := "profile-123"
	otherProfileID := "profile-456"

	mockStore := &mockMessageStore{
		getProfileIDBySubjectFn: func(ctx context.Context, tid, aid, subj string) (string, error) {
			if tid == tenantID && aid == appID && subj == externalID {
				return profileID, nil
			}
			if tid == tenantID && aid == appID && subj == otherExternalID {
				return otherProfileID, nil
			}
			return "", postgres.ErrNotFound
		},
		getInAppMessageFn: func(ctx context.Context, tid, mid string) (domain.InAppMessage, error) {
			if tid == tenantID && mid == messageID {
				return domain.InAppMessage{
					ID:        messageID,
					TenantID:  tenantID,
					ProfileID: otherProfileID,
					Status:    "delivered",
				}, nil
			}
			return domain.InAppMessage{}, postgres.ErrNotFound
		},
		acceptEventsFn: func(ctx context.Context, p domain.Principal, events []domain.Event) ([]string, error) {
			t.Error("AcceptEvents should not be called for IDOR violation")
			return nil, nil
		},
	}

	server := &Server{
		store:              mockStore,
		trackingSecretKey:  secret,
		trustedProxy:       false,
		publicLimiter:      nil,
	}

	token, err := SignInAppToken(tenantID, appID, externalID, time.Now().Add(1*time.Hour), secret)
	if err != nil {
		t.Fatalf("SignInAppToken failed: %v", err)
	}

	req := httptest.NewRequest("POST", "/v1/messages/"+messageID+"/impression?tenant="+tenantID+"&app="+appID+"&external_id="+externalID+"&token="+token, nil)
	req.SetPathValue("id", messageID)
	req.SetPathValue("action", "impression")
	w := httptest.NewRecorder()

	server.reportMessageEngagement(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", w.Code)
	}
}
