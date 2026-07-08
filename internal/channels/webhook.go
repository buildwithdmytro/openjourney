package channels

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
)

// WebhookConfig specifies the configuration payload stored on the SendingIdentity.
type WebhookConfig struct {
	Endpoint   string `json:"endpoint"`
	HMACSecret string `json:"hmac_secret,omitempty"`
}

// WebhookAdapter implements ports.ChannelAdapter for HTTP Webhook notifications.
type WebhookAdapter struct {
	client *http.Client
}

// NewWebhookAdapter initializes a WebhookAdapter with custom secure transport.
func NewWebhookAdapter() *WebhookAdapter {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}

			// Perform lookup over secure DNS resolver
			ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
			if err != nil {
				return nil, err
			}

			if len(ips) == 0 {
				return nil, fmt.Errorf("no IP addresses resolved for host %s", host)
			}

			// Validate all resolved IPs
			for _, ip := range ips {
				if IsPrivateIP(ip) {
					return nil, fmt.Errorf("forbidden socket dial to private IP range: %s", ip.String())
				}
			}

			// Dial specifically using the first verified IP to protect against DNS rebinding
			dialer := &net.Dialer{
				Timeout:   5 * time.Second,
				KeepAlive: 30 * time.Second,
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(ips[0].String(), port))
		},
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   5 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("redirect limit exceeded")
			}
			// Secure redirect targets by pre-validating target URL host
			if err := IsSafeURL(req.URL.String()); err != nil {
				return fmt.Errorf("redirect SSRF safeguard block: %w", err)
			}
			return nil
		},
	}

	return &WebhookAdapter{
		client: client,
	}
}

// Send executes secure outbound HTTP POST webhook with HMAC signing and retries.
func (w *WebhookAdapter) Send(ctx context.Context, msg ports.RenderedMessage) (string, error) {
	if msg.Identity.Channel != "webhook" {
		return "", &DeliveryError{Err: fmt.Errorf("invalid channel for Webhook: %s", msg.Identity.Channel), Retryable: false}
	}

	var webCfg WebhookConfig
	if len(msg.Identity.Config) > 0 && string(msg.Identity.Config) != "{}" {
		if err := json.Unmarshal(msg.Identity.Config, &webCfg); err != nil {
			return "", &DeliveryError{Err: fmt.Errorf("failed to parse Webhook config: %w", err), Retryable: false}
		}
	}

	endpoint := webCfg.Endpoint
	if endpoint == "" {
		endpoint = msg.Endpoint
	}

	if endpoint == "" {
		return "", &DeliveryError{Err: errors.New("empty webhook endpoint"), Retryable: false}
	}

	// SSRF validate URL string before making request
	if err := IsSafeURL(endpoint); err != nil {
		return "", &DeliveryError{Err: fmt.Errorf("SSRF safety validation failed: %w", err), Retryable: false}
	}

	bodyBytes := []byte(msg.Body)
	req, err := http.NewRequestWithContext(ctx, "POST", endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", &DeliveryError{Err: fmt.Errorf("failed to create request: %w", err), Retryable: false}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "OpenJourney-Webhook-Adapter/1.0")

	// Sign payload with HMAC if secret is configured
	if webCfg.HMACSecret != "" {
		mac := hmac.New(sha256.New, []byte(webCfg.HMACSecret))
		mac.Write(bodyBytes)
		signature := hex.EncodeToString(mac.Sum(nil))
		req.Header.Set("X-Signature", signature)
	}

	// Retry loop with exponential backoff: 100ms -> 200ms -> 400ms
	var resp *http.Response
	retryDelay := 100 * time.Millisecond
	maxAttempts := 3

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// Reset request body reader if retrying
		if attempt > 1 {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		resp, err = w.client.Do(req)
		if err == nil {
			// Check for transient server errors (5xx)
			if resp.StatusCode >= 500 {
				resp.Body.Close()
				err = fmt.Errorf("server responded with status code %d", resp.StatusCode)
			} else {
				break
			}
		}

		if attempt < maxAttempts {
			select {
			case <-ctx.Done():
				return "", &DeliveryError{Err: ctx.Err(), Retryable: true}
			case <-time.After(retryDelay):
				retryDelay *= 2
			}
		}
	}

	if err != nil {
		return "", w.mapError(err)
	}

	defer resp.Body.Close()

	// 2xx and 3xx codes are considered successful deliveries, 4xx is a permanent failure
	if resp.StatusCode >= 400 {
		return "", &DeliveryError{Err: fmt.Errorf("webhook delivery failed with status: %d", resp.StatusCode), Retryable: false}
	}

	return fmt.Sprintf("webhook-success-%d", resp.StatusCode), nil
}

// ValidateConfig verifies that the webhook target is completely and securely configured.
func (w *WebhookAdapter) ValidateConfig(iden domain.SendingIdentity) error {
	if iden.Channel != "webhook" {
		return fmt.Errorf("Webhook channel must be webhook, got: %s", iden.Channel)
	}

	var webCfg WebhookConfig
	if len(iden.Config) > 0 && string(iden.Config) != "{}" {
		if err := json.Unmarshal(iden.Config, &webCfg); err != nil {
			return fmt.Errorf("invalid webhook config json: %w", err)
		}
	}

	if webCfg.Endpoint == "" {
		return errors.New("webhook endpoint is required")
	}

	if err := IsSafeURL(webCfg.Endpoint); err != nil {
		return fmt.Errorf("unacceptable webhook endpoint: %w", err)
	}

	return nil
}

func (w *WebhookAdapter) mapError(err error) error {
	if err == nil {
		return nil
	}

	retryable := false
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() || netErr.Temporary() {
			retryable = true
		}
	} else {
		errStr := strings.ToLower(err.Error())
		if strings.Contains(errStr, "timeout") ||
			strings.Contains(errStr, "temporary") ||
			strings.Contains(errStr, "connection refused") ||
			strings.Contains(errStr, "connection reset") {
			retryable = true
		}
	}

	return &DeliveryError{
		Err:       err,
		Retryable: retryable,
	}
}

