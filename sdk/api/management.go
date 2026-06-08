// Package api exposes helpers for embedding CLIProxyAPI.
//
// It wraps internal management handler types so external projects can integrate
// management endpoints without importing internal packages.
package api

import (
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	"github.com/router-for-me/CLIProxyAPI/v6/sdk/config"
	apisdkbridge "github.com/router-for-me/CLIProxyAPI/v6/sdkbridge/api"
)

// ManagementTokenRequester exposes a limited subset of management endpoints for requesting tokens.
type ManagementTokenRequester = apisdkbridge.ManagementTokenRequester

// NewManagementTokenRequester creates a limited management handler exposing only token request endpoints.
func NewManagementTokenRequester(cfg *config.Config, manager *coreauth.Manager) ManagementTokenRequester {
	return apisdkbridge.NewManagementTokenRequester(cfg, manager)
}
