package usage

import "strings"

const systemTenantID = "00000000-0000-0000-0000-000000000001"

func normalizeTenantID(tenantID string) string {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return systemTenantID
	}
	return tenantID
}
