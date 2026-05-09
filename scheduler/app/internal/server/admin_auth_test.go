package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWithAdminTokenAllowsValidBearerToken(t *testing.T) {
	t.Parallel()

	called := false
	handler := withAdminToken("secret", func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/workflows", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected success status, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatalf("expected wrapped handler to be called")
	}
}

func TestWithAdminTokenRejectsMissingAndInvalidToken(t *testing.T) {
	t.Parallel()

	handler := withAdminToken("secret", func(w http.ResponseWriter, _ *http.Request) {
		t.Fatalf("wrapped handler should not be called")
	})

	for name, header := range map[string]string{
		"missing": "",
		"invalid": "Bearer wrong",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/api/admin/workflows", nil)
			if header != "" {
				req.Header.Set("Authorization", header)
			}
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("expected unauthorized, got %d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestWithAdminTokenRejectsDisabledAdminAPI(t *testing.T) {
	t.Parallel()

	handler := withAdminToken("", func(w http.ResponseWriter, _ *http.Request) {
		t.Fatalf("wrapped handler should not be called")
	})
	req := httptest.NewRequest(http.MethodGet, "/api/admin/workflows", nil)
	req.Header.Set("Authorization", "Bearer anything")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected service unavailable, got %d body=%s", rec.Code, rec.Body.String())
	}
}
