package tlscert

import (
	"crypto/x509"
	"net"
	"slices"
	"testing"
	"time"
)

func leafOf(t *testing.T, dir string, names []string, now time.Time) *x509.Certificate {
	t.Helper()
	cert, err := Load(dir, names, now)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	return leaf
}

func TestCertificateIsKeptUntilItHasToChange(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	names := []string{"localhost", "alpha", "127.0.0.1", "192.168.0.10"}
	first := leafOf(t, dir, names, now)
	if !slices.Contains(first.DNSNames, "alpha") || !slices.ContainsFunc(first.IPAddresses, net.ParseIP("192.168.0.10").Equal) {
		t.Fatalf("names %v, addresses %v", first.DNSNames, first.IPAddresses)
	}
	if err := first.VerifyHostname("192.168.0.10"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(first.ExtKeyUsage, x509.ExtKeyUsageServerAuth) || first.NotAfter.Sub(first.NotBefore) > 398*24*time.Hour {
		t.Fatal("not acceptable to browsers")
	}

	// A restart keeps it: every new one makes the browsers warn again.
	if again := leafOf(t, dir, names, now.Add(24*time.Hour)); !again.Equal(first) {
		t.Fatal("certificate replaced without need")
	}
	// Another address of the server needs a new one.
	if moved := leafOf(t, dir, append(names, "10.0.0.5"), now); moved.Equal(first) {
		t.Fatal("certificate kept without the new address")
	}
	// So does one about to expire.
	current := leafOf(t, dir, names, now)
	if renewed := leafOf(t, dir, names, current.NotAfter.Add(-renewBefore/2)); renewed.Equal(current) {
		t.Fatal("certificate not renewed before it expires")
	}
}

func TestLocalNames(t *testing.T) {
	names := LocalNames([]string{"bblv.example.com", "localhost"})
	if names[0] != "localhost" || !slices.Contains(names, "127.0.0.1") || names[len(names)-1] != "bblv.example.com" {
		t.Fatalf("names = %v", names)
	}
	seen := map[string]bool{}
	for _, n := range names {
		if seen[n] {
			t.Fatalf("%s twice in %v", n, names)
		}
		seen[n] = true
		if ip := net.ParseIP(n); ip != nil && ip.To4() == nil && !ip.IsLoopback() {
			t.Fatalf("changing IPv6 address %s in %v", n, names)
		}
	}
}
