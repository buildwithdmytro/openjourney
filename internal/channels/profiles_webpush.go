package channels

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// WebPushConfig holds the VAPID credentials stored in SendingIdentity.Config.
type WebPushConfig struct {
	VAPIDPrivateRef string `json:"vapid_private_ref"` // env var name for private key
	VAPIDPublicRef  string `json:"vapid_public_ref"`  // env var name for public key
	VAPIDSubject    string `json:"vapid_subject"`     // subject line (mailto: or https:)
}

// WebPushProfile implements ProviderProfile for web push via VAPID (RFC 8292).
type WebPushProfile struct{}

func (p *WebPushProfile) BuildRequest(ctx context.Context, msg ports.RenderedMessage) (*http.Request, error) {
	var cfg WebPushConfig
	if len(msg.Identity.Config) > 0 && string(msg.Identity.Config) != "{}" {
		if err := json.Unmarshal(msg.Identity.Config, &cfg); err != nil {
			return nil, fmt.Errorf("webpush: invalid identity config: %w", err)
		}
	}

	if cfg.VAPIDPrivateRef == "" {
		return nil, errors.New("webpush: vapid_private_ref is required in identity config")
	}
	if cfg.VAPIDPublicRef == "" {
		return nil, errors.New("webpush: vapid_public_ref is required in identity config")
	}
	if cfg.VAPIDSubject == "" {
		return nil, errors.New("webpush: vapid_subject is required in identity config")
	}
	if msg.Endpoint == "" {
		return nil, errors.New("webpush: subscription endpoint is required")
	}

	// Resolve secrets from environment variables.
	privateKeyPEM := os.Getenv(cfg.VAPIDPrivateRef)
	if privateKeyPEM == "" {
		return nil, fmt.Errorf("webpush: environment variable %s not set", cfg.VAPIDPrivateRef)
	}

	publicKeyB64 := os.Getenv(cfg.VAPIDPublicRef)
	if publicKeyB64 == "" {
		return nil, fmt.Errorf("webpush: environment variable %s not set", cfg.VAPIDPublicRef)
	}

	// Parse the private key (P-256 ECDSA in PEM format or raw).
	privKey, err := parseVAPIDPrivateKey(privateKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("webpush: parse private key: %w", err)
	}

	// Build VAPID JWT.
	jwt, err := buildVAPIDJWT(privKey, cfg.VAPIDSubject)
	if err != nil {
		return nil, fmt.Errorf("webpush: build JWT: %w", err)
	}

	// POST to subscription endpoint with VAPID headers, no body.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, msg.Endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("webpush: create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Length", "0")
	req.Header.Set("TTL", "24") // 24 hours default TTL
	req.Header.Set("Authorization", fmt.Sprintf("vapid t=%s,k=%s", jwt, publicKeyB64))

	return req, nil
}

func (p *WebPushProfile) ParseResponse(resp *http.Response, body []byte) (string, error) {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// Success; generate a provider ID from status/location header if available.
		providerID := fmt.Sprintf("webpush-ok-%d", resp.StatusCode)
		if location := resp.Header.Get("Location"); location != "" {
			providerID = location
		}
		return providerID, nil
	}

	retryable := resp.StatusCode >= 500 || resp.StatusCode == http.StatusRequestTimeout

	return "", &DeliveryError{
		Err: fmt.Errorf("webpush: send failed %d", resp.StatusCode),
		Retryable: retryable,
	}
}

func (p *WebPushProfile) IsInvalidToken(resp *http.Response, body []byte) bool {
	// 404 Not Found → subscription endpoint no longer valid.
	// 410 Gone → subscription endpoint has been revoked.
	return resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone
}

// NewWebPushAdapter constructs a ports.ChannelAdapter for web push via VAPID.
func NewWebPushAdapter() ports.ChannelAdapter {
	return NewHTTPProviderAdapter(&WebPushProfile{}, "push")
}

// parseVAPIDPrivateKey parses a P-256 ECDSA private key in PEM or raw format.
// For now, assume raw base64url-encoded private key (as per VAPID spec).
func parseVAPIDPrivateKey(input string) (*ecdsa.PrivateKey, error) {
	// Try base64url decoding first (VAPID raw format).
	keyBytes, err := base64.RawURLEncoding.DecodeString(input)
	if err != nil {
		// Fall back to raw hex or other formats if needed.
		return nil, fmt.Errorf("decode base64url private key: %w", err)
	}

	if len(keyBytes) != 32 {
		return nil, fmt.Errorf("invalid private key length: expected 32 bytes, got %d", len(keyBytes))
	}

	// Construct P-256 ECDSA private key from the 32-byte value.
	privKey := &ecdsa.PrivateKey{
		PublicKey: ecdsa.PublicKey{
			Curve: elliptic.P256(),
		},
		D: new(big.Int).SetBytes(keyBytes),
	}
	// Derive public key coordinates.
	privKey.PublicKey.X, privKey.PublicKey.Y = elliptic.P256().ScalarBaseMult(keyBytes)

	return privKey, nil
}

// buildVAPIDJWT builds an RFC 8292 VAPID JWT (unsigned for now; signature added below).
func buildVAPIDJWT(privKey *ecdsa.PrivateKey, subject string) (string, error) {
	now := time.Now().UTC()
	exp := now.Add(12 * time.Hour)

	// VAPID JWT header (ES256 for P-256).
	header := map[string]string{
		"typ": "JWT",
		"alg": "ES256",
	}
	headerBytes, _ := json.Marshal(header)
	headerB64 := base64.RawURLEncoding.EncodeToString(headerBytes)

	// VAPID JWT claims.
	claims := map[string]interface{}{
		"sub": subject,
		"aud": "https://push.example.com", // Placeholder; browser ignores AUD for VAPID.
		"exp": exp.Unix(),
		"iat": now.Unix(),
	}
	claimsBytes, _ := json.Marshal(claims)
	claimsB64 := base64.RawURLEncoding.EncodeToString(claimsBytes)

	signingInput := headerB64 + "." + claimsB64

	// Sign with ECDSA-SHA256.
	hasher := sha256.New()
	hasher.Write([]byte(signingInput))
	digest := hasher.Sum(nil)

	r, s, err := ecdsa.Sign(rand.Reader, privKey, digest)
	if err != nil {
		return "", fmt.Errorf("sign JWT: %w", err)
	}

	// Encode (r, s) as fixed-length 64-byte concat (32 bytes each, big-endian).
	rBytes := r.Bytes()
	sBytes := s.Bytes()
	// Pad to 32 bytes if needed.
	rPadded := make([]byte, 32)
	sPadded := make([]byte, 32)
	copy(rPadded[32-len(rBytes):], rBytes)
	copy(sPadded[32-len(sBytes):], sBytes)
	sig := append(rPadded, sPadded...)

	sigB64 := base64.RawURLEncoding.EncodeToString(sig)
	return signingInput + "." + sigB64, nil
}
