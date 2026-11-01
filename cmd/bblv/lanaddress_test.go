package main

import "testing"

func TestLANAddress(t *testing.T) {
	for _, c := range []struct{ scheme, addr, primary, want string }{
		{"https", ":8443", "192.168.0.5", "https://192.168.0.5:8443"},
		{"https", "0.0.0.0:8443", "192.168.0.5", "https://192.168.0.5:8443"},
		{"http", "[::]:8080", "192.168.0.5", "http://192.168.0.5:8080"},
		{"https", "192.168.0.7:443", "192.168.0.5", "https://192.168.0.7:443"},
		{"https", "[fd00::7]:8443", "", "https://[fd00::7]:8443"},
		{"https", ":8443", "", ""},                     // no network yet
		{"http", "127.0.0.1:18080", "192.168.0.5", ""}, // only this computer
		{"http", "localhost:8080", "192.168.0.5", ""},
		{"https", "8443", "192.168.0.5", ""},
	} {
		if got := lanAddress(c.scheme, c.addr, c.primary); got != c.want {
			t.Errorf("lanAddress(%q, %q, %q) = %q, want %q", c.scheme, c.addr, c.primary, got, c.want)
		}
	}
}
