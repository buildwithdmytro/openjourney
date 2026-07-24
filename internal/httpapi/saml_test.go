package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/beevik/etree"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/ports"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	dsig "github.com/russellhaering/goxmldsig"
	"github.com/stretchr/testify/require"
)

type samlMockStore struct {
	ports.Store
	providers map[string]domain.SAMLProvider
	users     map[string]domain.User
}

func newSAMLMockStore() *samlMockStore {
	return &samlMockStore{
		providers: make(map[string]domain.SAMLProvider),
		users:     make(map[string]domain.User),
	}
}

func (m *samlMockStore) CreateSAMLProvider(_ context.Context, p domain.Principal, input domain.SAMLProvider) (domain.SAMLProvider, error) {
	if input.IDPEntityID == "" || input.IDPSSOURL == "" || input.IDPCert == "" || input.SPEntityID == "" {
		return domain.SAMLProvider{}, fmt.Errorf("missing required fields")
	}
	input.ID = "sp-" + randID()
	input.TenantID = p.TenantID
	if input.Status == "" {
		input.Status = "active"
	}
	key := p.TenantID + ":" + input.IDPEntityID
	m.providers[key] = input
	return input, nil
}

func (m *samlMockStore) GetSAMLProvider(_ context.Context, tenantID, idpEntityID string) (domain.SAMLProvider, error) {
	key := tenantID + ":" + idpEntityID
	p, ok := m.providers[key]
	if !ok {
		return domain.SAMLProvider{}, postgres.ErrNotFound
	}
	return p, nil
}

func (m *samlMockStore) ListSAMLProviders(_ context.Context, tenantID string) ([]domain.SAMLProvider, error) {
	var res []domain.SAMLProvider
	for _, p := range m.providers {
		if p.TenantID == tenantID {
			res = append(res, p)
		}
	}
	if len(res) == 0 {
		return nil, postgres.ErrNotFound
	}
	return res, nil
}

func (m *samlMockStore) UpsertSAMLUserAndCreateSession(_ context.Context, tenantID, idpEntityID, nameID, email, displayName string) (domain.AuthSession, error) {
	pKey := tenantID + ":" + idpEntityID
	provider, ok := m.providers[pKey]
	if !ok || !provider.Enabled || provider.Status != "active" {
		return domain.AuthSession{}, postgres.ErrUnauthorized
	}

	uKey := tenantID + ":" + idpEntityID + ":" + nameID
	u, exists := m.users[uKey]
	if exists && u.DisabledAt != nil {
		return domain.AuthSession{}, postgres.ErrUnauthorized
	}
	if !exists {
		u = domain.User{
			ID:          "u-" + randID(),
			OIDCIssuer:  idpEntityID,
			OIDCSubject: nameID,
			Email:       email,
			DisplayName: displayName,
		}
		m.users[uKey] = u
	}

	token := "ojs_" + randID() + randID()
	return domain.AuthSession{
		AccessToken: token,
		TokenType:   "Bearer",
		ExpiresAt:   time.Now().Add(12 * time.Hour),
	}, nil
}

func generateSelfSignedCert(t *testing.T) (*rsa.PrivateKey, string) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: "Test SAML IdP",
		},
		NotBefore: time.Now().Add(-1 * time.Hour),
		NotAfter:  time.Now().Add(24 * time.Hour),
		KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	require.NoError(t, err)

	pemBytes := fmt.Sprintf("-----BEGIN CERTIFICATE-----\n%s\n-----END CERTIFICATE-----",
		base64.StdEncoding.EncodeToString(certBytes))
	return key, pemBytes
}

