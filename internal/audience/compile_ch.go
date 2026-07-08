package audience

func CompileClickHouse(n *EventHistory, tenantID string) (string, []any) {
	sql := `SELECT subject_hash FROM behavior_events
		WHERE tenant_id = ? AND event_type = ? AND occurred_at >= now() - INTERVAL ? DAY
		GROUP BY subject_hash HAVING count() >= ?`

	minCount := n.MinCount
	if minCount <= 0 {
		minCount = 1
	}

	args := []any{tenantID, n.EventType, n.TimeWindowDays, minCount}
	return sql, args
}
