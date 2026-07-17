package usage

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// APIKeyDailySpendingResetEvent is one manual daily-spending reset action.
type APIKeyDailySpendingResetEvent struct {
	ID            int64     `json:"id"`
	TenantID      string    `json:"tenant_id"`
	APIKeyID      string    `json:"api_key_id"`
	DayKey        string    `json:"day_key"`
	ResetAt       time.Time `json:"reset_at"`
	ActorUserID   string    `json:"actor_user_id,omitempty"`
	ActorUsername string    `json:"actor_username,omitempty"`
	ActorKind     string    `json:"actor_kind,omitempty"`
	// CostBaseline is raw today cost at reset time (SUM request_logs.cost for the day).
	CostBaseline float64 `json:"cost_baseline"`
	// EffectiveUsedBefore is the effective daily used amount cleared by this reset.
	EffectiveUsedBefore float64 `json:"effective_used_before"`
	// RawTodayCost is the true project-day spend (no baseline) at reset time.
	RawTodayCost float64 `json:"raw_today_cost"`
}

// TIMESTAMP for SQLite affinity + PG bootstrap compatibility (same as baseline table).
const apiKeyDailySpendingResetEventsTableSQL = `
CREATE TABLE IF NOT EXISTS api_key_daily_spending_reset_events (
  id                     INTEGER PRIMARY KEY AUTOINCREMENT,
  tenant_id              TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001',
  api_key_id             TEXT NOT NULL,
  day_key                TEXT NOT NULL DEFAULT '',
  reset_at               TIMESTAMP NOT NULL,
  actor_user_id          TEXT NOT NULL DEFAULT '',
  actor_username         TEXT NOT NULL DEFAULT '',
  actor_kind             TEXT NOT NULL DEFAULT '',
  cost_baseline          REAL NOT NULL DEFAULT 0,
  effective_used_before  REAL NOT NULL DEFAULT 0,
  raw_today_cost         REAL NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_api_key_daily_spending_reset_events_key
  ON api_key_daily_spending_reset_events(tenant_id, api_key_id, reset_at DESC);
`

func ensureAPIKeyDailySpendingResetEventsTable(db *sql.DB) error {
	if db == nil {
		return nil
	}
	if _, err := db.Exec(apiKeyDailySpendingResetEventsTableSQL); err != nil {
		return fmt.Errorf("usage: ensure api_key_daily_spending_reset_events: %w", err)
	}
	return nil
}

func bootstrapAPIKeyDailySpendingResetEvents(db *sql.DB) error {
	return ensureAPIKeyDailySpendingResetEventsTable(db)
}

