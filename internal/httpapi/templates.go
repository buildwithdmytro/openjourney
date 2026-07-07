package httpapi

import (
	"errors"
	"net/http"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

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
