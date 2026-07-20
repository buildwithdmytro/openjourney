package httpapi

import (
	"bytes"
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
	createInAppMessageFn    func(ctx context.Context, tenantID, workspaceID, appID, profileID string, msg domain.InAppMessage) (domain.InAppMessage, error)
	listInAppMessagesFn     func(ctx context.Context, p domain.Principal, appID string) ([]domain.InAppMessage, error)
	listInboxForProfileFn   func(ctx context.Context, tenantID, appID, profileID string, limit int) ([]domain.InAppMessage, error)
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

func (m *mockMessageStore) CreateInAppMessage(ctx context.Context, tenantID, workspaceID, appID, profileID string, msg domain.InAppMessage) (domain.InAppMessage, error) {
	if m.createInAppMessageFn != nil {
		return m.createInAppMessageFn(ctx, tenantID, workspaceID, appID, profileID, msg)
	}
	return domain.InAppMessage{}, nil
}

func (m *mockMessageStore) ListInAppMessages(ctx context.Context, p domain.Principal, appID string) ([]domain.InAppMessage, error) {
	if m.listInAppMessagesFn != nil {
		return m.listInAppMessagesFn(ctx, p, appID)
	}
	return []domain.InAppMessage{}, nil
}

func (m *mockMessageStore) ListInboxForProfile(ctx context.Context, tenantID, appID, profileID string, limit int) ([]domain.InAppMessage, error) {
	if m.listInboxForProfileFn != nil {
		return m.listInboxForProfileFn(ctx, tenantID, appID, profileID, limit)
	}
	return []domain.InAppMessage{}, nil
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

func TestAdminMessageHandlers(t *testing.T) {
	tenantID := "tenant-123"
	workspaceID := "workspace-456"
	appID := "app-789"
	profileID := "profile-001"
	messageID := "msg-123"

	t.Run("create_admin_message", func(t *testing.T) {
		mockStore := &mockMessageStore{
			createInAppMessageFn: func(ctx context.Context, tid, wid, aid, pid string, msg domain.InAppMessage) (domain.InAppMessage, error) {
				result := msg
				result.ID = messageID
				result.TenantID = tid
				result.WorkspaceID = wid
				result.AppID = aid
				result.ProfileID = pid
				result.CreatedAt = time.Now()
				result.UpdatedAt = time.Now()
				return result, nil
			},
		}

		server := &Server{store: mockStore}

		body, _ := json.Marshal(map[string]any{
			"app_id":      appID,
			"profile_id":  profileID,
			"message_type": "modal",
			"content":     json.RawMessage(`{"title":"Test"}`),
		})

		req := httptest.NewRequest("POST", "/v1/messages", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), principalKey{}, domain.Principal{
			TenantID:    tenantID,
			WorkspaceID: workspaceID,
		}))

		w := httptest.NewRecorder()
		server.createAdminMessage(w, req)

		if w.Code != http.StatusCreated {
			t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("list_messages", func(t *testing.T) {
		messages := []domain.InAppMessage{
			{
				ID:        "msg-001",
				TenantID:  tenantID,
				AppID:     appID,
				MessageType: "modal",
			},
		}

		mockStore := &mockMessageStore{
			listInAppMessagesFn: func(ctx context.Context, p domain.Principal, aid string) ([]domain.InAppMessage, error) {
				return messages, nil
			},
		}

		server := &Server{store: mockStore}
		req := httptest.NewRequest("GET", "/v1/messages?app_id="+appID, nil)
		req = req.WithContext(context.WithValue(req.Context(), principalKey{}, domain.Principal{
			TenantID: tenantID,
		}))

		w := httptest.NewRecorder()
		server.listMessages(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("get_message", func(t *testing.T) {
		mockStore := &mockMessageStore{
			getInAppMessageFn: func(ctx context.Context, tid, mid string) (domain.InAppMessage, error) {
				return domain.InAppMessage{
					ID:       messageID,
					TenantID: tenantID,
					Status:   "delivered",
				}, nil
			},
		}

		server := &Server{store: mockStore}
		req := httptest.NewRequest("GET", "/v1/messages/"+messageID, nil)
		req.SetPathValue("id", messageID)
		req = req.WithContext(context.WithValue(req.Context(), principalKey{}, domain.Principal{
			TenantID: tenantID,
		}))

		w := httptest.NewRecorder()
		server.getMessage(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})

	t.Run("get_profile_inbox", func(t *testing.T) {
		messages := []domain.InAppMessage{
			{
				ID:        "msg-001",
				ProfileID: profileID,
				Status:    "delivered",
			},
		}

		mockStore := &mockMessageStore{
			listInboxForProfileFn: func(ctx context.Context, tid, aid, pid string, limit int) ([]domain.InAppMessage, error) {
				return messages, nil
			},
		}

		server := &Server{store: mockStore}
		req := httptest.NewRequest("GET", "/v1/profiles/"+profileID+"/inbox?app_id="+appID, nil)
		req.SetPathValue("profileId", profileID)
		req = req.WithContext(context.WithValue(req.Context(), principalKey{}, domain.Principal{
			TenantID: tenantID,
		}))

		w := httptest.NewRecorder()
		server.getProfileInbox(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}
	})
}

func TestFetchInboxFiltersMessages(t *testing.T) {
	secret := []byte("test-secret")
	tenantID := "tenant-123"
	appID := "app-456"
	anonID := "anon-user"
	profileID := "profile-123"
	workspaceID := "workspace-789"

	t.Run("messages_without_display_rule_returned", func(t *testing.T) {
		msg := domain.InAppMessage{
			ID:          "msg-001",
			TenantID:    tenantID,
			WorkspaceID: workspaceID,
			AppID:       appID,
			ProfileID:   profileID,
			Status:      "delivered",
		}

		mockStore := &mockMessageStore{
			getProfileIDBySubjectFn: func(ctx context.Context, tid, aid, subj string) (string, error) {
				return profileID, nil
			},
			listInboxForProfileFn: func(ctx context.Context, tid, aid, pid string, limit int) ([]domain.InAppMessage, error) {
				return []domain.InAppMessage{msg}, nil
			},
		}

		server := &Server{
			store:              mockStore,
			trackingSecretKey:  secret,
			trustedProxy:       false,
			publicLimiter:      nil,
		}

		req := httptest.NewRequest("GET", "/v1/messages/inbox?tenant="+tenantID+"&app="+appID+"&anonymous_id="+anonID, nil)
		w := httptest.NewRecorder()

		server.fetchInbox(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", w.Code)
		}

		var resp map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Errorf("failed to unmarshal response: %v", err)
		}
		messages, ok := resp["messages"].([]any)
		if !ok || len(messages) != 1 {
			t.Errorf("expected 1 message, got %d", len(messages))
		}
	})
}
