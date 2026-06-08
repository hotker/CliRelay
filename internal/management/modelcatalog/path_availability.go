package modelcatalog

import (
	"sort"
	"strings"

	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/registry"
	internalrouting "github.com/router-for-me/CLIProxyAPI/v6/internal/routing"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/usage"
)

// Path availability contract:
// - Owner: model path capability boundary.
// - Responsibility: expose routing/path-specific model availability derived from registry metadata.
func (s *Service) PathAvailability() map[string]any {
	modelRegistry := registry.GetGlobalRegistry()
	items := make(map[string]*modelPathAvailabilityResponse)

	rootOpenAICapabilities := openAIV1Capabilities("/")
	rootGeminiCapabilities := geminiV1BetaCapabilities("/")
	appendModelPaths(items, s.modelRootRouteScopedModels(modelRegistry.GetAvailableModels("openai")), "/", rootOpenAICapabilities)
	appendModelPaths(items, s.modelRootRouteScopedModels(modelRegistry.GetAvailableModels("gemini")), "/", rootGeminiCapabilities)

	routes := []modelPathRouteResponse{
		{
			Label:        "系统默认",
			Path:         "/",
			System:       true,
			ReadOnly:     true,
			Capabilities: append(append([]modelPathCapabilityResponse{}, rootOpenAICapabilities...), rootGeminiCapabilities...),
		},
	}

	for _, route := range configuredPathRoutes(s.cfg) {
		capabilities := append(append([]modelPathCapabilityResponse{}, openAIV1Capabilities(route.Path)...), geminiV1BetaCapabilities(route.Path)...)
		routes = append(routes, modelPathRouteResponse{
			Label:        route.Label,
			Path:         route.Path,
			Group:        route.Group,
			System:       false,
			ReadOnly:     false,
			Capabilities: capabilities,
		})
		appendModelPaths(items, s.modelPathRouteScopedModels(modelRegistry.GetAvailableModels("openai"), route.Group), route.Path, openAIV1Capabilities(route.Path))
		appendModelPaths(items, s.modelPathRouteScopedModels(modelRegistry.GetAvailableModels("gemini"), route.Group), route.Path, geminiV1BetaCapabilities(route.Path))
	}

	return map[string]any{
		"object": "list",
		"data":   sortModelPathAvailabilityRows(items),
		"routes": routes,
	}
}

func (s *Service) modelPathRouteScopedModels(models []map[string]any, routeGroup string) []map[string]any {
	routeGroup = internalrouting.NormalizeGroupName(routeGroup)
	if s == nil || s.authManager == nil || routeGroup == "" {
		return models
	}
	filtered := make([]map[string]any, 0, len(models))
	for _, model := range models {
		id := strings.TrimSpace(modelPathStringValue(model["id"]))
		if id == "" {
			continue
		}
		if s.authManager.CanServeModelWithScopes(id, nil, nil, routeGroup) {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

func (s *Service) modelRootRouteScopedModels(models []map[string]any) []map[string]any {
	if s == nil || s.authManager == nil || s.cfg == nil || !s.cfg.Routing.IncludeDefaultGroup {
		return models
	}
	filtered := make([]map[string]any, 0, len(models))
	for _, model := range models {
		id := strings.TrimSpace(modelPathStringValue(model["id"]))
		if id == "" {
			continue
		}
		if s.authManager.CanServeModelWithScopes(id, nil, nil, "") {
			filtered = append(filtered, model)
		}
	}
	return filtered
}

func configuredPathRoutes(cfg *config.Config) []configuredModelPathRoute {
	seen := make(map[string]struct{})
	out := []configuredModelPathRoute{}
	appendRoute := func(label, path, group string) {
		path = internalrouting.NormalizeNamespacePath(path)
		group = internalrouting.NormalizeGroupName(group)
		if path == "" || group == "" {
			return
		}
		key := strings.ToLower(path)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		if strings.TrimSpace(label) == "" {
			label = path
		}
		out = append(out, configuredModelPathRoute{
			Label: strings.TrimSpace(label),
			Path:  path,
			Group: group,
		})
	}

	if cfg == nil {
		cfg = &config.Config{}
	}
	cfg.SanitizeRouting()
	for _, route := range cfg.Routing.PathRoutes {
		appendRoute(route.Path, route.Path, route.Group)
	}
	for _, row := range usage.ListCcSwitchImportConfigs() {
		if row.RoutePath == "" || len(row.AllowedChannelGroups) == 0 {
			continue
		}
		appendRoute(row.ProviderName, row.RoutePath, row.AllowedChannelGroups[0])
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Path < out[j].Path
	})
	return out
}
