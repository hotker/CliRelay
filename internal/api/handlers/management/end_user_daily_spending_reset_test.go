package management

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/enduser"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/identity"
	apikeysettings "github.com/router-for-me/CLIProxyAPI/v6/internal/management/settings/apikey"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

func setupEndUserDailySpendingHandlerTest(t *testing.T) (*Handler, identity.Principal, string, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := usage.InitDB(filepath.Join(t.TempDir(), "usage.db"), config.RequestLogStorageConfig{}, time.UTC); err != nil {
		t.Fatalf("InitDB: %v", err)
	}
	t.Cleanup(func() {
		enduser.SetDefault(nil)
		usage.CloseDB()
	})
	db := usage.RuntimeDB()
	if _, err := db.Exec(`
		CREATE TABLE end_users (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			username TEXT NOT NULL,
			username_normalized TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'active',
			must_change_password INTEGER NOT NULL DEFAULT 0,
			password_changed_at TIMESTAMP,
			last_login_at TIMESTAMP,
			failed_login_count INTEGER NOT NULL DEFAULT 0,
			lock_stage INTEGER NOT NULL DEFAULT 0,
			locked_until TIMESTAMP,
			created_at TIMESTAMP NOT NULL,
			updated_at TIMESTAMP NOT NULL,
			version INTEGER NOT NULL DEFAULT 1,
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
	tenantID := uuid.NewString()
	endUserID := uuid.NewString()
	now := time.Now().UTC()
	if _, err := db.Exec(`
		INSERT INTO end_users (
			id, tenant_id, username, username_normalized, display_name, password_hash, daily_spending_limit, created_at, updated_at
		) VALUES (?, ?, 'alice', 'alice', 'Alice', 'unused', 100, ?, ?)
	`, endUserID, tenantID, now, now); err != nil {
		t.Fatalf("insert end user: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO usage_rollup_buckets (
			tenant_id, bucket_kind, bucket_start, end_user_id, model, source, cost_total, updated_at
		) VALUES (?, 'day', ?, ?, 'test-model', 'test', 12.5, ?)
	`, tenantID, usage.LocalDayKeyAt(now), endUserID, now); err != nil {
		t.Fatalf("insert end-user rollup: %v", err)
	}
	enduser.SetDefault(enduser.NewService(db))
	principal := identity.Principal{
		Kind: "user",
		User: identity.User{
			ID:          uuid.NewString(),
			Username:    "admin",
			DisplayName: "Admin",
		},
		EffectiveTenant: identity.Tenant{ID: tenantID},
		Permissions: map[string]bool{
			"end_users.read":  true,
			"end_users.write": true,
		},
	}
	return &Handler{}, principal, tenantID, endUserID
}

func setupPortalAPIKeyDailySpendingHandlerTest(t *testing.T) (*Handler, string, string, string, string) {
	t.Helper()
	h, _, tenantID, endUserID := setupEndUserDailySpendingHandlerTest(t)
	if tenantID == identity.SystemTenantID {
		t.Fatal("portal tenant must differ from system tenant")
	}
	db := usage.RuntimeDB()
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS tenants (
			id TEXT PRIMARY KEY,
			status TEXT NOT NULL,
			type TEXT NOT NULL,
			expires_at TIMESTAMP,
			access_token_ttl_seconds INTEGER,
			refresh_token_ttl_seconds INTEGER
		);
		CREATE TABLE IF NOT EXISTS end_user_sessions (
			id TEXT PRIMARY KEY,
			end_user_id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			access_token_hash TEXT NOT NULL UNIQUE,
			refresh_token_hash TEXT NOT NULL UNIQUE,
			access_expires_at TIMESTAMP NOT NULL,
			refresh_expires_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			last_seen_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			revoked_at TIMESTAMP,
			revoke_reason TEXT NOT NULL DEFAULT '',
			user_agent_hash TEXT NOT NULL DEFAULT ''
		)
	`); err != nil {
		t.Fatalf("create portal auth tables: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO tenants (id, status, type) VALUES (?, 'active', 'standard')`, tenantID); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}

	keyID := uuid.NewString()
	if err := usage.UpsertAPIKeyForTenant(tenantID, usage.APIKeyRow{
		ID:                 keyID,
		Key:                "sk-portal-reset-tenant",
		Name:               "Portal reset tenant",
		EndUserID:          endUserID,
		DailySpendingLimit: 100,
	}); err != nil {
		t.Fatalf("insert owned API key: %v", err)
	}

	portalToken := "cpt_portal-reset-tenant-test"
	portalTokenSum := sha256.Sum256([]byte(portalToken))
	if _, err := db.Exec(`
		INSERT INTO end_user_sessions (
			id, end_user_id, tenant_id, access_token_hash, refresh_token_hash, access_expires_at, refresh_expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?)
	`, uuid.NewString(), endUserID, tenantID, hex.EncodeToString(portalTokenSum[:]), "unused-portal-reset-refresh-hash",
		time.Now().UTC().Add(time.Hour), time.Now().UTC().Add(2*time.Hour)); err != nil {
		t.Fatalf("insert portal session: %v", err)
	}
	return h, tenantID, endUserID, keyID, portalToken
}

