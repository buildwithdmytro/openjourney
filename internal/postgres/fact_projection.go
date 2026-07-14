package postgres

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/buildwithdmytro/openjourney/internal/domain"
	"github.com/jackc/pgx/v5"
)

type frozenConversionGoal struct {
	Name       string `json:"name"`
	ValueField string `json:"value_field"`
}

type engagementPayload struct {
	CampaignID        string `json:"campaign_id"`
	JourneyID         string `json:"journey_id"`
	NodeID            string `json:"node_id"`
	Channel           string `json:"channel"`
	Endpoint          string `json:"endpoint"`
	ProviderMessageID string `json:"provider_message_id"`
}

func (s *Store) projectEngagementFact(ctx context.Context, tx pgx.Tx, event domain.AcceptedEvent, profileID string) error {
	eventType := map[string]string{
		"message.delivered":  "delivered",
		"email.opened":       "opened",
		"link.clicked":       "clicked",
		"message.bounced":    "bounced",
		"message.complained": "complained",
	}[event.Type]
	if eventType == "" {
		return nil
	}

	var payload engagementPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return err
	}

	var (
		sourceType   string
		sourceID     string
		nodeID       *string
		experimentID *string
		variant      *string
		resolvedID   string
		channel      string
	)
	err := tx.QueryRow(ctx, `
		SELECT source_type, source_id, node_id, experiment_id, variant, profile_id, channel
		FROM (
			SELECT 'campaign'::text AS source_type, da.campaign_id AS source_id,
				NULL::text AS node_id, da.experiment_id, da.variant, da.profile_id, da.channel,
				da.attempted_at AS sent_at
			FROM delivery_attempts da
			JOIN campaigns c ON c.id = da.campaign_id
			WHERE da.tenant_id = $1 AND c.tenant_id = $1 AND c.workspace_id = $2
			  AND da.decision IN ('sent','provider_sent')
			  AND (NULLIF($3, '') IS NULL OR da.profile_id = NULLIF($3, '')::uuid)
			  AND (NULLIF($4, '') IS NULL OR da.endpoint = $4)
			  AND (NULLIF($5, '') IS NULL OR da.campaign_id = NULLIF($5, '')::uuid)
			  AND NULLIF($6, '') IS NULL
			  AND (NULLIF($8, '') IS NULL OR da.provider_message_id = $8)
			UNION ALL
			SELECT 'journey'::text AS source_type, jmi.journey_id AS source_id,
				jmi.node_id, jmi.experiment_id, jmi.variant, jmi.profile_id, jmi.channel,
				jmi.updated_at AS sent_at
			FROM journey_message_intents jmi
			WHERE jmi.tenant_id = $1 AND jmi.workspace_id = $2
			  AND jmi.decision IN ('sent','provider_sent')
			  AND (NULLIF($3, '') IS NULL OR jmi.profile_id = NULLIF($3, '')::uuid)
			  AND (NULLIF($4, '') IS NULL OR jmi.endpoint = $4)
			  AND NULLIF($5, '') IS NULL
			  AND (NULLIF($6, '') IS NULL OR jmi.journey_id = NULLIF($6, '')::uuid)
			  AND (NULLIF($7, '') IS NULL OR jmi.node_id = $7)
			  AND (NULLIF($8, '') IS NULL OR jmi.provider_message_id = $8)
		) engagement_send
		ORDER BY sent_at DESC
		LIMIT 1`, event.Principal.TenantID, event.Principal.WorkspaceID, profileID,
		payload.Endpoint, payload.CampaignID, payload.JourneyID, payload.NodeID,
		payload.ProviderMessageID).Scan(&sourceType, &sourceID, &nodeID, &experimentID,
		&variant, &resolvedID, &channel)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	if payload.Channel != "" && !strings.EqualFold(payload.Channel, channel) {
		return nil
	}

	_, err = tx.Exec(ctx, `INSERT INTO engagement_facts
		(tenant_id, workspace_id, source_type, source_id, node_id, experiment_id, variant,
		 profile_id, channel, event_type, occurred_at, source_event_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (source_event_id, event_type) DO NOTHING`,
		event.Principal.TenantID, event.Principal.WorkspaceID, sourceType, sourceID, nodeID,
		experimentID, variant, resolvedID, channel, eventType, event.OccurredAt, event.ID)
	return err
}

