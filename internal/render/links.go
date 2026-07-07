package render

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/net/html"
)

// RewriteLinks replaces all <a href> values with signed tracking redirect URLs.
// upsert receives the original URL and returns a stable link ID (from the DB).
func RewriteLinks(htmlStr string, tenantID, appID, campaignID, profileID, templateID, dispatchID string, upsert func(string) (string, error), secretKey []byte, baseTrackingURL string) (string, error) {
	doc, err := html.Parse(strings.NewReader(htmlStr))
	if err != nil {
		return "", err
	}

	var traverse func(*html.Node) error
	traverse = func(n *html.Node) error {
		if n.Type == html.ElementNode && n.Data == "a" {
			for i, attr := range n.Attr {
				if attr.Key == "href" {
					url := attr.Val
					if url == "" || strings.HasPrefix(url, "#") ||
						strings.HasPrefix(url, "javascript:") ||
						strings.HasPrefix(url, "mailto:") ||
						strings.HasPrefix(url, "tel:") {
						continue
					}
					linkID, err := upsert(url)
					if err != nil {
						return err
					}
					token, err := SignLinkToken(tenantID, appID, campaignID, profileID, linkID, templateID, dispatchID, url, secretKey)
					if err != nil {
						return err
					}
					n.Attr[i].Val = fmt.Sprintf("%s/r/%s", strings.TrimSuffix(baseTrackingURL, "/"), token)
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if err := traverse(c); err != nil {
				return err
			}
		}
		return nil
	}

	if err := traverse(doc); err != nil {
		return "", err
	}

	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// SignLinkToken creates a signed, base64-URL-encoded token.
// Payload: tenantID|appID|campaignID|profileID|linkID|templateID|dispatchID|url
func SignLinkToken(tenantID, appID, campaignID, profileID, linkID, templateID, dispatchID, url string, secretKey []byte) (string, error) {
	payload := strings.Join([]string{tenantID, appID, campaignID, profileID, linkID, templateID, dispatchID, url}, "|")
	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(payload))
	signature := mac.Sum(nil)
	full := fmt.Sprintf("%s.%s", payload, base64.RawURLEncoding.EncodeToString(signature))
	return base64.RawURLEncoding.EncodeToString([]byte(full)), nil
}

// VerifyLinkToken validates the token and returns its fields.
func VerifyLinkToken(token string, secretKey []byte) (tenantID, appID, campaignID, profileID, linkID, templateID, dispatchID, url string, err error) {
	fullBytes, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", "", "", "", "", "", "", "", err
	}
	fullStr := string(fullBytes)
	lastDot := strings.LastIndex(fullStr, ".")
	if lastDot == -1 {
		return "", "", "", "", "", "", "", "", errors.New("invalid token format")
	}
	payload := fullStr[:lastDot]
	signatureStr := fullStr[lastDot+1:]

	signature, err := base64.RawURLEncoding.DecodeString(signatureStr)
	if err != nil {
		return "", "", "", "", "", "", "", "", err
	}

	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(payload))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return "", "", "", "", "", "", "", "", errors.New("invalid signature")
	}

	parts := strings.Split(payload, "|")
	if len(parts) < 8 {
		return "", "", "", "", "", "", "", "", errors.New("invalid payload format")
	}
	url = strings.Join(parts[7:], "|")
	return parts[0], parts[1], parts[2], parts[3], parts[4], parts[5], parts[6], url, nil
}

// SignOpenToken creates a signed open-tracking token.
// Payload: tenantID|appID|campaignID|profileID|templateID|dispatchID
func SignOpenToken(tenantID, appID, campaignID, profileID, templateID, dispatchID string, secretKey []byte) (string, error) {
	payload := strings.Join([]string{tenantID, appID, campaignID, profileID, templateID, dispatchID}, "|")
	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(payload))
	signature := mac.Sum(nil)
	full := fmt.Sprintf("%s.%s", payload, base64.RawURLEncoding.EncodeToString(signature))
	return base64.RawURLEncoding.EncodeToString([]byte(full)), nil
}

// VerifyOpenToken validates the token and returns its fields.
func VerifyOpenToken(token string, secretKey []byte) (tenantID, appID, campaignID, profileID, templateID, dispatchID string, err error) {
	fullBytes, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return "", "", "", "", "", "", err
	}
	fullStr := string(fullBytes)
	lastDot := strings.LastIndex(fullStr, ".")
	if lastDot == -1 {
		return "", "", "", "", "", "", errors.New("invalid token format")
	}
	payload := fullStr[:lastDot]
	signatureStr := fullStr[lastDot+1:]

	signature, err := base64.RawURLEncoding.DecodeString(signatureStr)
	if err != nil {
		return "", "", "", "", "", "", err
	}

	mac := hmac.New(sha256.New, secretKey)
	mac.Write([]byte(payload))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return "", "", "", "", "", "", errors.New("invalid signature")
	}

	parts := strings.Split(payload, "|")
	if len(parts) != 6 {
		return "", "", "", "", "", "", errors.New("invalid payload format")
	}
	return parts[0], parts[1], parts[2], parts[3], parts[4], parts[5], nil
}