func TestEndUserDailySpendingResetWritesAndListsHistory(t *testing.T) {
	h, principal, tenantID, endUserID := setupEndUserDailySpendingHandlerTest(t)

	resetRecorder := httptest.NewRecorder()
	resetContext, _ := gin.CreateTestContext(resetRecorder)
	resetContext.Set(managementPrincipalKey, principal)
	resetContext.Params = gin.Params{{Key: "id", Value: endUserID}}
	resetContext.Request = httptest.NewRequest(http.MethodPost, "/v0/management/end-users/"+endUserID+"/daily-spending/reset", nil)
	h.PostEndUserDailySpendingReset(resetContext)
	if resetRecorder.Code != http.StatusOK {
		t.Fatalf("reset status = %d, want %d; body=%s", resetRecorder.Code, http.StatusOK, resetRecorder.Body.String())
	}
	var resetBody struct {
		ResetCount int     `json:"daily-spending-reset-count"`
		UsedBefore float64 `json:"effective-used-before"`
		RawToday   float64 `json:"raw-today-cost"`
	}
	if err := json.Unmarshal(resetRecorder.Body.Bytes(), &resetBody); err != nil {
		t.Fatalf("decode reset response: %v", err)
	}
	if resetBody.ResetCount != 1 || resetBody.UsedBefore != 12.5 || resetBody.RawToday != 12.5 {
		t.Fatalf("reset response = %#v, want count=1 used/raw=12.5", resetBody)
	}
	events, err := usage.ListEndUserDailySpendingResetEvents(tenantID, endUserID, 10)
	if err != nil {
		t.Fatalf("list persisted reset events: %v", err)
	}
	if len(events) != 1 || events[0].ActorUserID != principal.User.ID || events[0].ActorUsername != "admin" || events[0].ActorKind != "user" {
		t.Fatalf("persisted events = %#v, want actor from principal", events)
	}

	writeOnly := principal
	writeOnly.Permissions = map[string]bool{"end_users.write": true}
	historyRecorder := httptest.NewRecorder()
	historyContext, _ := gin.CreateTestContext(historyRecorder)
	historyContext.Set(managementPrincipalKey, writeOnly)
	historyContext.Params = gin.Params{{Key: "id", Value: endUserID}}
	historyContext.Request = httptest.NewRequest(http.MethodGet, "/v0/management/end-users/"+endUserID+"/daily-spending/reset-history?limit=1", nil)
	h.GetEndUserDailySpendingResetHistory(historyContext)
	if historyRecorder.Code != http.StatusOK {
		t.Fatalf("history status = %d, want %d; body=%s", historyRecorder.Code, http.StatusOK, historyRecorder.Body.String())
	}
	var historyBody struct {
		Items             []usage.EndUserDailySpendingResetEvent `json:"items"`
		Total             int                                    `json:"total"`
		RawTodayCost      float64                                `json:"raw-today-cost"`
		DailySpendingUsed float64                                `json:"daily-spending-used"`
	}
	if err := json.Unmarshal(historyRecorder.Body.Bytes(), &historyBody); err != nil {
		t.Fatalf("decode history response: %v", err)
	}
	if historyBody.Total != 1 || len(historyBody.Items) != 1 || historyBody.RawTodayCost != 12.5 || historyBody.DailySpendingUsed != 0 {
		t.Fatalf("history response = %#v, want total/items=1 raw=12.5 used=0", historyBody)
	}
	if historyBody.Items[0].EffectiveUsedBefore != 12.5 || historyBody.Items[0].RawTodayCost != 12.5 {
		t.Fatalf("history item = %#v, want reset costs 12.5", historyBody.Items[0])
	}

	listRecorder := httptest.NewRecorder()
	listContext, _ := gin.CreateTestContext(listRecorder)
	listContext.Set(managementPrincipalKey, principal)
	listContext.Request = httptest.NewRequest(http.MethodGet, "/v0/management/end-users", nil)
	h.GetEndUsers(listContext)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body=%s", listRecorder.Code, http.StatusOK, listRecorder.Body.String())
	}
	var listBody struct {
		Items []enduser.User `json:"items"`
	}
	if err := json.Unmarshal(listRecorder.Body.Bytes(), &listBody); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if len(listBody.Items) != 1 || listBody.Items[0].DailySpendingResetCount != 1 || listBody.Items[0].DailySpendingUsed != 0 {
		t.Fatalf("end-user list = %#v, want reset count=1 used=0", listBody.Items)
	}
}