func (s *Store) projectConversionFact(ctx context.Context, tx pgx.Tx, event domain.AcceptedEvent, profileID string) (bool, string, string, error) {
	var (
		sourceType   string
		sourceID     string
		experimentID *string
		variant      *string
		sentAt       time.Time
		goalJSON     []byte
	)

	err := tx.QueryRow(ctx, `
		SELECT source_type, source_id, experiment_id, variant, sent_at, goal
		FROM (
			SELECT 'campaign'::text AS source_type, da.campaign_id AS source_id,
				da.experiment_id, da.variant, da.attempted_at AS sent_at,
				c.conversion_goal AS goal
			FROM delivery_attempts da
			JOIN campaigns c ON c.id = da.campaign_id
			WHERE da.tenant_id = $1 AND c.tenant_id = $1 AND c.workspace_id = $2
			  AND da.profile_id = $3 AND da.decision = 'sent'
			  AND c.conversion_goal->>'event_type' = $4
			  AND c.attribution_window IS NOT NULL
			  AND da.attempted_at BETWEEN $5::timestamptz - c.attribution_window AND $5::timestamptz
			  AND $6::jsonb @> COALESCE(c.conversion_goal->'filter', '{}'::jsonb)
			UNION ALL
			SELECT 'journey'::text AS source_type, jmi.journey_id AS source_id,
				jmi.experiment_id, jmi.variant, jmi.updated_at AS sent_at,
				jv.conversion_goal AS goal
			FROM journey_message_intents jmi
			JOIN journey_versions jv ON jv.id = jmi.journey_version_id
			WHERE jmi.tenant_id = $1 AND jmi.workspace_id = $2
			  AND jv.tenant_id = $1 AND jv.workspace_id = $2
			  AND jmi.profile_id = $3 AND jmi.decision = 'sent'
			  AND jv.conversion_goal->>'event_type' = $4
			  AND jv.attribution_window IS NOT NULL
			  AND jmi.updated_at BETWEEN $5::timestamptz - jv.attribution_window AND $5::timestamptz
			  AND $6::jsonb @> COALESCE(jv.conversion_goal->'filter', '{}'::jsonb)
		) attributed_sends
		ORDER BY sent_at DESC
		LIMIT 1`,
		event.Principal.TenantID, event.Principal.WorkspaceID, profileID, event.Type,
		event.OccurredAt, event.Payload,
	).Scan(&sourceType, &sourceID, &experimentID, &variant, &sentAt, &goalJSON)
	if err == pgx.ErrNoRows {
		return false, "", "", nil
	}
	if err != nil {
		return false, "", "", err
	}

	var goal frozenConversionGoal
	if err := json.Unmarshal(goalJSON, &goal); err != nil {
		return false, "", "", err
	}
	if goal.Name == "" {
		goal.Name = event.Type
	}
	value := conversionValue(event.Payload, goal.ValueField)
	result, err := tx.Exec(ctx, `INSERT INTO conversion_facts
		(tenant_id, workspace_id, source_type, source_id, experiment_id, variant, profile_id,
		 goal_name, value, occurred_at, attributed_send_at, source_event_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
		ON CONFLICT (source_event_id, goal_name) DO NOTHING`,
		event.Principal.TenantID, event.Principal.WorkspaceID, sourceType, sourceID,
		experimentID, variant, profileID, goal.Name, value, event.OccurredAt, sentAt, event.ID)
	if err != nil {
		return false, "", "", err
	}
	variantLabel := ""
	if variant != nil {
		variantLabel = *variant
	}
	return result.RowsAffected() == 1, sourceType, variantLabel, nil
}

func conversionValue(payload json.RawMessage, field string) float64 {
	if field == "" {
		return 0
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return 0
	}
	for _, part := range strings.Split(field, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return 0
		}
		value, ok = object[part]
		if !ok {
			return 0
		}
	}
	switch number := value.(type) {
	case json.Number:
		parsed, _ := number.Float64()
		return parsed
	case string:
		parsed, _ := strconv.ParseFloat(number, 64)
		return parsed
	default:
		return 0
	}
}
