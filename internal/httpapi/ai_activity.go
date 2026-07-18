package httpapi

import (
	"net/http"
	"strconv"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func (s *Server) listAIActivity(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}
	items, err := s.store.ListAIActivity(r.Context(), principal, limit)
	if err != nil {
		internalError(w, err, "list AI activity", principal)
		return
	}
	if items == nil {
		items = []domain.AIActivity{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"activities": items})
}
