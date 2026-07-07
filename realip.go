package middlewares

import (
	"context"
	"net"
	"net/http"
	"strings"
)

// Headers consulted for the client address, in order, when the direct peer is a
// trusted proxy.
const (
	headerXForwardedFor = "X-Forwarded-For"
	headerXRealIP       = "X-Real-IP"
)

type realIPConfig struct {
	trusted []*net.IPNet
}

// RealIPOption configures the RealIP middleware.
type RealIPOption func(*realIPConfig)

// WithTrustedProxies marks the given CIDR ranges as trusted proxies. Forwarded
// headers are honored only when the direct peer (RemoteAddr) falls inside one
// of them. Invalid CIDRs are ignored.
func WithTrustedProxies(cidrs ...string) RealIPOption {
	return func(c *realIPConfig) {
		for _, s := range cidrs {
			if _, ipnet, err := net.ParseCIDR(s); err == nil {
				c.trusted = append(c.trusted, ipnet)
			}
		}
	}
}

// TrustLoopbackProxies trusts the loopback ranges (127.0.0.0/8 and ::1/128),
// which is convenient for local development behind a reverse proxy.
func TrustLoopbackProxies() RealIPOption {
	return func(c *realIPConfig) {
		for _, s := range []string{"127.0.0.0/8", "::1/128"} {
			if _, ipnet, err := net.ParseCIDR(s); err == nil {
				c.trusted = append(c.trusted, ipnet)
			}
		}
	}
}

// RealIP resolves the client IP and stores it in the request context, readable
// with RealIPFrom. By default it uses the direct peer address (RemoteAddr) and
// ignores forwarded headers, which is the safe choice: without a trusted proxy
// policy a client can spoof X-Forwarded-For. Configure WithTrustedProxies (or
// TrustLoopbackProxies) to honor forwarded headers from known proxies.
func RealIP(opts ...RealIPOption) Middleware {
	var cfg realIPConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := directIP(r.RemoteAddr)
			if cfg.trustedPeer(ip) {
				if forwarded := forwardedIP(r); forwarded != "" {
					ip = forwarded
				}
			}
			ctx := context.WithValue(r.Context(), realIPKey, ip)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func (c *realIPConfig) trustedPeer(ip string) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, n := range c.trusted {
		if n.Contains(parsed) {
			return true
		}
	}
	return false
}

// directIP extracts the host part of a RemoteAddr ("ip:port" or a bare ip).
func directIP(remoteAddr string) string {
	if host, _, err := net.SplitHostPort(remoteAddr); err == nil {
		return host
	}
	return remoteAddr
}

// forwardedIP returns the client IP from the forwarded headers, preferring
// X-Real-IP and otherwise the left-most entry of X-Forwarded-For. It returns an
// empty string when no valid address is present.
func forwardedIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get(headerXRealIP)); v != "" {
		if net.ParseIP(v) != nil {
			return v
		}
	}
	if xff := r.Header.Get(headerXForwardedFor); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		first = strings.TrimSpace(first)
		if net.ParseIP(first) != nil {
			return first
		}
	}
	return ""
}
