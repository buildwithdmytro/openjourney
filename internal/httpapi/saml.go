package httpapi

import (
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/crewjam/saml"
)

func (s *Server) checkSAMLReplay(key string, exp time.Time) bool {
	s.samlReplayMu.Lock()
	defer s.samlReplayMu.Unlock()
	if s.samlReplayCache == nil {
		s.samlReplayCache = make(map[string]time.Time)
	}
	now := time.Now()
	for k, t := range s.samlReplayCache {
		if now.After(t) {
			delete(s.samlReplayCache, k)
		}
	}
	if t, ok := s.samlReplayCache[key]; ok && now.Before(t) {
		return false
	}
	s.samlReplayCache[key] = exp
	return true
}

func cleanPEMCert(pemStr string) string {
	pemStr = strings.ReplaceAll(pemStr, "-----BEGIN CERTIFICATE-----", "")
	pemStr = strings.ReplaceAll(pemStr, "-----END CERTIFICATE-----", "")
	pemStr = strings.ReplaceAll(pemStr, "\r", "")
	pemStr = strings.ReplaceAll(pemStr, "\n", "")
	pemStr = strings.ReplaceAll(pemStr, " ", "")
	return pemStr
}

func (s *Server) buildSP(r *http.Request, tenantID string, provider domain.SAMLProvider) *saml.ServiceProvider {
	certBase64 := cleanPEMCert(provider.IDPCert)
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	host := r.Host
	if host == "" {
		host = "localhost:8080"
	}

	acsURL := url.URL{Scheme: scheme, Host: host, Path: fmt.Sprintf("/v1/auth/saml/%s/acs", tenantID)}
	metaURL := url.URL{Scheme: scheme, Host: host, Path: fmt.Sprintf("/v1/auth/saml/%s/metadata", tenantID)}

	sp := &saml.ServiceProvider{
		EntityID:          provider.SPEntityID,
		AcsURL:            acsURL,
		MetadataURL:       metaURL,
		AllowIDPInitiated: true,
		IDPMetadata: &saml.EntityDescriptor{
			EntityID: provider.IDPEntityID,
			IDPSSODescriptors: []saml.IDPSSODescriptor{
				{
					SSODescriptor: saml.SSODescriptor{
						RoleDescriptor: saml.RoleDescriptor{
							KeyDescriptors: []saml.KeyDescriptor{
								{
									Use: "signing",
									KeyInfo: saml.KeyInfo{
										X509Data: saml.X509Data{
											X509Certificates: []saml.X509Certificate{
												{Data: certBase64},
											},
										},
									},
								},
							},
						},
					},
					SingleSignOnServices: []saml.Endpoint{
						{Binding: saml.HTTPRedirectBinding, Location: provider.IDPSSOURL},
						{Binding: saml.HTTPPostBinding, Location: provider.IDPSSOURL},
					},
				},
			},
		},
	}
	return sp
}