func TestEndUserDailySpendingResetRejectsUnlimitedAccount(t *testing.T) {
	h, principal, _, endUserID := setupEndUserDailySpendingHandlerTest(t)
	if _, err := usage.RuntimeDB().Exec(`UPDATE end_users SET daily_spending_limit = 0 WHERE id = ?`, endUserID); err != nil {
		t.Fatalf("clear limit: %v", err)
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set(managementPrincipalKey, principal)
	ctx.Params = gin.Params{{Key: "id", Value: endUserID}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/end-users/"+endUserID+"/daily-spending/reset", nil)
	h.PostEndUserDailySpendingReset(ctx)
	if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), `"code":"daily_spending_limit_missing"`) {
		t.Fatalf("status/body = %d %s, want 400 daily_spending_limit_missing", recorder.Code, recorder.Body.String())
	}
}

func TestPortalAPIKeyDailySpendingResetAndHistoryUsePortalTenant(t *testing.T) {
	h, tenantID, endUserID, keyID, portalToken := setupPortalAPIKeyDailySpendingHandlerTest(t)

	historyRecorder := httptest.NewRecorder()
	historyContext, _ := gin.CreateTestContext(historyRecorder)
	historyContext.Params = gin.Params{{Key: "id", Value: keyID}}
	historyContext.Request = httptest.NewRequest(http.MethodGet, "/v0/portal/api-keys/"+keyID+"/daily-spending/reset-history?limit=200", nil)
	historyContext.Request.Header.Set("Authorization", "Bearer "+portalToken)
	if got := effectiveTenantID(historyContext); got != identity.SystemTenantID {
		t.Fatalf("effective tenant before portal auth = %q, want system tenant fallback", got)
	}
	h.GetPortalAPIKeyDailySpendingResetHistory(historyContext)
	if historyRecorder.Code != http.StatusOK {
		t.Fatalf("empty history status = %d, want %d; body=%s", historyRecorder.Code, http.StatusOK, historyRecorder.Body.String())
	}
	var emptyHistory struct {
		Items []usage.APIKeyDailySpendingResetEvent `json:"items"`
		Total int                                   `json:"total"`
	}
	if err := json.Unmarshal(historyRecorder.Body.Bytes(), &emptyHistory); err != nil {
		t.Fatalf("decode empty history response: %v", err)
	}
	if emptyHistory.Total != 0 || len(emptyHistory.Items) != 0 {
		t.Fatalf("empty history response = %#v, want no reset events", emptyHistory)
	}

	resetRecorder := httptest.NewRecorder()
	resetContext, _ := gin.CreateTestContext(resetRecorder)
	resetContext.Params = gin.Params{{Key: "id", Value: keyID}}
	resetContext.Request = httptest.NewRequest(http.MethodPost, "/v0/portal/api-keys/"+keyID+"/daily-spending/reset", nil)
	resetContext.Request.Header.Set("Authorization", "Bearer "+portalToken)
	h.PostPortalAPIKeyDailySpendingReset(resetContext)
	if resetRecorder.Code != http.StatusOK {
		t.Fatalf("reset status = %d, want %d; body=%s", resetRecorder.Code, http.StatusOK, resetRecorder.Body.String())
	}

	events, err := usage.ListDailySpendingResetEvents(tenantID, keyID, 10)
	if err != nil {
		t.Fatalf("list portal key reset events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("reset events = %d, want 1", len(events))
	}
	if events[0].TenantID != tenantID || events[0].ActorUserID != endUserID || events[0].ActorKind != "end_user" {
		t.Fatalf("reset event = %#v, want portal tenant/user actor", events[0])
	}
}

func TestEndUserErrorMapsAPIKeySettingsItemNotFoundToNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	endUserError(ctx, apikeysettings.ErrItemNotFound)

	if recorder.Code != http.StatusNotFound || !strings.Contains(recorder.Body.String(), `"code":"not_found"`) {
		t.Fatalf("status/body = %d %s, want 404 not_found", recorder.Code, recorder.Body.String())
	}
}
