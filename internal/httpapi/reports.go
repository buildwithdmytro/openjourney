package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/postgres"
)

// parseReportQuery extracts and validates a ReportQuery from request query parameters.
// Returns a zero ReportQuery if no parameters are provided (backward-compatible).
// Returns HTTP 422 if granularity, dimensions, or filters are invalid.
func parseReportQuery(w http.ResponseWriter, r *http.Request) (domain.ReportQuery, bool) {
	query := domain.ReportQuery{
		Dimensions: []string{},
		Filters:    make(map[string]string),
	}

	q := r.URL.Query()

	if start := strings.TrimSpace(q.Get("start")); start != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, start); err == nil {
			query.Start = parsed
		} else if parsed, err := time.Parse(time.RFC3339, start); err == nil {
			query.Start = parsed
		} else {
			writeError(w, http.StatusUnprocessableEntity, "invalid_query", "start must be RFC3339 timestamp")
			return query, false
		}
	}

	if end := strings.TrimSpace(q.Get("end")); end != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, end); err == nil {
			query.End = parsed
		} else if parsed, err := time.Parse(time.RFC3339, end); err == nil {
			query.End = parsed
		} else {
			writeError(w, http.StatusUnprocessableEntity, "invalid_query", "end must be RFC3339 timestamp")
			return query, false
		}
	}

	if granularity := strings.TrimSpace(q.Get("granularity")); granularity != "" {
		query.Granularity = granularity
	}

	if dims := strings.TrimSpace(q.Get("dimensions")); dims != "" {
		query.Dimensions = strings.Split(dims, ",")
		for i, dim := range query.Dimensions {
			query.Dimensions[i] = strings.TrimSpace(dim)
		}
	}

	for key, vals := range q {
		if strings.HasPrefix(key, "filter_") {
			filterKey := strings.TrimPrefix(key, "filter_")
			if !domain.AllowedFilters[filterKey] {
				writeError(w, http.StatusUnprocessableEntity, "invalid_query", "filter must be one of: channel, variant, node, provider")
				return query, false
			}
			if len(vals) > 0 && vals[0] != "" {
				query.Filters[filterKey] = vals[0]
			}
		}
	}

	if err := query.ValidateGranularity(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_query", err.Error())
		return query, false
	}

	if err := query.ValidateDimensions(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_query", err.Error())
		return query, false
	}

	if err := query.ValidateFilters(); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_query", err.Error())
		return query, false
	}

	return query, true
}

func (s *Server) getCampaignReport(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	query, ok := parseReportQuery(w, r)
	if !ok {
		return
	}

	report, err := s.store.CampaignReport(r.Context(), principal, r.PathValue("id"), query)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "campaign report not found")
		return
	}
	if err != nil {
		internalError(w, err, "get campaign report", principal)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) getJourneyReport(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	query, ok := parseReportQuery(w, r)
	if !ok {
		return
	}

	report, err := s.store.JourneyReport(r.Context(), principal, r.PathValue("id"), query)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "journey report not found")
		return
	}
	if err != nil {
		internalError(w, err, "get journey report", principal)
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) getExperimentReport(w http.ResponseWriter, r *http.Request) {
	principal := principalFrom(r)
	query, ok := parseReportQuery(w, r)
	if !ok {
		return
	}

	report, err := s.store.ExperimentReport(r.Context(), principal, r.PathValue("id"), query)
	if errors.Is(err, postgres.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "experiment report not found")
		return
	}
	if err != nil {
		internalError(w, err, "get experiment report", principal)
		return
	}
	writeJSON(w, http.StatusOK, report)
}
