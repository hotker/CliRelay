package claude

import (
	"strings"

	internalclaude "github.com/router-for-me/CLIProxyAPI/v6/internal/auth/claude"
)

func MetadataFromTokenStorage(tokenStorage *internalclaude.ClaudeTokenStorage) map[string]any {
	metadata := map[string]any{
		"type": "claude",
	}
	if tokenStorage == nil {
		return metadata
	}
	if email := strings.TrimSpace(tokenStorage.Email); email != "" {
		metadata["email"] = email
	}
	if accessToken := strings.TrimSpace(tokenStorage.AccessToken); accessToken != "" {
		metadata["access_token"] = accessToken
	}
	if refreshToken := strings.TrimSpace(tokenStorage.RefreshToken); refreshToken != "" {
		metadata["refresh_token"] = refreshToken
	}
	if expired := strings.TrimSpace(tokenStorage.Expire); expired != "" {
		metadata["expired"] = expired
	}
	if lastRefresh := strings.TrimSpace(tokenStorage.LastRefresh); lastRefresh != "" {
		metadata["last_refresh"] = lastRefresh
	}
	return metadata
}