func randID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func createSignedSAMLResponse(t *testing.T, key *rsa.PrivateKey, certPEM string, idpEntityID, spEntityID, acsURL, nameID, email string) string {
	now := time.Now().UTC()
	issueInstant := now.Format(time.RFC3339)
	notOnOrAfter := now.Add(5 * time.Minute).Format(time.RFC3339)

	respXML := fmt.Sprintf(`<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="_%s" Version="2.0" IssueInstant="%s" Destination="%s">
	<saml:Issuer>%s</saml:Issuer>
	<samlp:Status>
		<samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/>
	</samlp:Status>
	<saml:Assertion ID="_%s" Version="2.0" IssueInstant="%s">
		<saml:Issuer>%s</saml:Issuer>
		<saml:Subject>
			<saml:NameID Format="urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress">%s</saml:NameID>
			<saml:SubjectConfirmation Method="urn:oasis:names:tc:SAML:2.0:cm:bearer">
				<saml:SubjectConfirmationData NotOnOrAfter="%s" Recipient="%s"/>
			</saml:SubjectConfirmation>
		</saml:Subject>
		<saml:Conditions NotBefore="%s" NotOnOrAfter="%s">
			<saml:AudienceRestriction>
				<saml:Audience>%s</saml:Audience>
			</saml:AudienceRestriction>
		</saml:Conditions>
		<saml:AuthnStatement AuthnInstant="%s">
			<saml:AuthnContext>
				<saml:AuthnContextClassRef>urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport</saml:AuthnContextClassRef>
			</saml:AuthnContext>
		</saml:AuthnStatement>
		<saml:AttributeStatement>
			<saml:Attribute Name="email">
				<saml:AttributeValue>%s</saml:AttributeValue>
			</saml:Attribute>
		</saml:AttributeStatement>
	</saml:Assertion>
</samlp:Response>`,
		randID(), issueInstant, acsURL, idpEntityID,
		randID(), issueInstant, idpEntityID,
		nameID, notOnOrAfter, acsURL,
		now.Add(-1*time.Minute).Format(time.RFC3339), notOnOrAfter, spEntityID,
		issueInstant, email,
	)

	doc := etree.NewDocument()
	require.NoError(t, doc.ReadFromString(respXML))

	certBlock, _ := pemDecode(certPEM)
	cert, err := x509.ParseCertificate(certBlock)
	require.NoError(t, err)

	keyStore := dsig.TLSCertKeyStore{
		Certificate: [][]byte{cert.Raw},
		PrivateKey:  key,
	}
	ctx := dsig.NewDefaultSigningContext(keyStore)

	signedEl, err := ctx.SignEnveloped(doc.Root())
	require.NoError(t, err)

	doc.SetRoot(signedEl)
	signedStr, err := doc.WriteToString()
	require.NoError(t, err)

	return base64.StdEncoding.EncodeToString([]byte(signedStr))
}

func createUnsignedSAMLResponse(idpEntityID, spEntityID, acsURL, nameID, email string) string {
	now := time.Now().UTC()
	issueInstant := now.Format(time.RFC3339)
	notOnOrAfter := now.Add(5 * time.Minute).Format(time.RFC3339)

	respXML := fmt.Sprintf(`<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="_%s" Version="2.0" IssueInstant="%s" Destination="%s">
	<saml:Issuer>%s</saml:Issuer>
	<samlp:Status>
		<samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/>
	</samlp:Status>
	<saml:Assertion ID="_%s" Version="2.0" IssueInstant="%s">
		<saml:Issuer>%s</saml:Issuer>
		<saml:Subject>
			<saml:NameID Format="urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress">%s</saml:NameID>
			<saml:SubjectConfirmation Method="urn:oasis:names:tc:SAML:2.0:cm:bearer">
				<saml:SubjectConfirmationData NotOnOrAfter="%s" Recipient="%s"/>
			</saml:SubjectConfirmation>
		</saml:Subject>
		<saml:Conditions NotBefore="%s" NotOnOrAfter="%s">
			<saml:AudienceRestriction>
				<saml:Audience>%s</saml:Audience>
			</saml:AudienceRestriction>
		</saml:Conditions>
		<saml:AuthnStatement AuthnInstant="%s">
			<saml:AuthnContext>
				<saml:AuthnContextClassRef>urn:oasis:names:tc:SAML:2.0:ac:classes:PasswordProtectedTransport</saml:AuthnContextClassRef>
			</saml:AuthnContext>
		</saml:AuthnStatement>
		<saml:AttributeStatement>
			<saml:Attribute Name="email">
				<saml:AttributeValue>%s</saml:AttributeValue>
			</saml:Attribute>
		</saml:AttributeStatement>
	</saml:Assertion>
</samlp:Response>`,
		randID(), issueInstant, acsURL, idpEntityID,
		randID(), issueInstant, idpEntityID,
		nameID, notOnOrAfter, acsURL,
		now.Add(-1*time.Minute).Format(time.RFC3339), notOnOrAfter, spEntityID,
		issueInstant, email,
	)

	return base64.StdEncoding.EncodeToString([]byte(respXML))
}

