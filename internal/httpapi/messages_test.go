package httpapi

import (
	"testing"
	"time"
)

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
