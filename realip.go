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
//
// Know what that default costs behind a reverse proxy. If requests arrive
// through nginx, a dev server or anything else in front, the direct peer is
// that proxy, so every client resolves to one address. Nothing errors and
// nothing looks wrong: rate limits, audit trails and ban lists keyed by IP
// simply become shared by everyone at once. A limit of twenty attempts per
// address is then twenty for the whole userbase, and the second person to
// mistype a password is locked out by the first.
//
// That is the price of not trusting a header anyone can forge, and it is the
// right default - but it is only correct once WithTrustedProxies tells this
// middleware which peer is the proxy. Configure it before relying on anything
// keyed by client address.
func RealIP(opts ...RealIPOption) Middleware {
	var cfg realIPConfig
	for _, opt := range opts {
		opt(&cfg)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := directIP(r.RemoteAddr)
			if cfg.trustedPeer(ip) {
				if forwarded := cfg.forwardedIP(r); forwarded != "" {
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

// forwardedIP returns the client IP from the forwarded headers. It walks
// X-Forwarded-For from right to left - the right-most entry is the one appended
// by the nearest proxy - skipping trusted-proxy addresses; the first untrusted
// address is the client. This resists spoofing: a client-supplied left-most
// entry (X-Forwarded-For: 6.6.6.6) is never returned because the proxy appends
// the real address to its right. X-Real-IP, which a proxy sets as a single
// value, is used only as a fallback and only when it is a valid IP. An empty
// string is returned when no valid address is present.
func (c *realIPConfig) forwardedIP(r *http.Request) string {
	if xff := r.Header.Get(headerXForwardedFor); xff != "" {
		parts := strings.Split(xff, ",")
		for i := len(parts) - 1; i >= 0; i-- {
			ip := strings.TrimSpace(parts[i])
			if net.ParseIP(ip) == nil {
				break // malformed chain: do not trust anything further left
			}
			if !c.trustedPeer(ip) {
				return ip
			}
		}
	}
	if v := strings.TrimSpace(r.Header.Get(headerXRealIP)); v != "" {
		if net.ParseIP(v) != nil {
			return v
		}
	}
	return ""
}
