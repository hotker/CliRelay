package usage

import (
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
)

func TestCountUsageByEndUserAggregatesAllKeys(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "enduser-quota-*.db")
	if err != nil {
		t.Fatalf("create temp db: %v", err)
	}
	dbPath := tmpFile.Name()
	_ = tmpFile.Close()
	t.Cleanup(func() {
		CloseDB()
		_ = os.Remove(dbPath)
		_ = os.Remove(dbPath + "-wal")
		_ = os.Remove(dbPath + "-shm")
	})
	if err := InitDB(dbPath, config.RequestLogStorageConfig{}, time.UTC); err != nil {
		t.Fatalf("InitDB: %v", err)
	}

	db := getDB()
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS end_users (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			username TEXT NOT NULL,
			username_normalized TEXT NOT NULL,
			display_name TEXT NOT NULL,
			password_hash TEXT NOT NULL DEFAULT 'x',
			status TEXT NOT NULL DEFAULT 'active',
			permission_profile_id TEXT NOT NULL DEFAULT '',
			daily_limit INTEGER NOT NULL DEFAULT 0,
			total_quota INTEGER NOT NULL DEFAULT 0,
			spending_limit REAL NOT NULL DEFAULT 0,
			daily_spending_limit REAL NOT NULL DEFAULT 0,
			concurrency_limit INTEGER NOT NULL DEFAULT 0,
			rpm_limit INTEGER NOT NULL DEFAULT 0,
			tpm_limit INTEGER NOT NULL DEFAULT 0,
			allowed_models TEXT NOT NULL DEFAULT '[]',
			allowed_channels TEXT NOT NULL DEFAULT '[]',
			allowed_channel_groups TEXT NOT NULL DEFAULT '[]',
			system_prompt TEXT NOT NULL DEFAULT ''
		)
	`); err != nil {
		t.Fatalf("create end_users: %v", err)
	}

	endUserID := uuid.NewString()
	if _, err := db.Exec(`
		INSERT INTO end_users (id, tenant_id, username, username_normalized, display_name, daily_limit)
		VALUES (?, ?, 'u1', 'u1', '陈龙', 10)
	`, endUserID, systemTenantID); err != nil {
		t.Fatalf("insert end user: %v", err)
	}

	keyA := "sk-enduser-a"
	keyB := "sk-enduser-b"
	idA := uuid.NewString()
	idB := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	for _, row := range []struct {
		key, id string
	}{{keyA, idA}, {keyB, idB}} {
		if err := UpsertAPIKey(APIKeyRow{
			TenantID:   systemTenantID,
			ID:         row.id,
			Key:        row.key,
			Name:       "k",
			EndUserID:  endUserID,
			CreatedAt:  now,
			UpdatedAt:  now,
			DailyLimit: 999, // stripOwnedKeyQuota must clear this
		}); err != nil {
			t.Fatalf("upsert key: %v", err)
		}
	}
	if got := GetAPIKey(keyA); got == nil || got.DailyLimit != 0 {
		t.Fatalf("owned key daily_limit = %#v, want 0", got)
	}

	ts := CutoffStartUTC(1).Add(time.Hour).Format(time.RFC3339)
	for _, row := range []struct {
		key, id string
	}{{keyA, idA}, {keyB, idB}} {
		if _, err := db.Exec(
			`INSERT INTO request_logs
			 (tenant_id, timestamp, api_key, api_key_id, model, source, failed, latency_ms, input_tokens, output_tokens, reasoning_tokens, cached_tokens, total_tokens, cost)
			 VALUES (?, ?, ?, ?, ?, ?, 0, 1, 0, 0, 0, 0, 1, 0)`,
			systemTenantID, ts, row.key, row.id, "model", "test",
		); err != nil {
			t.Fatalf("insert log: %v", err)
		}
	}

	today, err := CountTodayByEndUser(endUserID)
	if err != nil {
		t.Fatalf("CountTodayByEndUser: %v", err)
	}
	if today != 2 {
		t.Fatalf("CountTodayByEndUser = %d, want 2 (shared pool)", today)
	}
	total, err := CountTotalByEndUser(endUserID)
	if err != nil {
		t.Fatalf("CountTotalByEndUser: %v", err)
	}
	if total != 2 {
		t.Fatalf("CountTotalByEndUser = %d, want 2", total)
	}

	q := GetEndUserQuota(endUserID)
	if q == nil || q.DisplayName != "陈龙" || q.DailyLimit != 10 {
		t.Fatalf("GetEndUserQuota = %#v", q)
	}
	if name := ResolveAPIKeyDisplayName(GetAPIKey(keyA), "fallback"); name != "陈龙" {
		t.Fatalf("display name = %q, want 陈龙", name)
	}
}
