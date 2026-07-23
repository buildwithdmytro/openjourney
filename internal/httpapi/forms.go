package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	formdefinition "github.com/buildwithdmytro/openjourney/internal/forms"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"github.com/buildwithdmytro/openjourney/internal/publishing"
)

func (s *Server) createForm(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var input domain.Form
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	out, err := s.store.CreateForm(r.Context(), p, input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_form", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) listForms(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	out, err := s.store.ListForms(r.Context(), p)
	if err != nil {
		internalError(w, err, "list forms", p)
		return
	}
	if out == nil {
		out = []domain.Form{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"forms": out})
}

func (s *Server) getForm(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	out, err := s.store.GetForm(r.Context(), p, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "form not found")
		return
	}
	if err != nil {
		internalError(w, err, "get form", p)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) updateForm(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var input domain.Form
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	input.ID = r.PathValue("id")
	out, err := s.store.UpdateForm(r.Context(), p, input)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "form not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_form", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) publishForm(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	if p.ActorType != "user" || p.UserID == "" {
		writeError(w, http.StatusForbidden, "human_approval_required", "publishing requires an authenticated user")
		return
	}
	form, err := s.store.GetForm(r.Context(), p, r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "form not found")
		return
	}
	if err != nil {
		internalError(w, err, "get form for publish", p)
		return
	}
	definition, err := formdefinition.CanonicalizeDraft(form.Draft)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_form_schema", err.Error())
		return
	}
	out, err := publishing.Publish(r.Context(), p, form.ID, "forms", form.Draft, s.blobStore,
		func(_ json.RawMessage) ([]byte, error) { return definition, nil },
		func(ctx context.Context, principal domain.Principal, id, publisher, manifest string) (domain.FormVersion, error) {
			return s.store.PublishForm(ctx, principal, id, publisher, manifest, definition)
		})
	if errors.Is(err, publishing.ErrHumanActorRequired) {
		writeError(w, http.StatusForbidden, "human_approval_required", err.Error())
		return
	}
	if errors.Is(err, postgres.ErrSelfApproval) || errors.Is(err, publishing.ErrSelfApproval) {
		writeError(w, http.StatusForbidden, "self_approval_forbidden", err.Error())
		return
	}
	if errors.Is(err, postgres.ErrNotFound) {

		writeError(w, http.StatusNotFound, "not_found", "form not found")
		return
	}
	if err != nil {
		internalError(w, err, "publish form", p)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
