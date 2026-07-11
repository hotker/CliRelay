package usage

import (
	"database/sql"
	"net/http"
	"testing"
	"time"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/identityfingerprint"
)

func TestObserveIdentityFingerprintLearnsAndMergesClaudeAccount(t *testing.T) {
	initTestUsageDB(t, config.RequestLogStorageConfig{})

	accountKey := "claude-account"
	firstSeen := time.Date(2026, 6, 23, 1, 2, 3, 0, time.UTC)
	headers := http.Header{}
	headers.Set("User-Agent", "claude-cli/2.1.170 (external, cli)")
	headers.Set("X-App", "cli")
	headers.Set("Anthropic-Beta", "claude-code-20250219,oauth-2025-04-20")
	headers.Set("X-Stainless-Package-Version", "0.95.0")
	headers.Set("X-Stainless-Runtime-Version", "v24.4.0")
	headers.Set("X-Stainless-Timeout", "700")

	record, result, err := ObserveIdentityFingerprint(identityfingerprint.LearnInput{
		Provider:      identityfingerprint.ProviderClaude,
		AccountKey:    accountKey,
		AuthSubjectID: "subject-claude",
		Headers:       headers,
		ObservedAt:    firstSeen,
	})
	if err != nil {
		t.Fatalf("ObserveIdentityFingerprint returned error: %v", err)
	}
	if result.Reason != "created" || record == nil {
		t.Fatalf("merge result = %+v, record = %#v, want created", result, record)
	}
	if record.Fields[identityfingerprint.FieldUserAgent] != "claude-cli/2.1.170 (external, cli)" {
		t.Fatalf("learned User-Agent = %q", record.Fields[identityfingerprint.FieldUserAgent])
	}
	if record.Fields[identityfingerprint.FieldClaudeStainlessRuntime] != "v24.4.0" {
		t.Fatalf("learned runtime = %q", record.Fields[identityfingerprint.FieldClaudeStainlessRuntime])
	}
	if record.ObservedHeaders["X-Stainless-Timeout"] != "700" {
		t.Fatalf("observed headers = %#v, want stainless timeout", record.ObservedHeaders)
	}

	olderHeaders := http.Header{}
	olderHeaders.Set("User-Agent", "claude-cli/2.1.100 (external, cli)")
	olderHeaders.Set("X-App", "cli")
	olderHeaders.Set("X-Stainless-Runtime-Version", "v24.0.0")
	olderSeen := firstSeen.Add(time.Hour)
	record, result, err = ObserveIdentityFingerprint(identityfingerprint.LearnInput{
		Provider:      identityfingerprint.ProviderClaude,
		AccountKey:    accountKey,
		AuthSubjectID: "subject-claude",
		Headers:       olderHeaders,
		ObservedAt:    olderSeen,
	})
	if err != nil {
		t.Fatalf("ObserveIdentityFingerprint older returned error: %v", err)
	}
	if result.Reason != "older_version_last_seen" {
		t.Fatalf("older merge reason = %q, want older_version_last_seen", result.Reason)
	}
	if record.Fields[identityfingerprint.FieldUserAgent] != "claude-cli/2.1.170 (external, cli)" {
		t.Fatalf("older observation should not replace User-Agent, got %q", record.Fields[identityfingerprint.FieldUserAgent])
	}
	if !record.LastSeenAt.Equal(olderSeen) {
		t.Fatalf("LastSeenAt = %s, want %s", record.LastSeenAt, olderSeen)
	}

	newerHeaders := http.Header{}
	newerHeaders.Set("User-Agent", "claude-cli/2.1.180 (external, cli)")
	newerHeaders.Set("X-App", "cli")
	newerSeen := olderSeen.Add(time.Hour)
	record, result, err = ObserveIdentityFingerprint(identityfingerprint.LearnInput{
		Provider:      identityfingerprint.ProviderClaude,
		AccountKey:    accountKey,
		AuthSubjectID: "subject-claude",
		Headers:       newerHeaders,
		ObservedAt:    newerSeen,
	})
	if err != nil {
		t.Fatalf("ObserveIdentityFingerprint newer returned error: %v", err)
	}
	if result.Reason != "merged_profile" {
		t.Fatalf("newer merge reason = %q, want merged_profile", result.Reason)
	}
	if record.Version != "2.1.180" {
		t.Fatalf("Version = %q, want newer version", record.Version)
	}
	if record.Fields[identityfingerprint.FieldClaudeStainlessRuntime] != "v24.4.0" {
		t.Fatalf("newer partial observation should preserve runtime, got %q", record.Fields[identityfingerprint.FieldClaudeStainlessRuntime])
	}

	stored, err := GetIdentityFingerprint(identityfingerprint.ProviderClaude, accountKey)
	if err != nil {
		t.Fatalf("GetIdentityFingerprint returned error: %v", err)
	}
	if stored == nil || stored.Version != "2.1.180" {
		t.Fatalf("stored record = %#v, want newer version", stored)
	}
	list, err := ListIdentityFingerprints(identityfingerprint.ProviderClaude, 10)
	if err != nil {
		t.Fatalf("ListIdentityFingerprints returned error: %v", err)
	}
	if len(list) != 1 || list[0].AccountKey != accountKey {
		t.Fatalf("list = %#v, want one learned Claude account", list)
	}
	deleted, err := DeleteIdentityFingerprint(identityfingerprint.ProviderClaude, accountKey)
	if err != nil {
		t.Fatalf("DeleteIdentityFingerprint returned error: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}
}

func TestObserveIdentityFingerprintKeepsCodexProfilesSeparate(t *testing.T) {
	initTestUsageDB(t, config.RequestLogStorageConfig{})

	accountKey := "codex-account"
	cliHeaders := http.Header{}
	cliHeaders.Set("User-Agent", "codex_cli_rs/0.130.0 (Mac OS 26.3.1; arm64) iTerm.app/3.6.9")
	cliHeaders.Set("Version", "0.130.0")
	cliHeaders.Set("Originator", "codex_cli_rs")
	cliHeaders.Set("OpenAI-Beta", "responses_websockets=2026-02-06")
	cliHeaders.Set("X-Codex-Beta-Features", "compact_mode")

	cliRecord, result, err := ObserveIdentityFingerprint(identityfingerprint.LearnInput{
		Provider:      identityfingerprint.ProviderCodex,
		AccountKey:    accountKey,
		AuthSubjectID: "subject-codex",
		Headers:       cliHeaders,
		ObservedAt:    time.Date(2026, 6, 23, 2, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ObserveIdentityFingerprint returned error: %v", err)
	}
	if result.Reason != "created" || cliRecord.ProfileKey != "codex_cli_rs" {
		t.Fatalf("merge result = %+v, record = %#v, want CLI profile created", result, cliRecord)
	}

	desktopHeaders := http.Header{}
	desktopHeaders.Set("User-Agent", "Codex Desktop/0.144.0-alpha.4 (Mac OS 26.5.2; arm64) unknown (Codex Desktop; 26.707.31123)")
	desktopHeaders.Set("Version", "0.144.0")
	desktopHeaders.Set("Originator", "Codex Desktop")
	desktopHeaders.Set("X-Codex-Beta-Features", "remote_compaction_v2")
	desktopRecord, result, err := ObserveIdentityFingerprint(identityfingerprint.LearnInput{
		Provider:      identityfingerprint.ProviderCodex,
		AccountKey:    accountKey,
		AuthSubjectID: "subject-codex",
		Headers:       desktopHeaders,
		ObservedAt:    time.Date(2026, 6, 23, 3, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ObserveIdentityFingerprint desktop returned error: %v", err)
	}
	if result.Reason != "created" || desktopRecord.ProfileKey != identityfingerprint.ProfileKeyCodexDesktop {
		t.Fatalf("desktop merge result = %+v, record = %#v, want Desktop profile created", result, desktopRecord)
	}

	profiles, err := ListIdentityFingerprintProfiles(identityfingerprint.ProviderCodex, accountKey)
	if err != nil {
		t.Fatalf("ListIdentityFingerprintProfiles returned error: %v", err)
	}
	if len(profiles) != 2 {
		t.Fatalf("profiles = %#v, want two isolated Codex profiles", profiles)
	}
	storedCLI, err := GetIdentityFingerprintProfile(identityfingerprint.ProviderCodex, accountKey, "codex_cli_rs")
	if err != nil {
		t.Fatalf("GetIdentityFingerprintProfile CLI returned error: %v", err)
	}
	if storedCLI == nil || storedCLI.Fields[identityfingerprint.FieldUserAgent] != cliHeaders.Get("User-Agent") {
		t.Fatalf("stored CLI profile = %#v, want original CLI identity", storedCLI)
	}
}

func TestObserveIdentityFingerprintLearnsGeminiCLIHeaders(t *testing.T) {
	initTestUsageDB(t, config.RequestLogStorageConfig{})

	headers := http.Header{}
	headers.Set("User-Agent", "google-api-nodejs-client/9.16.0")
	headers.Set("X-Goog-Api-Client", "gl-node/24.1.0")
	headers.Set("Client-Metadata", "ideType=IDE_UNSPECIFIED,platform=PLATFORM_UNSPECIFIED,pluginType=GEMINI")

	record, result, err := ObserveIdentityFingerprint(identityfingerprint.LearnInput{
		Provider:      identityfingerprint.ProviderGemini,
		AccountKey:    "gemini-account",
		AuthSubjectID: "subject-gemini",
		Headers:       headers,
		ObservedAt:    time.Date(2026, 6, 23, 4, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("ObserveIdentityFingerprint returned error: %v", err)
	}
	if result.Reason != "created" || record.ClientProduct != "google-api-nodejs-client" || record.Version != "9.16.0" {
		t.Fatalf("record = %#v, result = %+v, want Gemini CLI created", record, result)
	}
	if record.Fields[identityfingerprint.FieldGeminiAPIClient] != "gl-node/24.1.0" {
		t.Fatalf("X-Goog-Api-Client = %q, want learned", record.Fields[identityfingerprint.FieldGeminiAPIClient])
	}
}

func TestObserveIdentityFingerprintSkipsRecentUnchangedLastSeenWrite(t *testing.T) {
	initTestUsageDB(t, config.RequestLogStorageConfig{})

	accountKey := "codex-unchanged-account"
	headers := http.Header{}
	headers.Set("User-Agent", "codex_cli_rs/0.144.1 (Mac OS 26.5.2; arm64) iTerm.app/3.6.9")
	headers.Set("Version", "0.144.1")
	headers.Set("Originator", "codex_cli_rs")

	firstSeen := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	record, result, err := ObserveIdentityFingerprint(identityfingerprint.LearnInput{
		Provider:   identityfingerprint.ProviderCodex,
		AccountKey: accountKey,
		Headers:    headers,
		ObservedAt: firstSeen,
	})
	if err != nil {
		t.Fatalf("ObserveIdentityFingerprint first returned error: %v", err)
	}
	if result.Reason != "created" || record == nil {
		t.Fatalf("first result = %+v, record=%#v, want created", result, record)
	}

	record, result, err = ObserveIdentityFingerprint(identityfingerprint.LearnInput{
		Provider:   identityfingerprint.ProviderCodex,
		AccountKey: accountKey,
		Headers:    headers,
		ObservedAt: firstSeen.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("ObserveIdentityFingerprint repeated returned error: %v", err)
	}
	if result.Changed || result.Reason != "unchanged_recently_seen" {
		t.Fatalf("repeated result = %+v, want unchanged_recently_seen without write", result)
	}
	if !record.LastSeenAt.Equal(firstSeen) {
		t.Fatalf("LastSeenAt = %s, want original %s", record.LastSeenAt, firstSeen)
	}

	record, result, err = ObserveIdentityFingerprint(identityfingerprint.LearnInput{
		Provider:   identityfingerprint.ProviderCodex,
		AccountKey: accountKey,
		Headers:    headers,
		ObservedAt: firstSeen.Add(identityFingerprintLastSeenMinInterval + time.Second),
	})
	if err != nil {
		t.Fatalf("ObserveIdentityFingerprint later returned error: %v", err)
	}
	if !result.Changed || result.Reason != "merged_profile" {
		t.Fatalf("later result = %+v, want throttled window elapsed update", result)
	}
	if !record.LastSeenAt.After(firstSeen) {
		t.Fatalf("LastSeenAt = %s, want refreshed after %s", record.LastSeenAt, firstSeen)
	}
}

func TestIdentityFingerprintStoreIsScopedToSystemTenant(t *testing.T) {
	initTestUsageDB(t, config.RequestLogStorageConfig{})
	db := getDB()
	if db == nil {
		t.Fatal("usage database unavailable")
	}
	if _, err := db.Exec(`
		INSERT INTO identity_fingerprints (
			tenant_id, provider, account_key, profile_key, fields_json, observed_headers_json,
			created_at, updated_at, last_seen_at
		) VALUES (?, ?, ?, ?, '{}', '{}', ?, ?, ?)
	`, "11111111-1111-1111-1111-111111111111", string(identityfingerprint.ProviderCodex), "shared-account", "codex_cli_rs", time.Now(), time.Now(), time.Now()); err != nil {
		t.Fatalf("insert foreign tenant fingerprint: %v", err)
	}
	if record, err := GetIdentityFingerprint(identityfingerprint.ProviderCodex, "shared-account"); err != nil || record != nil {
		t.Fatalf("GetIdentityFingerprint() record=%#v err=%v, want no cross-tenant result", record, err)
	}
	if records, err := ListIdentityFingerprints(identityfingerprint.ProviderCodex, 10); err != nil || len(records) != 0 {
		t.Fatalf("ListIdentityFingerprints() records=%#v err=%v, want no cross-tenant results", records, err)
	}
}

func TestIdentityFingerprintLegacySchemaMigratesToTenantPrimaryKeys(t *testing.T) {
	db, err := sql.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer db.Close()
	if _, err = db.Exec(`CREATE TABLE identity_fingerprints (
		tenant_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001', provider TEXT NOT NULL,
		account_key TEXT NOT NULL, profile_key TEXT NOT NULL DEFAULT 'default', auth_subject_id TEXT NOT NULL DEFAULT '',
		client_product TEXT NOT NULL DEFAULT '', client_variant TEXT NOT NULL DEFAULT '', version TEXT NOT NULL DEFAULT '',
		fields_json TEXT NOT NULL DEFAULT '{}', observed_headers_json TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL DEFAULT '',
		updated_at TEXT NOT NULL DEFAULT '', last_seen_at TEXT NOT NULL DEFAULT '', PRIMARY KEY (provider, account_key, profile_key));
		CREATE TABLE identity_fingerprint_account_policies (
		tenant_id TEXT NOT NULL DEFAULT '00000000-0000-0000-0000-000000000001', provider TEXT NOT NULL,
		account_key TEXT NOT NULL, strategy TEXT NOT NULL DEFAULT 'cli_preferred', active_profile_key TEXT NOT NULL DEFAULT '',
		revision INTEGER NOT NULL DEFAULT 1, updated_at TEXT NOT NULL DEFAULT '', PRIMARY KEY (provider, account_key));
		INSERT INTO identity_fingerprints(tenant_id,provider,account_key,profile_key) VALUES
		('00000000-0000-0000-0000-000000000001','codex','shared','codex_cli_rs');
		INSERT INTO identity_fingerprint_account_policies(tenant_id,provider,account_key) VALUES
		('00000000-0000-0000-0000-000000000001','codex','shared');`); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}

	initIdentityFingerprintsTable(db)

	if pk, errPK := sqlitePrimaryKeyColumns(db, "identity_fingerprints"); errPK != nil || len(pk) != 4 || pk[0] != "tenant_id" {
		t.Fatalf("identity_fingerprints primary key = %v, err=%v", pk, errPK)
	}
	if pk, errPK := sqlitePrimaryKeyColumns(db, "identity_fingerprint_account_policies"); errPK != nil || len(pk) != 3 || pk[0] != "tenant_id" {
		t.Fatalf("identity_fingerprint_account_policies primary key = %v, err=%v", pk, errPK)
	}
	const tenantB = "00000000-0000-0000-0000-00000000000b"
	if _, err = db.Exec(`INSERT INTO identity_fingerprints(tenant_id,provider,account_key,profile_key) VALUES(?,?,?,?)`, tenantB, "codex", "shared", "codex_cli_rs"); err != nil {
		t.Fatalf("insert same fingerprint key for tenant B: %v", err)
	}
	if _, err = db.Exec(`INSERT INTO identity_fingerprint_account_policies(tenant_id,provider,account_key) VALUES(?,?,?)`, tenantB, "codex", "shared"); err != nil {
		t.Fatalf("insert same policy key for tenant B: %v", err)
	}
	var fingerprints, policies int
	if err = db.QueryRow(`SELECT COUNT(*) FROM identity_fingerprints WHERE provider='codex' AND account_key='shared'`).Scan(&fingerprints); err != nil {
		t.Fatalf("count fingerprints: %v", err)
	}
	if err = db.QueryRow(`SELECT COUNT(*) FROM identity_fingerprint_account_policies WHERE provider='codex' AND account_key='shared'`).Scan(&policies); err != nil {
		t.Fatalf("count policies: %v", err)
	}
	if fingerprints != 2 || policies != 2 {
		t.Fatalf("migrated row counts = fingerprints %d policies %d, want 2/2", fingerprints, policies)
	}
}
