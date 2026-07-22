package management

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestPatchEndUserRejectsLegacyPeriodDayConflict(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, principal, _, endUserID := setupEndUserDailySpendingHandlerTest(t)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set(managementPrincipalKey, principal)
	ctx.Params = gin.Params{{Key: "id", Value: endUserID}}
	ctx.Request = httptest.NewRequest(http.MethodPatch, "/v0/management/end-users/"+endUserID, bytes.NewBufferString(`{
		"daily-spending-limit":10,
		"period-spending-limits":{"day":20}
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.PatchEndUser(ctx)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "period_day_legacy_conflict" {
		t.Fatalf("code = %q, want period_day_legacy_conflict", body.Error.Code)
	}
}

func TestPostEndUserAPIKeyRejectsLimitAboveAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, principal, _, endUserID := setupEndUserDailySpendingHandlerTest(t)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Set(managementPrincipalKey, principal)
	ctx.Params = gin.Params{{Key: "id", Value: endUserID}}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v0/management/end-users/"+endUserID+"/api-keys", bytes.NewBufferString(`{
		"name":"too-large",
		"period-spending-limits":{"day":200}
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.PostEndUserAPIKey(ctx)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", recorder.Code, recorder.Body.String())
	}
	var body struct {
		Error struct {
			Code    string `json:"code"`
			Details struct {
				Period       string  `json:"period"`
				KeyLimit     float64 `json:"key_limit"`
				AccountLimit float64 `json:"account_limit"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error.Code != "key_period_limit_exceeds_account" || body.Error.Details.Period != "day" || body.Error.Details.KeyLimit != 200 || body.Error.Details.AccountLimit != 100 {
		t.Fatalf("error = %+v", body.Error)
	}
}
