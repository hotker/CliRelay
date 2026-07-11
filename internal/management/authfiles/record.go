package authfiles

import (
	"path/filepath"
	"strings"
	"time"

	coreauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
)

type RecordOptions struct {
	AuthDir  string
	TenantID string
	Path     string
	Provider string
	Metadata map[string]any
	Existing *coreauth.Auth
	Now      time.Time
}

func BuildRecord(opts RecordOptions) *coreauth.Auth {
	path := strings.TrimSpace(opts.Path)
	if path == "" {
		return nil
	}
	now := opts.Now
	if now.IsZero() {
		now = time.Now()
	}
	authID := AuthIDForPath(opts.AuthDir, path)
	if authID == "" {
		authID = path
	}
	lastRefresh, hasLastRefresh := ExtractLastRefreshTimestamp(opts.Metadata)
	auth := &coreauth.Auth{
		ID:       authID,
		TenantID: NormalizeTenantID(opts.TenantID),
		Provider: opts.Provider,
		Prefix:   MetadataString(opts.Metadata, "prefix"),
		ProxyURL: MetadataString(opts.Metadata, "proxy_url", "proxy-url", "proxyUrl"),
		ProxyID:  MetadataString(opts.Metadata, "proxy_id", "proxy-id", "proxyId"),
		FileName: filepath.Base(path),
		Label:    ChannelLabelFromMetadata(opts.Metadata, opts.Provider),
		Status:   coreauth.StatusActive,
		Attributes: map[string]string{
			"path":   path,
			"source": path,
		},
		Metadata:  opts.Metadata,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if hasLastRefresh {
		auth.LastRefreshedAt = lastRefresh
	}
	if opts.Existing != nil {
		auth.CreatedAt = opts.Existing.CreatedAt
		if !hasLastRefresh {
			auth.LastRefreshedAt = opts.Existing.LastRefreshedAt
		}
		auth.NextRefreshAfter = opts.Existing.NextRefreshAfter
		auth.Runtime = opts.Existing.Runtime
	}
	return auth
}

func ChannelLabelFromMetadata(metadata map[string]any, provider string) string {
	if metadata != nil {
		if raw, ok := metadata["label"].(string); ok {
			if label := strings.TrimSpace(raw); label != "" {
				return label
			}
		}
		if raw, ok := metadata["email"].(string); ok {
			if email := strings.TrimSpace(raw); email != "" {
				return email
			}
		}
	}
	return strings.TrimSpace(provider)
}
