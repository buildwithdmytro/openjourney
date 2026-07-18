package httpapi

import (
	"net/http"
	"strconv"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

// listExtensionActivity returns the immutable invocation audit together with
// the current operational health state. The store applies both tenant and
// workspace predicates to the activity query.
func (s *Server) listExtensionActivity(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	limit := 100
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			limit = parsed
		}
	}

	extensionID := r.PathValue("id")
	activities, err := s.store.ListExtensionActivities(r.Context(), principal, extensionID, limit)
	if err != nil {
		internalError(w, err, "list extension activity", principal)
		return
	}
	health, err := s.store.GetExtensionHealth(r.Context(), principal, extensionID)
	if err != nil {
		internalError(w, err, "get extension health", principal)
		return
	}
	if activities == nil {
		activities = []domain.ExtensionActivity{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"activities": activities, "health": health})
}
