package routes

import (
	"github.com/gin-gonic/gin"
	managementhandlers "github.com/router-for-me/CLIProxyAPI/v6/internal/api/handlers/management"
)

func registerManagementAPIKeyRoutes(group *gin.RouterGroup, h *managementhandlers.Handler) {
	group.GET("/api-keys", h.GetAPIKeys)
	group.PUT("/api-keys", h.PutAPIKeys)
	group.PATCH("/api-keys", h.PatchAPIKeys)
	group.DELETE("/api-keys", h.DeleteAPIKeys)

	group.GET("/api-key-permission-profiles", h.GetAPIKeyPermissionProfiles)
	group.PUT("/api-key-permission-profiles", h.PutAPIKeyPermissionProfiles)

	group.GET("/api-key-entries", h.GetAPIKeyEntries)
	group.PUT("/api-key-entries", h.PutAPIKeyEntries)
	group.PATCH("/api-key-entries", h.PatchAPIKeyEntry)
	group.DELETE("/api-key-entries", h.DeleteAPIKeyEntry)

	group.GET("/gemini-api-key", h.GetGeminiKeys)
	group.PUT("/gemini-api-key", h.PutGeminiKeys)
	group.PATCH("/gemini-api-key", h.PatchGeminiKey)
	group.DELETE("/gemini-api-key", h.DeleteGeminiKey)
}

func registerManagementProviderRoutes(group *gin.RouterGroup, h *managementhandlers.Handler) {
	group.GET("/claude-api-key", h.GetClaudeKeys)
	group.PUT("/claude-api-key", h.PutClaudeKeys)
	group.PATCH("/claude-api-key", h.PatchClaudeKey)
	group.DELETE("/claude-api-key", h.DeleteClaudeKey)

	group.GET("/bedrock-api-key", h.GetBedrockKeys)
	group.PUT("/bedrock-api-key", h.PutBedrockKeys)
	group.PATCH("/bedrock-api-key", h.PatchBedrockKey)
	group.DELETE("/bedrock-api-key", h.DeleteBedrockKey)

	group.GET("/opencode-go-api-key", h.GetOpenCodeGoKeys)
	group.PUT("/opencode-go-api-key", h.PutOpenCodeGoKeys)
	group.PATCH("/opencode-go-api-key", h.PatchOpenCodeGoKey)
	group.DELETE("/opencode-go-api-key", h.DeleteOpenCodeGoKey)
	group.POST("/opencode-go-api-key/usage", h.QueryOpenCodeGoUsage)

	group.GET("/codex-api-key", h.GetCodexKeys)
	group.PUT("/codex-api-key", h.PutCodexKeys)
	group.PATCH("/codex-api-key", h.PatchCodexKey)
	group.DELETE("/codex-api-key", h.DeleteCodexKey)

	group.GET("/openai-compatibility", h.GetOpenAICompat)
	group.PUT("/openai-compatibility", h.PutOpenAICompat)
	group.PATCH("/openai-compatibility", h.PatchOpenAICompat)
	group.DELETE("/openai-compatibility", h.DeleteOpenAICompat)

	group.GET("/vertex-api-key", h.GetVertexCompatKeys)
	group.PUT("/vertex-api-key", h.PutVertexCompatKeys)
	group.PATCH("/vertex-api-key", h.PatchVertexCompatKey)
	group.DELETE("/vertex-api-key", h.DeleteVertexCompatKey)

	group.GET("/oauth-excluded-models", h.GetOAuthExcludedModels)
	group.PUT("/oauth-excluded-models", h.PutOAuthExcludedModels)
	group.PATCH("/oauth-excluded-models", h.PatchOAuthExcludedModels)
	group.DELETE("/oauth-excluded-models", h.DeleteOAuthExcludedModels)

	group.GET("/oauth-model-alias", h.GetOAuthModelAlias)
	group.PUT("/oauth-model-alias", h.PutOAuthModelAlias)
	group.PATCH("/oauth-model-alias", h.PatchOAuthModelAlias)
	group.DELETE("/oauth-model-alias", h.DeleteOAuthModelAlias)
}
