package util

import (
	"net"
	"net/http"
	"strings"
)

// ForwardedClientIP extracts the original client IP from common CDN/proxy headers.
// It is intended for audit/log display only; access control should continue to use
// the framework's trusted-proxy-aware client IP.
func ForwardedClientIP(req *http.Request) (string, string) {
	if req == nil || req.Header == nil {
		return "", ""
	}
	headerPriority := []string{
		"CF-Connecting-IP",
		"True-Client-IP",
		"X-Real-IP",
		"X-Forwarded-For",
	}
	for _, header := range headerPriority {
		if ip := firstHeaderIP(req.Header, header); ip != "" {
			return ip, header
		}
	}
	if ip := forwardedHeaderIP(req.Header); ip != "" {
		return ip, "Forwarded"
	}
	return "", ""
}

func firstHeaderIP(headers http.Header, header string) string {
	for _, value := range headers.Values(header) {
		for _, candidate := range strings.Split(value, ",") {
			if ip := normalizeIPCandidate(candidate); ip != "" {
				return ip
			}
		}
	}
	return ""
}

func forwardedHeaderIP(headers http.Header) string {
	for _, value := range headers.Values("Forwarded") {
		for _, entry := range strings.Split(value, ",") {
			for _, part := range strings.Split(entry, ";") {
				key, rawValue, ok := strings.Cut(part, "=")
				if !ok || !strings.EqualFold(strings.TrimSpace(key), "for") {
					continue
				}
				if ip := normalizeIPCandidate(rawValue); ip != "" {
					return ip
				}
			}
		}
	}
	return ""
}

// RemoteAddrIP extracts and normalizes the IP part of net/http Request.RemoteAddr.
func RemoteAddrIP(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return normalizeIPCandidate(host)
	}
	return normalizeIPCandidate(remoteAddr)
}

func normalizeIPCandidate(candidate string) string {
	candidate = strings.TrimSpace(candidate)
	if candidate == "" {
		return ""
	}
	candidate = strings.Trim(candidate, `"`)
	if candidate == "" || strings.EqualFold(candidate, "unknown") {
		return ""
	}
	if strings.HasPrefix(candidate, "[") && strings.Contains(candidate, "]") {
		if end := strings.Index(candidate, "]"); end >= 0 {
			candidate = candidate[1:end]
		}
	} else if host, _, err := net.SplitHostPort(candidate); err == nil {
		candidate = host
	}
	ip := net.ParseIP(strings.TrimSpace(candidate))
	if ip == nil {
		return ""
	}
	return ip.String()
}
