package server

import (
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"testing"
	"time"
)

func TestAllowedHost(t *testing.T) {
	s := &server{hosts: []string{"bblv.example.com"}}
	for host, want := range map[string]bool{
		"":                      true,
		"192.168.0.10:8080":     true,
		"[fe80::1]:8080":        true,
		"localhost:8080":        true,
		"komputer:8080":         true,
		"bblv.local":            true,
		"pc.lan:8080":           true,
		"pc.home.arpa.":         true,
		"BBLV.example.com:443":  true,
		"evil.example:8080":     false,
		"bblv.example.com.evil": false,
		"localhost.evil.com":    false,
	} {
		if got := s.allowedHost(host); got != want {
			t.Errorf("allowedHost(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestOtherHostNamesRejected(t *testing.T) {
	ts := setup(t)
	req, _ := http.NewRequest("GET", ts.URL+"/api/printers", nil)
	req.Host = "evil.example:8080" // a page of evil.example after DNS rebinding
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMisdirectedRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestSecurityHeaders(t *testing.T) {
	ts := setup(t)
	resp, err := http.Get(ts.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	for name, want := range map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Content-Security-Policy": "frame-ancestors 'none'",
		"Referrer-Policy":         "no-referrer",
	} {
		if got := resp.Header.Get(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestStreamEndsWhenPasswordChanges(t *testing.T) {
	old := sessionCheck
	sessionCheck = 20 * time.Millisecond
	t.Cleanup(func() { sessionCheck = old })
	ts := setup(t)
	jar, _ := cookiejar.New(nil)
	user := &http.Client{Jar: jar}
	if resp := send(t, user, "PUT", ts.URL+"/api/config/password", map[string]string{"password": "tajne"}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set password: %d", resp.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events", nil)
	resp, err := user.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("events: %d", resp.StatusCode)
	}

	// Another session (here the same browser) changes the password, which
	// ends every session made before.
	if r := send(t, user, "PUT", ts.URL+"/api/config/password", map[string]string{"current": "tajne", "password": "nowe"}); r.StatusCode != http.StatusNoContent {
		t.Fatalf("change password: %d", r.StatusCode)
	}
	if _, err = io.Copy(io.Discard, resp.Body); err != nil {
		t.Fatalf("stream did not end: %v", err)
	}
}
