package httpapi

import (
	"net/http"
)

func (s *Server) getOverview(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	overview, err := s.store.GetOverview(r.Context(), principal)
	if err != nil {
		internalError(w, err, "get overview", principal)
		return
	}
	writeJSON(w, http.StatusOK, overview)
}
