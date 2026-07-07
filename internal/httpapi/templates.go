package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/render"
)

// 1×1 transparent GIF (43 bytes)
var transparentGIF = []byte{
	0x47, 0x49, 0x46, 0x38, 0x39, 0x61, 0x01, 0x00, 0x01, 0x00, 0x80, 0x00,
	0x00, 0xff, 0xff, 0xff, 0x00, 0x00, 0x00, 0x21, 0xf9, 0x04, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x2c, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00,
	0x00, 0x02, 0x02, 0x44, 0x01, 0x00, 0x3b,
}

func (s *Server) createSendingIdentity(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	var input domain.SendingIdentity
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := s.store.CreateSendingIdentity(r.Context(), principal, input)
	if err != nil {
		internalError(w, err, "create sending identity", principal)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) getSendingIdentity(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	res, err := s.store.GetSendingIdentity(r.Context(), principal, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "sending identity not found")
		return
	}
	if err != nil {
		internalError(w, err, "get sending identity", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) listSendingIdentities(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	res, err := s.store.ListSendingIdentities(r.Context(), principal)
	if err != nil {
		internalError(w, err, "list sending identities", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) createTemplate(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	var input domain.Template
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := s.store.CreateTemplate(r.Context(), principal, input)
	if err != nil {
		internalError(w, err, "create template", principal)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) getTemplate(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	res, err := s.store.GetTemplate(r.Context(), principal, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "template not found")
		return
	}
	if err != nil {
		internalError(w, err, "get template", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) updateTemplate(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	var input domain.Template
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	input.ID = id
	res, err := s.store.UpdateTemplate(r.Context(), principal, input)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "template not found")
		return
	}
	if err != nil {
		internalError(w, err, "update template", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) listTemplates(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	res, err := s.store.ListTemplates(r.Context(), principal)
	if err != nil {
		internalError(w, err, "list templates", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// previewTemplate renders the template body with Liquid using a sample profile.
func (s *Server) previewTemplate(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")

	tmpl, err := s.store.GetTemplate(r.Context(), principal, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "template not found")
		return
	}
	if err != nil {
		internalError(w, err, "preview template", principal)
		return
	}

	var input struct {
		ExternalID string `json:"external_id"`
	}
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}

	profile, _, err := s.store.GetProfile(r.Context(), principal, input.ExternalID)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "profile not found")
		return
	}
	if err != nil {
		internalError(w, err, "preview template: get profile", principal)
		return
	}

	var attrs map[string]any
	_ = json.Unmarshal(profile.Attributes, &attrs)
	if attrs == nil {
		attrs = map[string]any{}
	}
	vars := map[string]any{
		"profile": map[string]any{
			"id":          profile.ID,
			"external_id": profile.ExternalID,
			"attributes":  attrs,
		},
	}

	subjectTmpl := ""
	if tmpl.SubjectTemplate != nil {
		subjectTmpl = *tmpl.SubjectTemplate
	}
	htmlTmpl := ""
	if tmpl.HTMLTemplate != nil {
		htmlTmpl = *tmpl.HTMLTemplate
	}

	subject, err := render.Render(subjectTmpl, vars)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "render_error", fmt.Sprintf("subject: %v", err))
		return
	}
	body, err := render.Render(htmlTmpl, vars)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "render_error", fmt.Sprintf("body: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"subject": subject,
		"body":    body,
	})
}

// redirectLink handles GET /r/{token}: verifies the HMAC, emits link.clicked, then 302 to the original URL.
func (s *Server) redirectLink(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")
	tenantID, appID, campaignID, profileID, _, templateID, dispatchID, destURL, err := render.VerifyLinkToken(token, s.trackingSecretKey)
	if err != nil {
		http.Error(w, "invalid token", http.StatusBadRequest)
		return
	}

	profile, profErr := s.store.GetProfileByID(r.Context(), tenantID, appID, profileID)

	if profErr == nil && profile.ExternalID != "" {
		now := time.Now()
		_, _ = s.store.AcceptEvents(r.Context(), domain.Principal{
			TenantID: tenantID,
			AppID:    appID,
		}, []domain.Event{
			{
				Type:           "link.clicked",
				SchemaVersion:  1,
				ExternalID:     profile.ExternalID,
				IdempotencyKey: fmt.Sprintf("lc-%s-%s-%s", campaignID, dispatchID, token[:12]),
				OccurredAt:     now,
				Source:         "tracker",
				Payload: mustJSON(map[string]any{
					"template_id": templateID,
					"dispatch_id": dispatchID,
					"url":         destURL,
					"campaign_id": campaignID,
				}),
			},
		})
	}

	http.Redirect(w, r, destURL, http.StatusFound)
}

// openPixel handles GET /o/{token} (clients use /o/{token}.gif): verifies HMAC, emits email.opened, returns 1×1 GIF.
func (s *Server) openPixel(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSuffix(r.PathValue("token"), ".gif")
	tenantID, appID, campaignID, profileID, templateID, dispatchID, err := render.VerifyOpenToken(token, s.trackingSecretKey)
	if err == nil {
		profile, profErr := s.store.GetProfileByID(r.Context(), tenantID, appID, profileID)
		if profErr == nil && profile.ExternalID != "" {
			now := time.Now()
			_, _ = s.store.AcceptEvents(r.Context(), domain.Principal{
				TenantID: tenantID,
				AppID:    appID,
			}, []domain.Event{
				{
					Type:           "email.opened",
					SchemaVersion:  1,
					ExternalID:     profile.ExternalID,
					IdempotencyKey: fmt.Sprintf("eo-%s-%s-%s", campaignID, dispatchID, token[:12]),
					OccurredAt:     now,
					Source:         "tracker",
					Payload: mustJSON(map[string]any{
						"template_id": templateID,
						"dispatch_id": dispatchID,
						"campaign_id": campaignID,
					}),
				},
			})
		}
	}

	w.Header().Set("Content-Type", "image/gif")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(transparentGIF)
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
