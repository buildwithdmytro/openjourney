package ai

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type retrievalStore struct {
	profile         domain.Profile
	classifications []domain.FieldClassification
}

func (s retrievalStore) GetProfile(context.Context, domain.Principal, string) (domain.Profile, []domain.Consent, error) {
	return s.profile, nil, nil
}
func (s retrievalStore) ListFieldClassifications(context.Context, domain.Principal, string) ([]domain.FieldClassification, error) {
	return s.classifications, nil
}

func TestRetrieveProfileOmitsUnauthorizedFields(t *testing.T) {
	store := retrievalStore{
		profile: domain.Profile{ID: "profile-1", Attributes: json.RawMessage(`{"email":"ada@example.com","plan":"pro","ssn":"123-45-6789","address":{"city":"Berlin"}}`)},
		classifications: []domain.FieldClassification{
			{ID: "fc-email", FieldPath: "email", Classification: "confidential", SendToModel: "redact"},
			{ID: "fc-plan", FieldPath: "plan", Classification: "internal", SendToModel: "allow"},
			{ID: "fc-ssn", FieldPath: "attributes.ssn", Classification: "restricted", SendToModel: "deny"},
			{ID: "fc-city", FieldPath: "attributes.address.city", Classification: "public", SendToModel: "allow"},
		},
	}
	got, err := RetrieveProfile(context.Background(), &store, domain.Principal{Scopes: []string{"profiles:read"}}, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got.Attributes["ssn"]; ok {
		t.Fatal("restricted field was returned")
	}
	if _, ok := got.Attributes["email"]; !ok {
		t.Fatal("classified field was unexpectedly omitted")
	}
	if _, ok := got.Attributes["plan"]; !ok {
		t.Fatal("allowed field was unexpectedly omitted")
	}
	address, ok := got.Attributes["address"].(map[string]any)
	if !ok || address["city"] != "Berlin" {
		t.Fatalf("nested classified field missing: %#v", got.Attributes["address"])
	}
	if _, ok := got.Attributes["unclassified"]; ok {
		t.Fatal("unclassified field was returned")
	}
	if len(got.RetrievalRefs) != 5 {
		t.Fatalf("unexpected retrieval refs: %#v", got.RetrievalRefs)
	}
}

func TestRetrieveProfileRequiresProfileReadScope(t *testing.T) {
	_, err := RetrieveProfile(context.Background(), retrievalStore{}, domain.Principal{}, "ada")
	if !errors.Is(err, ErrProfileReadDenied) {
		t.Fatalf("expected profile read denial, got %v", err)
	}
}