// IsPrivateIP returns true if the IP is in private, loopback, link-local, unspecified, or multicast ranges.
func IsPrivateIP(ip net.IP) bool {
	if ip == nil {
		return true
	}

	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return true
	}

	// IPv4 RFC 1918 check
	if ip4 := ip.To4(); ip4 != nil {
		// 10.0.0.0/8
		if ip4[0] == 10 {
			return true
		}
		// 172.16.0.0/12
		if ip4[0] == 172 && ip4[1] >= 16 && ip4[1] <= 31 {
			return true
		}
		// 192.168.0.0/16
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		// 100.64.0.0/10 (Carrier-Grade NAT / CGNAT RFC 6598)
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return true
		}
		return false
	}

	// IPv6 RFC 4193 Unique Local Address range fc00::/7
	if len(ip) == 16 {
		if (ip[0] & 0xfe) == 0xfc {
			return true
		}
	}

	return false
}

// IsSafeURL validates that the target URL string exists and does not resolve to local or private networks.
func IsSafeURL(targetURL string) error {
	parsed, err := url.Parse(targetURL)
	if err != nil {
		return err
	}

	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("unsupported protocol scheme: %s", parsed.Scheme)
	}

	host := parsed.Hostname()
	// Special fast-path for literal IPs
	if ip := net.ParseIP(host); ip != nil {
		if IsPrivateIP(ip) {
			return fmt.Errorf("forbidding private IP address access: %s", host)
		}
		return nil
	}

	// Perform host resolution
	ips, err := net.LookupIP(host)
	if err != nil {
		return fmt.Errorf("dns lookup failed: %w", err)
	}

	if len(ips) == 0 {
		return fmt.Errorf("host did not resolve to any addresses: %s", host)
	}

	for _, ip := range ips {
		if IsPrivateIP(ip) {
			return fmt.Errorf("host resolves to a forbidden private address: %s", ip.String())
		}
	}

	return nil
}
