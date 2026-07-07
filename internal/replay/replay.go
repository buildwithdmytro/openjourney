package replay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
)

type State struct {
	Profiles []CanonicalProfile `json:"profiles"`
}

type CanonicalProfile struct {
	ExternalID  string                      `json:"external_id,omitempty"`
	AnonymousID string                      `json:"anonymous_id,omitempty"`
	Attributes  map[string]any              `json:"attributes"`
	Consents    map[string]CanonicalConsent `json:"consents"`
}

type CanonicalConsent struct {
	Channel    string    `json:"channel"`
	Topic      string    `json:"topic"`
	State      string    `json:"state"`
	OccurredAt time.Time `json:"occurred_at"`
}

func Build(events []domain.AcceptedEvent) State {
	var profiles []*CanonicalProfile
	byExternal := map[string]*CanonicalProfile{}
	byAnonymous := map[string]*CanonicalProfile{}
	ensure := func(event domain.AcceptedEvent) *CanonicalProfile {
		var external, anonymous *CanonicalProfile
		if event.ExternalID != "" {
			external = byExternal[event.ExternalID]
		}
		if event.AnonymousID != "" {
			anonymous = byAnonymous[event.AnonymousID]
		}
		if external != nil && anonymous != nil && external != anonymous {
			merge(external, anonymous)
			removeProfile(&profiles, anonymous)
			for key, value := range byExternal {
				if value == anonymous {
					byExternal[key] = external
				}
			}
			for key, value := range byAnonymous {
				if value == anonymous {
					byAnonymous[key] = external
				}
			}
			return external
		}
		profile := external
		if profile == nil {
			profile = anonymous
		}
		if profile == nil {
			profile = &CanonicalProfile{Attributes: map[string]any{}, Consents: map[string]CanonicalConsent{}}
			profiles = append(profiles, profile)
		}
		if event.ExternalID != "" {
			profile.ExternalID = event.ExternalID
			byExternal[event.ExternalID] = profile
		}
		if event.AnonymousID != "" {
			profile.AnonymousID = event.AnonymousID
			byAnonymous[event.AnonymousID] = profile
		}
		return profile
	}

	for _, event := range events {
		if event.Type == "privacy.deleted" {
			continue
		}
		profile := ensure(event)
		switch event.Type {
		case "profile.updated":
			var body struct {
				Attributes map[string]any `json:"attributes"`
			}
			if json.Unmarshal(event.Payload, &body) == nil {
				for key, value := range body.Attributes {
					profile.Attributes[key] = value
				}
			}
		case "consent.changed":
			var body struct {
				Channel string `json:"channel"`
				Topic   string `json:"topic"`
				State   string `json:"state"`
			}
			if json.Unmarshal(event.Payload, &body) == nil {
				if body.Topic == "" {
					body.Topic = "marketing"
				}
				body.Channel = strings.ToLower(body.Channel)
				profile.Consents[body.Channel+":"+body.Topic] = CanonicalConsent{
					Channel: body.Channel, Topic: body.Topic, State: body.State, OccurredAt: event.OccurredAt,
				}
			}
		case "identity.merge":
			var body struct {
				SourceExternalID string `json:"source_external_id"`
			}
			if json.Unmarshal(event.Payload, &body) == nil {
				if source := byExternal[body.SourceExternalID]; source != nil && source != profile {
					merge(profile, source)
					removeProfile(&profiles, source)
					delete(byExternal, body.SourceExternalID)
					for key, value := range byAnonymous {
						if value == source {
							byAnonymous[key] = profile
						}
					}
				}
			}
		}
	}
	state := State{Profiles: make([]CanonicalProfile, 0, len(profiles))}
	for _, profile := range profiles {
		state.Profiles = append(state.Profiles, *profile)
	}
	Normalize(&state)
	return state
}

func Normalize(state *State) {
	if state.Profiles == nil {
		state.Profiles = []CanonicalProfile{}
	}
	sort.Slice(state.Profiles, func(i, j int) bool {
		left := state.Profiles[i].ExternalID + "\x00" + state.Profiles[i].AnonymousID
		right := state.Profiles[j].ExternalID + "\x00" + state.Profiles[j].AnonymousID
		return left < right
	})
	for index := range state.Profiles {
		if state.Profiles[index].Attributes == nil {
			state.Profiles[index].Attributes = map[string]any{}
		}
		if state.Profiles[index].Consents == nil {
			state.Profiles[index].Consents = map[string]CanonicalConsent{}
		}
	}
}

func Checksum(state State) string {
	Normalize(&state)
	data, _ := json.Marshal(state)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func merge(target, source *CanonicalProfile) {
	for key, value := range source.Attributes {
		if _, exists := target.Attributes[key]; !exists {
			target.Attributes[key] = value
		}
	}
	for key, consent := range source.Consents {
		current, exists := target.Consents[key]
		if !exists || consent.OccurredAt.After(current.OccurredAt) {
			target.Consents[key] = consent
		}
	}
	if target.AnonymousID == "" {
		target.AnonymousID = source.AnonymousID
	}
}

func removeProfile(profiles *[]*CanonicalProfile, target *CanonicalProfile) {
	for index, profile := range *profiles {
		if profile == target {
			*profiles = append((*profiles)[:index], (*profiles)[index+1:]...)
			return
		}
	}
}
