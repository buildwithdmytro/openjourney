package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidFormToken = errors.New("invalid form token")
	ErrExpiredFormToken = errors.New("expired form token")
	ErrInvalidInAppToken = errors.New("invalid in-app token")
	ErrExpiredInAppToken = errors.New("expired in-app token")
)

// IPRateLimiter is a deterministic, in-memory token bucket keyed by client IP.
// A request consumes one token. It is intended for the public HTTP edge; durable
// abuse controls can be layered in front of it when needed.
type IPRateLimiter struct {
	mu     sync.Mutex
	clock  func() time.Time
	rate   float64
	burst  float64
	bucket map[string]ipBucket
}

type ipBucket struct {
	tokens float64
	seen   time.Time
}

func NewIPRateLimiter(ratePerSecond float64, burst int) *IPRateLimiter {
	if ratePerSecond <= 0 {
		ratePerSecond = 1
	}
	if burst < 1 {
		burst = 1
	}
	return &IPRateLimiter{
		clock:  time.Now,
		rate:   ratePerSecond,
		burst:  float64(burst),
		bucket: make(map[string]ipBucket),
	}
}

// Allow reports whether one request from ip fits in its bucket.
func (l *IPRateLimiter) Allow(ip string) bool {
	if l == nil {
		return true
	}
	now := l.clock()
	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.bucket[ip]
	if !ok {
		b = ipBucket{tokens: l.burst, seen: now}
	} else {
		elapsed := now.Sub(b.seen).Seconds()
		if elapsed > 0 {
			b.tokens = minFloat(l.burst, b.tokens+elapsed*l.rate)
			b.seen = now
		}
	}
	if b.tokens < 1 {
		l.bucket[ip] = b
		return false
	}
	b.tokens--
	l.bucket[ip] = b
	return true
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// HoneypotEmpty is true only when the hidden bot-trap field is absent or blank.
func HoneypotEmpty(value string) bool { return strings.TrimSpace(value) == "" }

// HoneypotPassed reads a form field without making assumptions about the
// submitted payload's schema.
func HoneypotPassed(r *http.Request, field string) bool {
	return r != nil && HoneypotEmpty(r.FormValue(field))
}

type FormToken struct {
	FormID    string
	Version   int
	ExpiresAt time.Time
}

// SignFormToken creates a compact URL-safe token binding a form to its pinned
// version and expiry. The signed value is also suitable as a submission
// idempotency key.
func SignFormToken(formID string, version int, expiresAt time.Time, secret []byte) (string, error) {
	if strings.TrimSpace(formID) == "" || version < 1 || len(secret) == 0 || expiresAt.IsZero() {
		return "", ErrInvalidFormToken
	}
	payload := strings.Join([]string{formID, strconv.Itoa(version), strconv.FormatInt(expiresAt.Unix(), 10)}, "|")
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	full := payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(full)), nil
}

// VerifyFormToken validates the signature, expected form binding, version and
// expiry. now is injected so expiry behavior remains unit-testable.
func VerifyFormToken(token, expectedFormID string, expectedVersion int, secret []byte, now time.Time) (FormToken, error) {
	var out FormToken
	if token == "" || expectedFormID == "" || expectedVersion < 1 || len(secret) == 0 {
		return out, ErrInvalidFormToken
	}
	full, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return out, ErrInvalidFormToken
	}
	parts := strings.Split(string(full), ".")
	if len(parts) != 2 {
		return out, ErrInvalidFormToken
	}
	payload := parts[0]
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return out, ErrInvalidFormToken
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return out, ErrInvalidFormToken
	}
	fields := strings.Split(payload, "|")
	if len(fields) != 3 || fields[0] != expectedFormID {
		return out, ErrInvalidFormToken
	}
	version, err := strconv.Atoi(fields[1])
	if err != nil || version != expectedVersion {
		return out, ErrInvalidFormToken
	}
	expiresUnix, err := strconv.ParseInt(fields[2], 10, 64)
	if err != nil || expiresUnix <= 0 {
		return out, ErrInvalidFormToken
	}
	expiresAt := time.Unix(expiresUnix, 0)
	if !now.Before(expiresAt) {
		return out, ErrExpiredFormToken
	}
	return FormToken{FormID: fields[0], Version: version, ExpiresAt: expiresAt}, nil
}

type InAppToken struct {
	TenantID  string
	AppID     string
	Subject   string
	ExpiresAt time.Time
}

// SignInAppToken creates a compact URL-safe token binding a subject (external_id)
// to a tenant and app for known-subject inbox access.
func SignInAppToken(tenantID, appID, subject string, expiresAt time.Time, secret []byte) (string, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(appID) == "" || strings.TrimSpace(subject) == "" || len(secret) == 0 || expiresAt.IsZero() {
		return "", ErrInvalidInAppToken
	}
	payload := strings.Join([]string{tenantID, appID, subject, strconv.FormatInt(expiresAt.Unix(), 10)}, "|")
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	full := payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(full)), nil
}

// VerifyInAppToken validates the signature, expected tenant/app binding, and expiry.
// now is injected so expiry behavior remains unit-testable.
func VerifyInAppToken(token, expectedTenantID, expectedAppID string, secret []byte, now time.Time) (InAppToken, error) {
	var out InAppToken
	if token == "" || strings.TrimSpace(expectedTenantID) == "" || strings.TrimSpace(expectedAppID) == "" || len(secret) == 0 {
		return out, ErrInvalidInAppToken
	}
	full, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return out, ErrInvalidInAppToken
	}
	parts := strings.Split(string(full), ".")
	if len(parts) != 2 {
		return out, ErrInvalidInAppToken
	}
	payload := parts[0]
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return out, ErrInvalidInAppToken
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(payload))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return out, ErrInvalidInAppToken
	}
	fields := strings.Split(payload, "|")
	if len(fields) != 4 || fields[0] != expectedTenantID || fields[1] != expectedAppID {
		return out, ErrInvalidInAppToken
	}
	subject := fields[2]
	if strings.TrimSpace(subject) == "" {
		return out, ErrInvalidInAppToken
	}
	expiresUnix, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil || expiresUnix <= 0 {
		return out, ErrInvalidInAppToken
	}
	expiresAt := time.Unix(expiresUnix, 0)
	if !now.Before(expiresAt) {
		return out, ErrExpiredInAppToken
	}
	return InAppToken{TenantID: fields[0], AppID: fields[1], Subject: subject, ExpiresAt: expiresAt}, nil
}

type CaptchaRequest struct {
	Token    string
	RemoteIP string
}

// CaptchaVerifier is deliberately pluggable; the v1 default is a no-op and
// deployments may inject a provider-backed verifier.
type CaptchaVerifier interface {
	Verify(context.Context, CaptchaRequest) error
}

type NoopCaptchaVerifier struct{}

func (NoopCaptchaVerifier) Verify(context.Context, CaptchaRequest) error { return nil }

// ClientIP uses forwarding headers only when the deployment explicitly trusts
// its proxy. Otherwise RemoteAddr is authoritative.
func ClientIP(r *http.Request, trustedProxy bool) string {
	if r == nil {
		return ""
	}
	if trustedProxy {
		if forwarded := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-For"), ",")[0]); forwarded != "" {
			return forwarded
		}
		if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
			return real
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func (t FormToken) String() string {
	return fmt.Sprintf("%s@%d", t.FormID, t.Version)
}
