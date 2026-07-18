package httpapi

import (
	"errors"
	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
	"net/http"
)

func (s *Server) createCompany(w http.ResponseWriter, r *http.Request) {
	var in struct {
		domain.Company
		Members []domain.CompanyMember `json:"members"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, 400, "invalid_json", err.Error())
		return
	}
	out, err := s.store.CreateCompany(r.Context(), principalFrom(r), in.Company, in.Members)
	if err != nil {
		writeError(w, 422, "invalid_company", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, out)
}
func (s *Server) getCompany(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.GetCompany(r.Context(), principalFrom(r), r.PathValue("id"))
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, 404, "not_found", "company not found")
		return
	}
	if err != nil {
		internalError(w, err, "get company", principalFrom(r))
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) listCompanies(w http.ResponseWriter, r *http.Request) {
	out, err := s.store.ListCompanies(r.Context(), principalFrom(r))
	if err != nil {
		internalError(w, err, "list companies", principalFrom(r))
		return
	}
	if out == nil {
		out = []domain.Company{}
	}
	writeJSON(w, 200, map[string]any{"companies": out})
}
func (s *Server) updateCompany(w http.ResponseWriter, r *http.Request) {
	var in struct {
		domain.Company
		Members []domain.CompanyMember `json:"members"`
	}
	if err := decodeJSON(w, r, &in); err != nil {
		writeError(w, 400, "invalid_json", err.Error())
		return
	}
	in.ID = r.PathValue("id")
	out, err := s.store.UpdateCompany(r.Context(), principalFrom(r), in.Company, in.Members)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, 404, "not_found", "company not found")
		return
	}
	if err != nil {
		writeError(w, 422, "invalid_company", err.Error())
		return
	}
	writeJSON(w, 200, out)
}
