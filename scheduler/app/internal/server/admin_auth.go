package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// withAdminToken gates admin-only handlers behind the configured bearer token.
// registerAdminRoutes normally skips these routes when ADMIN_API_TOKEN is empty;
// this check is a second guard for tests and future call sites.
func withAdminToken(adminToken string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if adminToken == "" {
			http.Error(w, "admin api disabled", http.StatusServiceUnavailable)
			return
		}
		incoming, ok := bearerTokenFromHeader(r.Header.Get("Authorization"))
		if !ok {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		// Avoid ordinary string comparison so token checks do not short-circuit on the first mismatched byte.
		if subtle.ConstantTimeCompare([]byte(incoming), []byte(adminToken)) != 1 {
			http.Error(w, "invalid bearer token", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

// bearerTokenFromHeader accepts the standard "Bearer <token>" Authorization
// header form and rejects empty tokens after whitespace trimming.
func bearerTokenFromHeader(value string) (string, bool) {
	value = strings.TrimSpace(value)
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(value, prefix))
	return token, token != ""
}
