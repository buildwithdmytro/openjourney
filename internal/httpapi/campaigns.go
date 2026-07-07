package httpapi

import (
	"errors"
	"net/http"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

func (s *Server) createCampaign(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	var camp domain.Campaign
	if err := decodeJSON(w, r, &camp); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	res, err := s.store.CreateCampaign(r.Context(), principal, camp)
	if err != nil {
		internalError(w, err, "create campaign", principal)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}

func (s *Server) getCampaign(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	res, err := s.store.GetCampaign(r.Context(), principal, id)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "campaign not found")
		return
	}
	if err != nil {
		internalError(w, err, "get campaign", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) updateCampaign(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	id := r.PathValue("id")
	var camp domain.Campaign
	if err := decodeJSON(w, r, &camp); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	camp.ID = id
	res, err := s.store.UpdateCampaign(r.Context(), principal, camp)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "campaign not found")
		return
	}
	if err != nil {
		internalError(w, err, "update campaign", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) listCampaigns(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	res, err := s.store.ListCampaigns(r.Context(), principal)
	if err != nil {
		internalError(w, err, "list campaigns", principal)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
