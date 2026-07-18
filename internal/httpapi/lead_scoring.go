package httpapi

import (
	"net/http"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/scoring"
)

// createLeadScoringModel is the acquisition-friendly authoring surface over
// M7's scoring registry. It creates a draft expression model and version; the
// existing human-gated publish endpoint makes it usable by scores.compute.
func (s *Server) createLeadScoringModel(w http.ResponseWriter, r *http.Request) {
	var input scoring.LeadScoreInput
	if err := decodeJSON(w, r, &input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_json", err.Error())
		return
	}
	definition, outputMax, err := scoring.BuildLeadScoreDefinition(input)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_lead_score", err.Error())
		return
	}

	p := principalFrom(r)
	model, err := s.store.CreateScoringModel(r.Context(), p, domain.ScoringModel{Name: input.Name, Kind: "expression"})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_lead_score", err.Error())
		return
	}
	version, err := s.store.CreateScoringModelVersion(r.Context(), p, domain.ScoringModelVersion{
		ScoringModelID: model.ID,
		ScoreName:      input.ScoreName,
		Definition:     definition,
		OutputMin:      0,
		OutputMax:      outputMax,
	})
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "invalid_lead_score", err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"model": model, "version": version})
}
