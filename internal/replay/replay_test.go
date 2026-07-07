package replay

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

func TestBuildMergesAnonymousAndKnownIdentity(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	state := Build([]domain.AcceptedEvent{
		{Type: "profile.updated", AnonymousID: "anon", OccurredAt: at,
			Payload: json.RawMessage(`{"attributes":{"first_name":"Ada"}}`)},
		{Type: "profile.updated", ExternalID: "user", AnonymousID: "anon", OccurredAt: at.Add(time.Second),
			Payload: json.RawMessage(`{"attributes":{"plan":"pro"}}`)},
		{Type: "consent.changed", ExternalID: "user", OccurredAt: at.Add(2 * time.Second),
			Payload: json.RawMessage(`{"channel":"EMAIL","state":"subscribed"}`)},
	})
	if len(state.Profiles) != 1 {
		t.Fatalf("profiles=%d", len(state.Profiles))
	}
	profile := state.Profiles[0]
	if profile.ExternalID != "user" || profile.AnonymousID != "anon" {
		t.Fatalf("identity=%+v", profile)
	}
	if profile.Attributes["first_name"] != "Ada" || profile.Attributes["plan"] != "pro" {
		t.Fatalf("attributes=%+v", profile.Attributes)
	}
	if profile.Consents["email:marketing"].State != "subscribed" {
		t.Fatalf("consents=%+v", profile.Consents)
	}
}

func TestBuildExplicitMergePreservesTargetWinsAndLatestConsent(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	state := Build([]domain.AcceptedEvent{
		{Type: "profile.updated", ExternalID: "source", AnonymousID: "source-browser", OccurredAt: at,
			Payload: json.RawMessage(`{"attributes":{"plan":"basic","source_only":true}}`)},
		{Type: "consent.changed", ExternalID: "source", OccurredAt: at.Add(time.Second),
			Payload: json.RawMessage(`{"channel":"email","topic":"marketing","state":"subscribed"}`)},
		{Type: "profile.updated", ExternalID: "target", AnonymousID: "target-browser", OccurredAt: at.Add(2 * time.Second),
			Payload: json.RawMessage(`{"attributes":{"plan":"enterprise","target_only":true}}`)},
		{Type: "consent.changed", ExternalID: "target", OccurredAt: at.Add(3 * time.Second),
			Payload: json.RawMessage(`{"channel":"email","topic":"marketing","state":"unsubscribed"}`)},
		{Type: "identity.merge", ExternalID: "target", OccurredAt: at.Add(4 * time.Second),
			Payload: json.RawMessage(`{"source_external_id":"source"}`)},
	})

	if len(state.Profiles) != 1 {
		t.Fatalf("profiles=%d", len(state.Profiles))
	}
	profile := state.Profiles[0]
	if profile.ExternalID != "target" || profile.AnonymousID != "target-browser" {
		t.Fatalf("identity=%+v", profile)
	}
	if profile.Attributes["plan"] != "enterprise" || profile.Attributes["source_only"] != true ||
		profile.Attributes["target_only"] != true {
		t.Fatalf("attributes=%+v", profile.Attributes)
	}
	if consent := profile.Consents["email:marketing"]; consent.State != "unsubscribed" {
		t.Fatalf("consent=%+v", consent)
	}
}

func TestBuildAnonymousKnownMergeChecksumIsStableForEquivalentEventOrder(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	anonymousFirst := []domain.AcceptedEvent{
		{Type: "profile.updated", AnonymousID: "browser-1", OccurredAt: at,
			Payload: json.RawMessage(`{"attributes":{"first_name":"Ada"}}`)},
		{Type: "profile.updated", ExternalID: "customer-1", AnonymousID: "browser-1", OccurredAt: at.Add(time.Second),
			Payload: json.RawMessage(`{"attributes":{"plan":"pro"}}`)},
	}
	knownFirst := []domain.AcceptedEvent{
		{Type: "profile.updated", ExternalID: "customer-1", AnonymousID: "browser-1", OccurredAt: at,
			Payload: json.RawMessage(`{"attributes":{"plan":"pro"}}`)},
		{Type: "profile.updated", AnonymousID: "browser-1", OccurredAt: at.Add(time.Second),
			Payload: json.RawMessage(`{"attributes":{"first_name":"Ada"}}`)},
	}

	left := Build(anonymousFirst)
	right := Build(knownFirst)
	if Checksum(left) != Checksum(right) {
		t.Fatalf("equivalent identity histories diverged:\nleft=%+v\nright=%+v", left, right)
	}
}

func TestBuildAliasDoesNotForkProfileOrChangeAttributes(t *testing.T) {
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	state := Build([]domain.AcceptedEvent{
		{Type: "profile.updated", ExternalID: "customer-1", OccurredAt: at,
			Payload: json.RawMessage(`{"attributes":{"plan":"pro"}}`)},
		{Type: "identity.alias", ExternalID: "customer-1", OccurredAt: at.Add(time.Second),
			Payload: json.RawMessage(`{"namespace":"email","value":"ada@example.test"}`)},
	})

	if len(state.Profiles) != 1 {
		t.Fatalf("profiles=%d", len(state.Profiles))
	}
	if profile := state.Profiles[0]; profile.ExternalID != "customer-1" || profile.Attributes["plan"] != "pro" {
		t.Fatalf("profile=%+v", profile)
	}
}