func pemDecode(pemStr string) ([]byte, error) {
	b64 := cleanPEMCert(pemStr)
	return base64.StdEncoding.DecodeString(b64)
}

func TestSAMLSSO_E2E(t *testing.T) {
	store := newSAMLMockStore()
	handler := New(store, 75)

	key, certPEM := generateSelfSignedCert(t)
	tenantID := randID()
	idpEntityID := "https://idp.example.com/metadata"
	spEntityID := "https://openjourney.io/saml/sp"

	// 1. Create SAML Provider
	_, err := store.CreateSAMLProvider(t.Context(), domain.Principal{TenantID: tenantID}, domain.SAMLProvider{
		IDPEntityID: idpEntityID,
		IDPSSOURL:   "https://idp.example.com/sso",
		IDPCert:     certPEM,
		SPEntityID:  spEntityID,
		Enabled:     true,
		Status:      "active",
	})
	require.NoError(t, err)

	acsURL := fmt.Sprintf("http://example.com/v1/auth/saml/%s/acs", tenantID)

	// 2. Metadata endpoint
	metaReq := httptest.NewRequest("GET", fmt.Sprintf("/v1/auth/saml/%s/metadata", tenantID), nil)
	metaReq.Host = "example.com"
	metaRec := httptest.NewRecorder()
	handler.ServeHTTP(metaRec, metaReq)
	require.Equal(t, http.StatusOK, metaRec.Code)
	require.Contains(t, metaRec.Body.String(), "<EntityDescriptor")

	// 3. Login redirect endpoint
	loginReq := httptest.NewRequest("GET", fmt.Sprintf("/v1/auth/saml/%s/login", tenantID), nil)
	loginReq.Host = "example.com"
	loginRec := httptest.NewRecorder()
	handler.ServeHTTP(loginRec, loginReq)
	require.Equal(t, http.StatusFound, loginRec.Code)
	require.Contains(t, loginRec.Header().Get("Location"), "https://idp.example.com/sso")

	// 4. Valid Signed Assertion ACS -> Authenticates & mints session
	validResponse := createSignedSAMLResponse(t, key, certPEM, idpEntityID, spEntityID, acsURL, "user1@example.com", "user1@example.com")
	formValues := url.Values{}
	formValues.Set("SAMLResponse", validResponse)

	acsReq := httptest.NewRequest("POST", fmt.Sprintf("/v1/auth/saml/%s/acs", tenantID), strings.NewReader(formValues.Encode()))
	acsReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	acsReq.Host = "example.com"
	acsRec := httptest.NewRecorder()
	handler.ServeHTTP(acsRec, acsReq)
	require.Equal(t, http.StatusOK, acsRec.Code, "ACS error: %s", acsRec.Body.String())

	var sess domain.AuthSession
	require.NoError(t, json.NewDecoder(acsRec.Body).Decode(&sess))
	require.NotEmpty(t, sess.AccessToken)
	require.True(t, strings.HasPrefix(sess.AccessToken, "ojs_"))

	// 5. Tampered Assertion -> Signature check fails (401 Unauthorized)
	decodedBytes, err := base64.StdEncoding.DecodeString(validResponse)
	require.NoError(t, err)
	tamperedBytes := bytes.Replace(decodedBytes, []byte("user1@example.com"), []byte("hacker@evil.com"), 1)
	tamperedResponse := base64.StdEncoding.EncodeToString(tamperedBytes)

	tamperedForm := url.Values{}
	tamperedForm.Set("SAMLResponse", tamperedResponse)
	tReq := httptest.NewRequest("POST", fmt.Sprintf("/v1/auth/saml/%s/acs", tenantID), strings.NewReader(tamperedForm.Encode()))
	tReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tReq.Host = "example.com"
	tRec := httptest.NewRecorder()
	handler.ServeHTTP(tRec, tReq)
	require.Equal(t, http.StatusUnauthorized, tRec.Code)

	// 6. Unsigned Assertion -> Rejected (401 Unauthorized)
	unsignedResponse := createUnsignedSAMLResponse(idpEntityID, spEntityID, acsURL, "user2@example.com", "user2@example.com")
	uForm := url.Values{}
	uForm.Set("SAMLResponse", unsignedResponse)
	uReq := httptest.NewRequest("POST", fmt.Sprintf("/v1/auth/saml/%s/acs", tenantID), strings.NewReader(uForm.Encode()))
	uReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	uReq.Host = "example.com"
	uRec := httptest.NewRecorder()
	handler.ServeHTTP(uRec, uReq)
	require.Equal(t, http.StatusUnauthorized, uRec.Code)

	// 7. Disabled Provider -> Refused (401 Unauthorized)
	pKey := tenantID + ":" + idpEntityID
	p := store.providers[pKey]
	p.Enabled = false
	store.providers[pKey] = p

	dReq := httptest.NewRequest("POST", fmt.Sprintf("/v1/auth/saml/%s/acs", tenantID), strings.NewReader(formValues.Encode()))
	dReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	dReq.Host = "example.com"
	dRec := httptest.NewRecorder()
	handler.ServeHTTP(dRec, dReq)
	require.Equal(t, http.StatusUnauthorized, dRec.Code)

	// 8. Disabled User -> Refused (401 Unauthorized)
	p.Enabled = true
	store.providers[pKey] = p

	uKey := tenantID + ":" + idpEntityID + ":user1@example.com"
	u := store.users[uKey]
	now := time.Now()
	u.DisabledAt = &now
	store.users[uKey] = u

	disReq := httptest.NewRequest("POST", fmt.Sprintf("/v1/auth/saml/%s/acs", tenantID), strings.NewReader(formValues.Encode()))
	disReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	disReq.Host = "example.com"
	disRec := httptest.NewRecorder()
	handler.ServeHTTP(disRec, disReq)
	require.Equal(t, http.StatusUnauthorized, disRec.Code)
}

