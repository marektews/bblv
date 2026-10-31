package server

import "testing"

func TestValidHost(t *testing.T) {
	for in, want := range map[string]string{
		"192.168.0.33":      "192.168.0.33",
		"fe80::1":           "fe80::1",
		"[FE80::1]":         "fe80::1",
		"a1.lan":            "a1.lan",
		"printer-1.local.":  "printer-1.local",
		"BambuA1":           "BambuA1",
		"192.168.0.33:8883": "",
		"http://a1.lan":     "",
		"a1.lan/":           "",
		"a1 lan":            "",
		"-a1.lan":           "",
		"a1..lan":           "",
	} {
		got, ok := validHost(in)
		if ok != (want != "") || got != want {
			t.Errorf("validHost(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}
