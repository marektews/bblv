package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRedirectToHTTPS(t *testing.T) {
	for _, tc := range []struct{ https, host, path, want string }{
		{":8443", "192.168.0.10:8080", "/printer/a1?x=1", "https://192.168.0.10:8443/printer/a1?x=1"},
		{":8443", "alpha", "/", "https://alpha:8443/"},
		{":8443", "[fe80::1]:8080", "/api/events", "https://[fe80::1]:8443/api/events"},
		{":443", "bblv.lan:8080", "/settings", "https://bblv.lan/settings"},
	} {
		req := httptest.NewRequest("GET", tc.path, nil)
		req.Host = tc.host
		rec := httptest.NewRecorder()
		RedirectToHTTPS(tc.https).ServeHTTP(rec, req)
		if rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != tc.want {
			t.Errorf("%s%s with HTTPS on %s: %d %q, want %q", tc.host, tc.path, tc.https, rec.Code, rec.Header().Get("Location"), tc.want)
		}
	}
}
