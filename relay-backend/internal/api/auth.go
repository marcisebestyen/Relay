package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net"
	"net/http"
	"strings"
)

const (
	apiPrefix       = "/api/v1/"
	eventsPath      = "/api/v1/events"
	accessTokenName = "access_token"
	bearerScheme    = "Bearer"
)

// bearerAuth guards /api/v1 with a static token. Other paths (/health,
// /ready, /metrics) stay open. Only GET /api/v1/events also accepts the
// token as a query parameter, because EventSource cannot set headers.
// Digests are compared so the token length does not leak.
func bearerAuth(token string) func(http.Handler) http.Handler {
	want := sha256.Sum256([]byte(token))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !strings.HasPrefix(r.URL.Path, apiPrefix) {
				next.ServeHTTP(w, r)
				return
			}
			got := sha256.Sum256([]byte(requestToken(r)))
			if subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
				w.Header().Set("WWW-Authenticate", bearerScheme)
				WriteError(w, http.StatusUnauthorized, CodeUnauthorized, "missing or invalid API token", nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func requestToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); h != "" {
		scheme, token, _ := strings.Cut(h, " ")
		if strings.EqualFold(scheme, bearerScheme) {
			return token
		}
		return ""
	}
	if r.Method == http.MethodGet && r.URL.Path == eventsPath {
		return r.URL.Query().Get(accessTokenName)
	}
	return ""
}

// LogAuthStatus logs whether auth is on, never the token. With auth off it
// warns when addr is reachable from other hosts.
func LogAuthStatus(logger *slog.Logger, token, addr string) {
	if token != "" {
		logger.Info("auth enabled")
		return
	}
	if isLoopbackAddr(addr) {
		logger.Info("auth disabled")
		return
	}
	logger.Warn("auth disabled and the listen address is not loopback; anyone who can reach it can use the API",
		"addr", addr)
}

// isLoopbackAddr treats an empty host (":8080") and unparsable input as non-loopback.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
