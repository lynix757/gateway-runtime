package middleware

import (
	"net"
	"net/http"
	"strings"
)

type TrustedProxyConfig struct {
	CIDRs []*net.IPNet
}

func ParseTrustedProxyCIDRs(values []string) (TrustedProxyConfig, error) {
	var cfg TrustedProxyConfig
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		_, network, err := net.ParseCIDR(raw)
		if err != nil {
			return TrustedProxyConfig{}, err
		}
		cfg.CIDRs = append(cfg.CIDRs, network)
	}
	return cfg, nil
}

func ClientIP(r *http.Request, cfg TrustedProxyConfig) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	remoteIP := net.ParseIP(host)
	if remoteIP == nil || !isTrusted(remoteIP, cfg.CIDRs) {
		return host
	}

	xff := r.Header.Get("X-Forwarded-For")
	if xff == "" {
		return host
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		ip := net.ParseIP(strings.TrimSpace(parts[i]))
		if ip == nil {
			continue
		}
		if !isTrusted(ip, cfg.CIDRs) {
			return ip.String()
		}
	}
	return host
}

func isTrusted(ip net.IP, cidrs []*net.IPNet) bool {
	for _, network := range cidrs {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}
