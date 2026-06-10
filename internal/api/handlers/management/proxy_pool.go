package management

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/config"
	proxypoolsettings "github.com/router-for-me/CLIProxyAPI/v6/internal/management/settings/proxypool"
	"github.com/router-for-me/CLIProxyAPI/v6/internal/util"
	log "github.com/sirupsen/logrus"
)

const (
	defaultProxyCheckFallbackURL = "https://www.gstatic.com/generate_204"
	defaultProxyCheckPublicPath  = "/v0/management/public/ping"
	defaultProxyCheckTimeout     = 8 * time.Second
)

type proxyPoolAPIEntry struct {
	config.ProxyPoolEntry
	MaskedURL string `json:"masked_url"`
}

// GetProxyPool returns reusable proxy entries for the management UI.
func (h *Handler) GetProxyPool(c *gin.Context) {
	var entries []config.ProxyPoolEntry
	if proxypoolsettings.StoreAvailable() {
		entries = proxypoolsettings.List()
	} else if h != nil {
		h.mu.Lock()
		if h.cfg != nil {
			entries = append(entries, h.cfg.ProxyPool...)
		}
		h.mu.Unlock()
	}

	items := make([]proxyPoolAPIEntry, 0, len(entries))
	for _, entry := range entries {
		items = append(items, proxyPoolAPIEntry{
			ProxyPoolEntry: entry,
			MaskedURL:      maskProxyPoolURL(entry.URL),
		})
	}
	c.JSON(http.StatusOK, gin.H{"items": items})
}

// PutProxyPool replaces the reusable proxy list after normalization.
func (h *Handler) PutProxyPool(c *gin.Context) {
	var body struct {
		Items []config.ProxyPoolEntry `json:"items"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		var entries []config.ProxyPoolEntry
		if errArray := c.ShouldBindJSON(&entries); errArray != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
			return
		}
		body.Items = entries
	}

	normalized := config.NormalizeProxyPool(body.Items)
	if len(body.Items) > 0 && len(normalized) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no valid proxy entries"})
		return
	}
	if proxypoolsettings.StoreAvailable() {
		if err := proxypoolsettings.Replace(normalized); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("failed to save proxy pool: %v", err)})
			return
		}
		h.mu.Lock()
		if h.cfg == nil {
			h.cfg = &config.Config{}
		}
		h.cfg.ProxyPool = proxypoolsettings.List()
		h.mu.Unlock()
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
		return
	}

	h.mu.Lock()
	if h.cfg == nil {
		h.cfg = &config.Config{}
	}
	h.cfg.ProxyPool = normalized
	h.mu.Unlock()

	h.persist(c)
}

// GetPublicPing returns a lightweight public 204 endpoint for proxy latency probes.
func (h *Handler) GetPublicPing(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// PostProxyPoolCheck checks whether a proxy can reach the deployed server.
func (h *Handler) PostProxyPoolCheck(c *gin.Context) {
	var body struct {
		ID      string `json:"id"`
		URL     string `json:"url"`
		TestURL string `json:"test_url"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}

	proxyURL := strings.TrimSpace(body.URL)
	if proxyURL == "" && proxypoolsettings.StoreAvailable() {
		if entry := proxypoolsettings.Get(body.ID); entry != nil && entry.Enabled {
			proxyURL = strings.TrimSpace(entry.URL)
		}
	}
	if proxyURL == "" && h != nil && h.cfg != nil {
		proxyURL = h.cfg.ResolveProxyURL(body.ID, "")
	}
	if err := config.ValidateProxyURL(proxyURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	testURL := strings.TrimSpace(body.TestURL)
	if testURL == "" {
		testURL = defaultProxyCheckURL(c.Request)
	}
	if _, err := url.ParseRequestURI(testURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid test_url"})
		return
	}

	var sdkCfg *config.SDKConfig
	if h != nil && h.cfg != nil {
		sdkCfg = &h.cfg.SDKConfig
	}
	started := time.Now()
	statusCode, err := checkProxyConnectivity(c.Request.Context(), proxyURL, testURL, sdkCfg)
	latencyMs := time.Since(started).Milliseconds()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"ok":        false,
			"latencyMs": latencyMs,
			"message":   err.Error(),
		})
		return
	}

	ok := statusCode >= 200 && statusCode < 400
	payload := gin.H{
		"ok":         ok,
		"statusCode": statusCode,
		"latencyMs":  latencyMs,
	}
	c.JSON(http.StatusOK, payload)
}

func checkProxyConnectivity(ctx context.Context, proxyURL string, testURL string, sdkCfg *config.SDKConfig) (int, error) {
	transport := util.BuildProxyTransport(proxyURL, false)
	if transport == nil {
		return 0, fmt.Errorf("failed to build proxy transport")
	}
	util.ApplyTLSConfig(transport, sdkCfg)
	client := &http.Client{Timeout: defaultProxyCheckTimeout, Transport: transport}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, testURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", "CLIProxyAPI proxy checker")
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() {
		if errClose := resp.Body.Close(); errClose != nil {
			log.WithError(errClose).Debug("failed to close proxy check response body")
		}
	}()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
	return resp.StatusCode, nil
}

func defaultProxyCheckURL(r *http.Request) string {
	if r == nil {
		return defaultProxyCheckFallbackURL
	}

	host := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if idx := strings.IndexByte(host, ','); idx >= 0 {
		host = host[:idx]
	}
	if host == "" {
		host = strings.TrimSpace(r.Host)
	}
	if host == "" {
		return defaultProxyCheckFallbackURL
	}

	scheme := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))
	if idx := strings.IndexByte(scheme, ','); idx >= 0 {
		scheme = scheme[:idx]
	}
	if scheme == "" {
		if r.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}

	return scheme + "://" + host + defaultProxyCheckPublicPath
}

func maskProxyPoolURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "***"
	}
	return strings.ToLower(parsed.Scheme) + "://" + parsed.Host
}
