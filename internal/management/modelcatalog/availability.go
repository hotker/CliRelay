package modelcatalog

import (
	"strings"

	modelconfigsettings "github.com/router-for-me/CLIProxyAPI/v6/internal/management/settings/modelconfig"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	internalrouting "github.com/router-for-me/CLIProxyAPI/v6/internal/routing"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

// Availability contract:
// - Owner: model availability query boundary.
// - Responsibility: turn registry state plus stored capabilities into management-facing availability DTOs.
func (s *Service) ConfiguredAvailability(allowedRaw, allowedGroupsRaw string) map[string]any {
	modelRegistry := registry.GetGlobalRegistry()
	allModels := s.filterModelsByScopes(modelRegistry.GetAvailableModels("openai"), allowedRaw, allowedGroupsRaw)

	allConfigRows := modelconfigsettings.ListAllConfigs()
	configByID := make(map[string]usage.ModelConfigRow, len(allConfigRows))
	for _, row := range allConfigRows {
		configByID[strings.ToLower(strings.TrimSpace(row.ModelID))] = row
	}

	data := make([]map[string]any, 0, len(allModels))
	for _, model := range allModels {
		id, _ := model["id"].(string)
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		entry := map[string]any{
			"id":     id,
			"object": "model",
			"source": "registry",
		}
		if ownedBy, exists := model["owned_by"]; exists {
			entry["owned_by"] = ownedBy
		}
		if row, ok := configByID[strings.ToLower(id)]; ok {
			attachModelConfigCapabilities(entry, row)
			entry["pricing"] = map[string]any{
				"mode":                          row.PricingMode,
				"input_price_per_million":       row.InputPricePerMillion,
				"output_price_per_million":      row.OutputPricePerMillion,
				"cached_price_per_million":      row.CachedPricePerMillion,
				"cache_read_price_per_million":  row.CacheReadPricePerMillion,
				"cache_write_price_per_million": row.CacheWritePricePerMillion,
				"price_per_call":                row.PricePerCall,
			}
			if row.Description != "" {
				entry["description"] = row.Description
			}
			if row.Source != "" {
				entry["metadata_source"] = row.Source
			}
		}
		data = append(data, entry)
	}

	activeRows := modelconfigsettings.ListConfigs("active")
	activeMetadata := make([]map[string]any, 0, len(activeRows))
	for _, row := range activeRows {
		activeMetadata = append(activeMetadata, map[string]any{
			"id":       row.ModelID,
			"owned_by": row.OwnedBy,
			"source":   row.Source,
			"enabled":  row.Enabled,
		})
	}

	return map[string]any{
		"object":          "list",
		"scoped":          s.authManager != nil,
		"data":            data,
		"active_metadata": activeMetadata,
	}
}

func (s *Service) Models(allowedRaw, allowedGroupsRaw string) map[string]any {
	modelRegistry := registry.GetGlobalRegistry()
	allModels := s.filterModelsByScopes(modelRegistry.GetAvailableModels("openai"), allowedRaw, allowedGroupsRaw)

	pricingMap := usage.GetAllModelPricing()
	filteredModels := make([]map[string]any, len(allModels))
	for i, model := range allModels {
		filteredModel := map[string]any{
			"id":     model["id"],
			"object": model["object"],
		}
		if created, exists := model["created"]; exists {
			filteredModel["created"] = created
		}
		if ownedBy, exists := model["owned_by"]; exists {
			filteredModel["owned_by"] = ownedBy
		}
		if modelID, ok := model["id"].(string); ok {
			if row, exists := modelconfigsettings.GetConfig(modelID); exists {
				attachModelConfigCapabilities(filteredModel, row)
			}
			if pricing, exists := pricingMap[modelID]; exists {
				filteredModel["pricing"] = map[string]any{
					"input_price_per_million":  pricing.InputPricePerMillion,
					"output_price_per_million": pricing.OutputPricePerMillion,
					"cached_price_per_million": pricing.CachedPricePerMillion,
				}
			}
		}
		filteredModels[i] = filteredModel
	}

	return map[string]any{
		"object": "list",
		"data":   filteredModels,
	}
}

func (s *Service) filterModelsByScopes(models []map[string]any, allowedRaw, allowedGroupsRaw string) []map[string]any {
	allowedRaw = strings.TrimSpace(allowedRaw)
	allowedGroups := internalrouting.ParseNormalizedSet(strings.TrimSpace(allowedGroupsRaw), internalrouting.NormalizeGroupName)
	if s == nil || s.authManager == nil {
		return models
	}
	if allowedRaw != "" && allowedRaw != "*" && !strings.EqualFold(allowedRaw, "all") {
		allowed := make(map[string]struct{})
		for _, part := range strings.Split(allowedRaw, ",") {
			key := strings.ToLower(strings.TrimSpace(part))
			if key == "" {
				continue
			}
			allowed[key] = struct{}{}
		}
		if len(allowed) == 0 {
			return models
		}
		filtered := make([]map[string]any, 0, len(models))
		for _, model := range models {
			id, _ := model["id"].(string)
			if id == "" {
				continue
			}
			if s.authManager.CanServeModelWithScopes(id, allowed, allowedGroups, "") {
				filtered = append(filtered, model)
			}
		}
		return filtered
	}
	if len(allowedGroups) == 0 {
		return models
	}
	filtered := make([]map[string]any, 0, len(models))
	for _, model := range models {
		id, _ := model["id"].(string)
		if id == "" {
			continue
		}
		if s.authManager.CanServeModelWithScopes(id, nil, allowedGroups, "") {
			filtered = append(filtered, model)
		}
	}
	return filtered
}
