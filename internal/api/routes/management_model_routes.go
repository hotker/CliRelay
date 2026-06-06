package routes

import (
	"github.com/gin-gonic/gin"
	managementhandlers "github.com/router-for-me/CLIProxyAPI/v6/internal/api/handlers/management"
)

func registerManagementModelRoutes(group *gin.RouterGroup, h *managementhandlers.Handler) {
	group.GET("/models", h.GetModels)
	group.GET("/models/configured-availability", h.GetConfiguredModelAvailability)
	group.GET("/model-path-availability", h.GetModelPathAvailability)
	group.GET("/model-configs", h.GetModelConfigs)
	group.POST("/model-configs", h.PostModelConfig)
	group.PUT("/model-configs/*id", h.PutModelConfig)
	group.DELETE("/model-configs/*id", h.DeleteModelConfig)
	group.GET("/model-owner-presets", h.GetModelOwnerPresets)
	group.PUT("/model-owner-presets", h.PutModelOwnerPresets)
	group.GET("/model-openrouter-sync", h.GetOpenRouterModelSync)
	group.PUT("/model-openrouter-sync", h.PutOpenRouterModelSync)
	group.POST("/model-openrouter-sync/run", h.PostOpenRouterModelSyncRun)
	group.GET("/channel-groups", h.GetChannelGroups)
	group.GET("/ccswitch-import-configs", h.GetCcSwitchImportConfigs)
	group.PUT("/ccswitch-import-configs", h.PutCcSwitchImportConfigs)
	group.GET("/routing-config", h.GetRoutingConfig)
	group.PUT("/routing-config", h.PutRoutingConfig)
	group.GET("/identity-fingerprint", h.GetIdentityFingerprint)
	group.PUT("/identity-fingerprint", h.PutIdentityFingerprint)
	group.GET("/model-pricing", h.GetModelPricing)
	group.PUT("/model-pricing", h.PutModelPricing)
}