func (s *Server) samlMetadata(w http.ResponseWriter, r *http.Request) {
	tenantID := r.PathValue("tenant_id")
	if tenantID == "" {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	idpEntityID := r.URL.Query().Get("idp_entity_id")
	var provider domain.SAMLProvider
	var err error
	if idpEntityID != nilStr {
		provider, err = s.store.GetSAMLProvider(r.Context(), tenantID, idpEntityID)
	} else {
		providers, lErr := s.store.ListSAMLProviders(r.Context(), tenantID)
		if lErr != nil || len(providers) == 0 {
			err = postgres.ErrNotFound
		} else {
			provider = providers[0]
		}
	}
	if err != nil {
		http.Error(w, `{"error":"provider_not_found"}`, http.StatusNotFound)
		return
	}

	sp := s.buildSP(r, tenantID, provider)
	meta := sp.Metadata()
	data, err := xml.MarshalIndent(meta, "", "  ")
	if err != nil {
		http.Error(w, `{"error":"internal_error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(xml.Header + string(data)))
}

const nilStr = ""

func (s *Server) samlLogin(w http.ResponseWriter, r *http.Request) {
	tenantID := r.PathValue("tenant_id")
	if tenantID == "" {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	idpEntityID := r.URL.Query().Get("idp_entity_id")
	var provider domain.SAMLProvider
	var err error
	if idpEntityID != "" {
		provider, err = s.store.GetSAMLProvider(r.Context(), tenantID, idpEntityID)
	} else {
		providers, lErr := s.store.ListSAMLProviders(r.Context(), tenantID)
		if lErr != nil || len(providers) == 0 {
			err = postgres.ErrNotFound
		} else {
			provider = providers[0]
		}
	}
	if err != nil {
		http.Error(w, `{"error":"provider_not_found"}`, http.StatusNotFound)
		return
	}
	if !provider.Enabled || provider.Status != "active" {
		http.Error(w, `{"error":"provider_disabled"}`, http.StatusBadRequest)
		return
	}

	sp := s.buildSP(r, tenantID, provider)
	redirectURL, err := sp.MakeRedirectAuthenticationRequest(r.URL.Query().Get("RelayState"))
	if err != nil {
		http.Error(w, `{"error":"saml_request_failed"}`, http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

func (s *Server) samlACS(w http.ResponseWriter, r *http.Request) {
	tenantID := r.PathValue("tenant_id")
	if tenantID == "" {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}

	if err := r.ParseForm(); err != nil {
		http.Error(w, `{"error":"invalid_form"}`, http.StatusBadRequest)
		return
	}
	samlResp := r.FormValue("SAMLResponse")
	if samlResp == "" {
		http.Error(w, `{"error":"missing_saml_response"}`, http.StatusBadRequest)
		return
	}

	decoded, err := base64.StdEncoding.DecodeString(samlResp)
	if err != nil {
		decoded, err = base64.RawStdEncoding.DecodeString(samlResp)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_saml_response", "invalid base64 encoding")
			return
		}
	}

	// Unmarshal outer response XML to find Issuer entityID if not provided in query
	idpEntityID := r.URL.Query().Get("idp_entity_id")
	if idpEntityID == "" {
		var resp saml.Response
		if err := xml.Unmarshal(decoded, &resp); err == nil && resp.Issuer != nil {
			idpEntityID = resp.Issuer.Value
		}
	}

	var provider domain.SAMLProvider
	if idpEntityID != "" {
		provider, err = s.store.GetSAMLProvider(r.Context(), tenantID, idpEntityID)
	} else {
		providers, lErr := s.store.ListSAMLProviders(r.Context(), tenantID)
		if lErr != nil || len(providers) == 0 {
			err = postgres.ErrNotFound
		} else {
			provider = providers[0]
		}
	}
	if err != nil || !provider.Enabled || provider.Status != "active" {
		writeError(w, http.StatusUnauthorized, "provider_disabled", "provider not found or disabled")
		return
	}

	sp := s.buildSP(r, tenantID, provider)

	fullURL := *r.URL
	if fullURL.Scheme == "" {
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			fullURL.Scheme = "https"
		} else {
			fullURL.Scheme = "http"
		}
	}
	if fullURL.Host == "" {
		fullURL.Host = r.Host
		if fullURL.Host == "" {
			fullURL.Host = "localhost:8080"
		}
	}

	// Delegate XML signature verification and assertion parsing strictly to crewjam/saml library!
	assertion, err := sp.ParseXMLResponse(decoded, nil, fullURL)
	if err != nil {
		msg := err.Error()
		var ivr *saml.InvalidResponseError
		if errors.As(err, &ivr) && ivr.PrivateErr != nil {
			msg = fmt.Sprintf("%s: %v", err.Error(), ivr.PrivateErr)
		}
		writeError(w, http.StatusUnauthorized, "invalid_saml_assertion", msg)
		return
	}

	if assertion.ID == "" {
		writeError(w, http.StatusUnauthorized, "invalid_saml_assertion", "missing assertion ID in SAML assertion")
		return
	}

	exp := time.Now().Add(10 * time.Minute)
	if assertion.Conditions != nil && !assertion.Conditions.NotOnOrAfter.IsZero() {
		exp = assertion.Conditions.NotOnOrAfter
	}
	if maxExp := time.Now().Add(1 * time.Hour); exp.After(maxExp) {
		exp = maxExp
	}

	cacheKey := tenantID + ":" + provider.IDPEntityID + ":" + assertion.ID
	if !s.checkSAMLReplay(cacheKey, exp) {
		writeError(w, http.StatusUnauthorized, "invalid_saml_assertion", "SAML assertion has already been used")
		return
	}

	if assertion.Subject == nil || assertion.Subject.NameID == nil || assertion.Subject.NameID.Value == "" {
		writeError(w, http.StatusUnauthorized, "invalid_saml_assertion", "missing NameID in SAML assertion")
		return
	}
	nameID := assertion.Subject.NameID.Value

	email := nameID
	displayName := ""
	for _, statement := range assertion.AttributeStatements {
		for _, attr := range statement.Attributes {
			name := strings.ToLower(attr.Name)
			val := ""
			if len(attr.Values) > 0 {
				val = attr.Values[0].Value
			}
			if name == "email" || name == "mail" || strings.HasSuffix(name, "/emailaddress") {
				if val != "" {
					email = val
				}
			}
			if name == "displayname" || name == "name" || name == "cn" {
				if val != "" {
					displayName = val
				}
			}
		}
	}

	sess, err := s.store.UpsertSAMLUserAndCreateSession(r.Context(), tenantID, provider.IDPEntityID, nameID, email, displayName)
	if err != nil {
		if errors.Is(err, postgres.ErrUnauthorized) {
			writeError(w, http.StatusUnauthorized, "user_disabled", "user is disabled or unauthorized")
			return
		}
		writeError(w, http.StatusInternalServerError, "saml_session_error", err.Error())
		return
	}

	writeJSON(w, http.StatusOK, sess)
}

func (s *Server) createSAMLProvider(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	var req domain.SAMLProvider
	if err := decodeJSON(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	sp, err := s.store.CreateSAMLProvider(r.Context(), principal, req)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed_to_create_provider", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, sp)
}

func (s *Server) listSAMLProviders(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	providers, err := s.store.ListSAMLProviders(r.Context(), principal.TenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": providers})
}

