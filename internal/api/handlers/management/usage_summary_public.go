package management

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/api/bodyutil"
	apikeysettings "github.com/router-for-me/CLIProxyAPI/v6/internal/management/settings/apikey"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
	log "github.com/sirupsen/logrus"
)

type usageSummaryResponse struct {
	Found  bool             `json:"found"`
	Range  string           `json:"range"`
	Stats  usageStatsBody   `json:"stats"`
	Limits *usageLimitsBody `json:"limits,omitempty"`
}

type usageStatsBody struct {
	TotalCalls int64   `json:"total_calls"`
	QuotaCost  float64 `json:"quota_cost"`
}

// usageLimitsBody exposes only limits that are configured (>0) plus live usage.
type usageLimitsBody struct {
	DailyLimit         *int     `json:"daily-limit,omitempty"`
	DailyUsed          *int64   `json:"daily-used,omitempty"`
	TotalQuota         *int     `json:"total-quota,omitempty"`
	TotalUsed          *int64   `json:"total-used,omitempty"`
	SpendingLimit      *float64 `json:"spending-limit,omitempty"`
	SpendingUsed       *float64 `json:"spending-used,omitempty"`
	DailySpendingLimit *float64 `json:"daily-spending-limit,omitempty"`
	DailySpendingUsed  *float64 `json:"daily-spending-used,omitempty"`
}

// GetPublicUsageSummary returns today's call count and quota cost for an API key.
// This is a lightweight endpoint designed for CC Switch Provider card polling.
// `found` reflects API Key existence (not disabled), not whether it was used today.
// When the key has daily/total/spending limits, `limits` includes only those fields.
func (h *Handler) GetPublicUsageSummary(c *gin.Context) {
	apiKey := ""
	var req publicLookupRequest

	if c.Request.Method == http.MethodPost {
		body, err := bodyutil.ReadRequestBody(c, publicLookupBodyLimit)
		if err != nil {
			if bodyutil.IsTooLarge(err) {
				c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": "failed to read request body"})
			return
		}
		if trimmed := strings.TrimSpace(string(body)); trimmed != "" {
			if err := json.Unmarshal(body, &req); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
				return
			}
		}
		apiKey = strings.TrimSpace(req.APIKey)
	}

	if apiKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "api_key parameter is required"})
		return
	}

	// Public summary is unauthenticated and only receives the raw API key.
	// Resolve the key's tenant first so multi-tenant deployments do not query
	// the system catalog (which would always return found=false / zero stats).
	// End-user-owned keys share one account pool — aggregate all keys of the owner.
	tenantID := usage.ResolveAPIKeyTenant(apiKey)
	stats, err := usage.QueryStats(usage.LogQueryParams{
		TenantID: tenantID,
		APIKeys:  usage.ExpandPublicLookupAPIKeys(apiKey),
		Days:     1,
	})
	if err != nil {
		log.Warnf("management usage logs: public usage summary query failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query usage summary"})
		return
	}

	row := apikeysettings.NewService(nil, apikeysettings.WithTenantID(tenantID)).GetRow(apiKey)
	found := row != nil && !row.Disabled

	resp := usageSummaryResponse{
		Found: found,
		Range: "today",
		Stats: usageStatsBody{
			TotalCalls: stats.Total,
			QuotaCost:  stats.TotalCost,
		},
		Limits: buildPublicUsageLimits(apiKey, row),
	}
	c.JSON(http.StatusOK, resp)
}

func buildPublicUsageLimits(apiKey string, row *usage.APIKeyRow) *usageLimitsBody {
	if row == nil || row.Disabled {
		return nil
	}
	tenantID := strings.TrimSpace(row.TenantID)
	if tenantID == "" {
		tenantID = usage.ResolveAPIKeyTenant(apiKey)
	}
	effective := usage.EffectiveAPIKeyRowForTenant(tenantID, *row)
	out := &usageLimitsBody{}
	has := false
	if effective.DailyLimit > 0 {
		has = true
		limit := effective.DailyLimit
		out.DailyLimit = &limit
		if n, err := usage.CountTodayByKey(apiKey); err == nil {
			out.DailyUsed = &n
		}
	}
	if effective.TotalQuota > 0 {
		has = true
		limit := effective.TotalQuota
		out.TotalQuota = &limit
		if n, err := usage.CountTotalByKey(apiKey); err == nil {
			out.TotalUsed = &n
		}
	}
	if effective.SpendingLimit > 0 {
		has = true
		limit := effective.SpendingLimit
		out.SpendingLimit = &limit
		if n, err := usage.QueryTotalCostByKey(apiKey); err == nil {
			out.SpendingUsed = &n
		}
	}
	if effective.DailySpendingLimit > 0 {
		has = true
		limit := effective.DailySpendingLimit
		out.DailySpendingLimit = &limit
		if n, err := usage.QueryTodayCostByKey(apiKey); err == nil {
			out.DailySpendingUsed = &n
		}
	}
	if !has {
		return nil
	}
	return out
}
