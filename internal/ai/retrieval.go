package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

var ErrProfileReadDenied = errors.New("AI profile retrieval requires profiles:read")

// RetrievalStore is the permission-aware, tenant-scoped seam used before a
// payload is assembled for a model request.
type RetrievalStore interface {
	GetProfile(context.Context, domain.Principal, string) (domain.Profile, []domain.Consent, error)
	ListFieldClassifications(context.Context, domain.Principal, string) ([]domain.FieldClassification, error)
}

// RetrievedProfile contains only fields which the derived AI principal is
// permitted to retrieve. Retrieval references deliberately contain identifiers
// and classification ids, never the retrieved values themselves.
type RetrievedProfile struct {
	ProfileID     string         `json:"profile_id"`
	Attributes    map[string]any `json:"attributes"`
	RetrievalRefs []RetrievalRef `json:"retrieval_refs"`
}

type RetrievalRef struct {
	Type             string `json:"type"`
	ID               string `json:"id"`
	FieldPath        string `json:"field_path,omitempty"`
	ClassificationID string `json:"classification_id,omitempty"`
}

// RetrieveProfile loads a profile through the caller's tenant/workspace scope
// and returns only explicitly classified, model-retrievable fields. Missing
// classifications are intentionally omitted (the default is redact).
func RetrieveProfile(ctx context.Context, store RetrievalStore, principal domain.Principal, externalID string) (RetrievedProfile, error) {
	if !principal.HasScope("profiles:read") {
		return RetrievedProfile{}, ErrProfileReadDenied
	}
	profile, _, err := store.GetProfile(ctx, principal, externalID)
	if err != nil {
		return RetrievedProfile{}, fmt.Errorf("retrieve profile: %w", err)
	}
	classifications, err := store.ListFieldClassifications(ctx, principal, "profile")
	if err != nil {
		return RetrievedProfile{}, fmt.Errorf("retrieve profile classifications: %w", err)
	}

	policies := make(map[string]domain.FieldClassification, len(classifications))
	refs := []RetrievalRef{{Type: "profile", ID: profile.ID}}
	for _, classification := range classifications {
		path := normalizeProfilePath(classification.FieldPath)
		if path == "" {
			continue
		}
		policies[path] = classification
		refs = append(refs, RetrievalRef{
			Type:             "profile_field",
			ID:               profile.ID,
			FieldPath:        path,
			ClassificationID: classification.ID,
		})
	}

	var raw map[string]any
	if len(profile.Attributes) > 0 {
		if err := json.Unmarshal(profile.Attributes, &raw); err != nil {
			return RetrievedProfile{}, fmt.Errorf("decode profile attributes: %w", err)
		}
	}
	filtered := filterRetrievedMap(raw, "", policies)
	if filtered == nil {
		filtered = map[string]any{}
	}
	return RetrievedProfile{ProfileID: profile.ID, Attributes: filtered, RetrievalRefs: refs}, nil
}

func normalizeProfilePath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.TrimPrefix(path, "attributes.")
	if path == "attributes" {
		return ""
	}
	return path
}

func filterRetrievedMap(input map[string]any, prefix string, policies map[string]domain.FieldClassification) map[string]any {
	if len(input) == 0 {
		return nil
	}
	keys := make([]string, 0, len(input))
	for key := range input {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make(map[string]any)
	for _, key := range keys {
		path := key
		if prefix != "" {
			path = prefix + "." + key
		}
		value := input[key]
		if policy, ok := policies[path]; ok {
			if fieldCanBeRetrieved(policy) {
				out[key] = value
			}
			continue
		}
		if nested, ok := value.(map[string]any); ok {
			if filtered := filterRetrievedMap(nested, path, policies); filtered != nil {
				out[key] = filtered
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func fieldCanBeRetrieved(c domain.FieldClassification) bool {
	return c.Classification != "restricted" && c.SendToModel != "deny"
}
