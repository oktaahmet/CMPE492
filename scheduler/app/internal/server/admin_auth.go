package server

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

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
		if subtle.ConstantTimeCompare([]byte(incoming), []byte(adminToken)) != 1 {
			http.Error(w, "invalid bearer token", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func bearerTokenFromHeader(value string) (string, bool) {
	value = strings.TrimSpace(value)
	const prefix = "Bearer "
	if !strings.HasPrefix(value, prefix) {
		return "", false
	}
	token := strings.TrimSpace(strings.TrimPrefix(value, prefix))
	return token, token != ""
}
