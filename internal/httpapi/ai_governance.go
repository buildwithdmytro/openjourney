package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

func (s *Server) listAIProviderConfigs(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	items, err := s.store.ListAIProviderConfigs(r.Context(), p)
	if err != nil {
		internalError(w, err, "list AI provider configs", p)
		return
	}
	if items == nil {
		items = []domain.AIProviderConfig{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"providers": items})
}

func (s *Server) createAIProviderConfig(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var input domain.AIProviderConfig
	if err := decodeJSON(w, r, &input); err != nil {
		return
	}
	out, err := s.store.CreateAIProviderConfig(r.Context(), p, input)
	if err != nil {
		internalError(w, err, "create AI provider config", p)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) updateAIProviderConfig(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var input domain.AIProviderConfig
	if err := decodeJSON(w, r, &input); err != nil {
		return
	}
	input.ID = r.PathValue("id")
	out, err := s.store.UpdateAIProviderConfig(r.Context(), p, input)
	if err != nil {
		internalError(w, err, "update AI provider config", p)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteAIProviderConfig(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	if err := s.store.DeleteAIProviderConfig(r.Context(), p, r.PathValue("id")); err != nil {
		internalError(w, err, "delete AI provider config", p)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getAIBudget(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	period := time.Now().UTC().Format("2006-01")
	usage, err := s.store.GetAIBudgetUsage(r.Context(), p.TenantID, p.WorkspaceID, period)
	if err != nil {
		internalError(w, err, "get AI budget", p)
		return
	}
	config, err := s.store.GetDefaultAIProviderConfig(r.Context(), p)
	if errors.Is(err, postgres.ErrNotFound) {
		writeJSON(w, http.StatusOK, map[string]any{"usage": usage, "monthly_budget_cents": int64(0)})
		return
	}
	if err != nil {
		internalError(w, err, "get AI budget config", p)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"usage": usage, "monthly_budget_cents": config.MonthlyBudgetCents})
}

func (s *Server) listFieldClassifications(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	items, err := s.store.ListFieldClassifications(r.Context(), p, r.URL.Query().Get("entity_type"))
	if err != nil {
		internalError(w, err, "list field classifications", p)
		return
	}
	if items == nil {
		items = []domain.FieldClassification{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"classifications": items})
}

func (s *Server) createFieldClassification(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var input domain.FieldClassification
	if err := decodeJSON(w, r, &input); err != nil {
		return
	}
	out, err := s.store.CreateFieldClassification(r.Context(), p, input)
	if err != nil {
		internalError(w, err, "create field classification", p)
		return
	}
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) updateFieldClassification(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	var input domain.FieldClassification
	if err := decodeJSON(w, r, &input); err != nil {
		return
	}
	input.ID = r.PathValue("id")
	out, err := s.store.UpdateFieldClassification(r.Context(), p, input)
	if err != nil {
		internalError(w, err, "update field classification", p)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteFieldClassification(w http.ResponseWriter, r *http.Request) {
	p := principalFrom(r)
	if err := s.store.DeleteFieldClassification(r.Context(), p, r.PathValue("id")); err != nil {
		internalError(w, err, "delete field classification", p)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
