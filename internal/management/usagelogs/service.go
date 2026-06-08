package usagelogs

import (
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

type Service struct {
	cfg         *config.Config
	authManager *coreauth.Manager
}

func New(cfg *config.Config, authManager *coreauth.Manager) *Service {
	return &Service{cfg: cfg, authManager: authManager}
}