// InsertDailySpendingResetEvent appends one reset history row.
func InsertDailySpendingResetEvent(ev APIKeyDailySpendingResetEvent) error {
	db := getDB()
	if db == nil {
		return fmt.Errorf("usage: database not initialised")
	}
	tenantID := normalizeTenantID(ev.TenantID)
	apiKeyID := strings.TrimSpace(ev.APIKeyID)
	if apiKeyID == "" {
		return fmt.Errorf("usage: api_key_id is required")
	}
	resetAt := ev.ResetAt
	if resetAt.IsZero() {
		resetAt = time.Now().UTC()
	}
	dayKey := strings.TrimSpace(ev.DayKey)
	if dayKey == "" {
		dayKey = LocalDayKeyAt(resetAt)
	}
	baseline := ev.CostBaseline
	if baseline < 0 {
		baseline = 0
	}
	usedBefore := ev.EffectiveUsedBefore
	if usedBefore < 0 {
		usedBefore = 0
	}
	raw := ev.RawTodayCost
	if raw < 0 {
		raw = 0
	}
	_, err := db.Exec(
		`INSERT INTO api_key_daily_spending_reset_events
		 (tenant_id, api_key_id, day_key, reset_at, actor_user_id, actor_username, actor_kind,
		  cost_baseline, effective_used_before, raw_today_cost)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tenantID, apiKeyID, dayKey, resetAt.UTC().Format(time.RFC3339Nano),
		strings.TrimSpace(ev.ActorUserID), strings.TrimSpace(ev.ActorUsername), strings.TrimSpace(ev.ActorKind),
		baseline, usedBefore, raw,
	)
	if err != nil {
		return fmt.Errorf("usage: insert daily spending reset event: %w", err)
	}
	return nil
}

// CountDailySpendingResetEvents returns how many reset events exist for a key.
func CountDailySpendingResetEvents(tenantID, apiKeyID string) (int, error) {
	db := getDB()
	if db == nil {
		return 0, nil
	}
	tenantID = normalizeTenantID(tenantID)
	apiKeyID = strings.TrimSpace(apiKeyID)
	if apiKeyID == "" {
		return 0, nil
	}
	var n int
	err := db.QueryRow(
		`SELECT COUNT(*) FROM api_key_daily_spending_reset_events WHERE tenant_id = ? AND api_key_id = ?`,
		tenantID, apiKeyID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("usage: count daily spending reset events: %w", err)
	}
	return n, nil
}

// ListDailySpendingResetEventCounts returns reset counts keyed by api_key_id.
func ListDailySpendingResetEventCounts(tenantID string, apiKeyIDs []string) (map[string]int, error) {
	out := make(map[string]int)
	if len(apiKeyIDs) == 0 {
		return out, nil
	}
	db := getDB()
	if db == nil {
		return out, nil
	}
	tenantID = normalizeTenantID(tenantID)
	ids := make([]string, 0, len(apiKeyIDs))
	seen := make(map[string]struct{}, len(apiKeyIDs))
	for _, id := range apiKeyIDs {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]interface{}, 0, len(ids)+1)
	args = append(args, tenantID)
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	rows, err := db.Query(
		`SELECT api_key_id, COUNT(*) FROM api_key_daily_spending_reset_events
		 WHERE tenant_id = ? AND api_key_id IN (`+strings.Join(placeholders, ",")+`)
		 GROUP BY api_key_id`,
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("usage: list daily spending reset event counts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, fmt.Errorf("usage: scan reset event count: %w", err)
		}
		out[strings.TrimSpace(id)] = n
	}
	return out, rows.Err()
}

// ListDailySpendingResetEvents returns newest-first history for a key.
func ListDailySpendingResetEvents(tenantID, apiKeyID string, limit int) ([]APIKeyDailySpendingResetEvent, error) {
	db := getDB()
	if db == nil {
		return nil, nil
	}
	tenantID = normalizeTenantID(tenantID)
	apiKeyID = strings.TrimSpace(apiKeyID)
	if apiKeyID == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := db.Query(
		`SELECT id, tenant_id, api_key_id, day_key, reset_at, actor_user_id, actor_username, actor_kind,
		        cost_baseline, effective_used_before, raw_today_cost
		 FROM api_key_daily_spending_reset_events
		 WHERE tenant_id = ? AND api_key_id = ?
		 ORDER BY reset_at DESC, id DESC
		 LIMIT ?`,
		tenantID, apiKeyID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("usage: list daily spending reset events: %w", err)
	}
	defer rows.Close()
	out := make([]APIKeyDailySpendingResetEvent, 0)
	for rows.Next() {
		var ev APIKeyDailySpendingResetEvent
		var resetAt string
		if err := rows.Scan(
			&ev.ID, &ev.TenantID, &ev.APIKeyID, &ev.DayKey, &resetAt,
			&ev.ActorUserID, &ev.ActorUsername, &ev.ActorKind,
			&ev.CostBaseline, &ev.EffectiveUsedBefore, &ev.RawTodayCost,
		); err != nil {
			return nil, fmt.Errorf("usage: scan daily spending reset event: %w", err)
		}
		if ts, err := time.Parse(time.RFC3339Nano, resetAt); err == nil {
			ev.ResetAt = ts
		} else if ts, err := time.Parse(time.RFC3339, resetAt); err == nil {
			ev.ResetAt = ts
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

// DeleteDailySpendingResetEvents removes history for a key (e.g. on key delete).
func DeleteDailySpendingResetEvents(tenantID, apiKeyID string) error {
	db := getDB()
	if db == nil {
		return nil
	}
	tenantID = normalizeTenantID(tenantID)
	apiKeyID = strings.TrimSpace(apiKeyID)
	if apiKeyID == "" {
		return nil
	}
	_, err := db.Exec(
		`DELETE FROM api_key_daily_spending_reset_events WHERE tenant_id = ? AND api_key_id = ?`,
		tenantID, apiKeyID,
	)
	if err != nil {
		return fmt.Errorf("usage: delete daily spending reset events: %w", err)
	}
	return nil
}