func TestSAMLProviderScopeSplit(t *testing.T) {
	// Key with only scim:manage scope
	scimStore := &fakeStore{scopes: []string{"scim:manage"}}
	scimServer := New(scimStore, 75)

	body := strings.NewReader(`{"idp_entity_id":"https://idp.com","idp_sso_url":"https://idp.com/sso","idp_cert":"pem","sp_entity_id":"sp"}`)
	req := httptest.NewRequest("POST", "/v1/auth/saml/providers", body)
	req.Header.Set("Authorization", "Bearer test-key")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	scimServer.ServeHTTP(rec, req)
	require.Equal(t, http.StatusForbidden, rec.Code)

	getReq := httptest.NewRequest("GET", "/v1/auth/saml/providers", nil)
	getReq.Header.Set("Authorization", "Bearer test-key")
	getRec := httptest.NewRecorder()
	scimServer.ServeHTTP(getRec, getReq)
	require.Equal(t, http.StatusForbidden, getRec.Code)

	// Key with sso:manage scope
	ssoStore := &fakeStore{scopes: []string{"sso:manage"}}
	ssoServer := New(ssoStore, 75)

	body2 := strings.NewReader(`{"idp_entity_id":"https://idp.com","idp_sso_url":"https://idp.com/sso","idp_cert":"pem","sp_entity_id":"sp"}`)
	req2 := httptest.NewRequest("POST", "/v1/auth/saml/providers", body2)
	req2.Header.Set("Authorization", "Bearer test-key")
	req2.Header.Set("Content-Type", "application/json")
	rec2 := httptest.NewRecorder()
	ssoServer.ServeHTTP(rec2, req2)
	require.NotEqual(t, http.StatusForbidden, rec2.Code)

	getReq2 := httptest.NewRequest("GET", "/v1/auth/saml/providers", nil)
	getReq2.Header.Set("Authorization", "Bearer test-key")
	getRec2 := httptest.NewRecorder()
	ssoServer.ServeHTTP(getRec2, getReq2)
	require.NotEqual(t, http.StatusForbidden, getRec2.Code)
}

