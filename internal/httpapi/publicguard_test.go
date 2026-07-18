package httpapi

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"
)

func TestIPRateLimiterLimitAndRefill(t *testing.T) {
	l := NewIPRateLimiter(2, 2)
	now := time.Unix(100, 0)
	l.clock = func() time.Time { return now }
	if !l.Allow("203.0.113.1") || !l.Allow("203.0.113.1") || l.Allow("203.0.113.1") {
		t.Fatal("expected burst to allow two requests and reject the third")
	}
	now = now.Add(500 * time.Millisecond)
	if !l.Allow("203.0.113.1") {
		t.Fatal("expected one token after refill")
	}
}

func TestHoneypot(t *testing.T) {
	if !HoneypotEmpty(" \t") || HoneypotEmpty("bot") {
		t.Fatal("unexpected honeypot result")
	}
	r := httptest.NewRequest("POST", "/", nil)
	r.Form = map[string][]string{"website": {"spam"}}
	if HoneypotPassed(r, "website") {
		t.Fatal("filled honeypot passed")
	}
}

func TestFormTokenExpiryAndTamper(t *testing.T) {
	secret := []byte("test-secret")
	now := time.Unix(1000, 0)
	token, err := SignFormToken("form-1", 3, now.Add(time.Minute), secret)
	if err != nil {
		t.Fatal(err)
	}
	got, err := VerifyFormToken(token, "form-1", 3, secret, now)
	if err != nil || got.FormID != "form-1" || got.Version != 3 {
		t.Fatalf("verify token: %#v, %v", got, err)
	}
	if _, err := VerifyFormToken(token+"x", "form-1", 3, secret, now); !errors.Is(err, ErrInvalidFormToken) {
		t.Fatalf("tampered token error = %v", err)
	}
	if _, err := VerifyFormToken(token, "form-1", 3, secret, now.Add(time.Minute)); !errors.Is(err, ErrExpiredFormToken) {
		t.Fatalf("expired token error = %v", err)
	}
}

func TestClientIPDoesNotTrustHeaderByDefault(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "192.0.2.10:1234"
	r.Header.Set("X-Forwarded-For", "198.51.100.8")
	if got := ClientIP(r, false); got != "192.0.2.10" {
		t.Fatalf("untrusted client IP = %q", got)
	}
	if got := ClientIP(r, true); got != "198.51.100.8" {
		t.Fatalf("trusted client IP = %q", got)
	}
}

func TestNoopCaptcha(t *testing.T) {
	if err := (NoopCaptchaVerifier{}).Verify(context.Background(), CaptchaRequest{}); err != nil {
		t.Fatal(err)
	}
}
