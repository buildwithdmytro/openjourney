package postgres

import (
	"context"
	"encoding/json"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/buildwithdmytro/openjourney/internal/replay"
)

func (s *Store) VerifyReplay(ctx context.Context, principal domain.Principal) (domain.ReplayReport, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,tenant_id,workspace_id,app_id,event_type,schema_version,
		COALESCE(external_id,''),COALESCE(anonymous_id,''),idempotency_key,occurred_at,received_at,payload
		FROM accepted_events WHERE tenant_id=$1 AND app_id=$2 AND event_type <> 'privacy.deleted'
		ORDER BY occurred_at,received_at,id`, principal.TenantID, principal.AppID)
	if err != nil {
		return domain.ReplayReport{}, err
	}
	var events []domain.AcceptedEvent
	for rows.Next() {
		var event domain.AcceptedEvent
		if err := rows.Scan(&event.ID, &event.Principal.TenantID, &event.Principal.WorkspaceID,
			&event.Principal.AppID, &event.Type, &event.SchemaVersion, &event.ExternalID,
			&event.AnonymousID, &event.IdempotencyKey, &event.OccurredAt, &event.ReceivedAt,
			&event.Payload); err != nil {
			rows.Close()
			return domain.ReplayReport{}, err
		}
		events = append(events, event)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return domain.ReplayReport{}, err
	}
	replayed := replay.Build(events)
	live, err := s.liveReplayState(ctx, principal)
	if err != nil {
		return domain.ReplayReport{}, err
	}
	liveChecksum, replayChecksum := replay.Checksum(live), replay.Checksum(replayed)
	return domain.ReplayReport{
		Match: liveChecksum == replayChecksum, LiveChecksum: liveChecksum,
		ReplayChecksum: replayChecksum, EventCount: len(events), ProfileCount: len(replayed.Profiles),
	}, nil
}

func (s *Store) liveReplayState(ctx context.Context, principal domain.Principal) (replay.State, error) {
	rows, err := s.pool.Query(ctx, `SELECT id,COALESCE(external_id,''),COALESCE(anonymous_id,''),attributes
		FROM profiles WHERE tenant_id=$1 AND app_id=$2`, principal.TenantID, principal.AppID)
	if err != nil {
		return replay.State{}, err
	}
	state := replay.State{}
	profileIndexes := map[string]int{}
	for rows.Next() {
		var id string
		var profile replay.CanonicalProfile
		var attributes json.RawMessage
		if err := rows.Scan(&id, &profile.ExternalID, &profile.AnonymousID, &attributes); err != nil {
			rows.Close()
			return replay.State{}, err
		}
		_ = json.Unmarshal(attributes, &profile.Attributes)
		profile.Consents = map[string]replay.CanonicalConsent{}
		profileIndexes[id] = len(state.Profiles)
		state.Profiles = append(state.Profiles, profile)
	}
	rows.Close()
	consentRows, err := s.pool.Query(ctx, `SELECT DISTINCT ON (profile_id,channel,topic)
		profile_id,channel,topic,state,occurred_at FROM consent_ledger
		WHERE tenant_id=$1 ORDER BY profile_id,channel,topic,occurred_at DESC,created_at DESC`,
		principal.TenantID)
	if err != nil {
		return replay.State{}, err
	}
	defer consentRows.Close()
	for consentRows.Next() {
		var profileID string
		var consent replay.CanonicalConsent
		if err := consentRows.Scan(&profileID, &consent.Channel, &consent.Topic,
			&consent.State, &consent.OccurredAt); err != nil {
			return replay.State{}, err
		}
		if index, exists := profileIndexes[profileID]; exists {
			state.Profiles[index].Consents[consent.Channel+":"+consent.Topic] = consent
		}
	}
	replay.Normalize(&state)
	return state, consentRows.Err()
}
