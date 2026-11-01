package tlscert

import (
	"crypto/x509"
	"net"
	"slices"
	"testing"
	"time"
)

func leafOf(t *testing.T, dir string, names Names, now time.Time) *x509.Certificate {
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
	names := Names{Required: []string{"localhost", "alpha", "127.0.0.1", "192.168.0.10"}, Extra: []string{"172.19.32.1"}}
	first := leafOf(t, dir, names, now)
	if !slices.Contains(first.DNSNames, "alpha") || !slices.ContainsFunc(first.IPAddresses, net.ParseIP("192.168.0.10").Equal) {
		t.Fatalf("names %v, addresses %v", first.DNSNames, first.IPAddresses)
	}
	if err := first.VerifyHostname("172.19.32.1"); err != nil {
		t.Fatalf("extra address left out: %v", err)
	}
	if !slices.Contains(first.ExtKeyUsage, x509.ExtKeyUsageServerAuth) || first.NotAfter.Sub(first.NotBefore) > 398*24*time.Hour {
		t.Fatal("not acceptable to browsers")
	}

	// A restart keeps it: every new one makes the browsers warn again.
	if again := leafOf(t, dir, names, now.Add(24*time.Hour)); !again.Equal(first) {
		t.Fatal("certificate replaced without need")
	}
	// So does a virtual adapter that got another address at boot.
	booted := Names{Required: names.Required, Extra: []string{"172.25.16.1"}}
	if again := leafOf(t, dir, booted, now); !again.Equal(first) {
		t.Fatal("certificate replaced for an extra address")
	}
	// Another address in the local network needs a new one.
	moved := Names{Required: append(slices.Clone(names.Required), "10.0.0.5"), Extra: names.Extra}
	if renewed := leafOf(t, dir, moved, now); renewed.Equal(first) {
		t.Fatal("certificate kept without the new address")
	}
	// So does one about to expire.
	current := leafOf(t, dir, moved, now)
	if renewed := leafOf(t, dir, moved, current.NotAfter.Add(-renewBefore/2)); renewed.Equal(current) {
		t.Fatal("certificate not renewed before it expires")
	}
}

func TestLocalNames(t *testing.T) {
	n := LocalNames([]string{"bblv.example.com", "localhost"})
	if n.Required[0] != "localhost" || !slices.Contains(n.Required, "127.0.0.1") || !slices.Contains(n.Required, "bblv.example.com") {
		t.Fatalf("required = %v", n.Required)
	}
	seen := map[string]bool{}
	for _, name := range n.all() {
		if seen[name] {
			t.Fatalf("%s twice in %+v", name, n)
		}
		seen[name] = true
		ip := net.ParseIP(name)
		if ip != nil && ip.To4() == nil && !ip.IsLoopback() {
			t.Fatalf("changing IPv6 address %s in %+v", name, n)
		}
		if ip != nil && ip.IsLinkLocalUnicast() {
			t.Fatalf("link-local address %s in %+v", name, n)
		}
	}
}

func TestKeeperRenewsWhileRunning(t *testing.T) {
	dir := t.TempDir()
	k, err := NewKeeper(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	first, _ := k.GetCertificate(nil)
	k.check(time.Now().Add(time.Hour))
	if same, _ := k.GetCertificate(nil); !same.Leaf.Equal(first.Leaf) {
		t.Fatal("certificate replaced without need")
	}
	k.check(first.Leaf.NotAfter.Add(-renewBefore / 2))
	renewed, _ := k.GetCertificate(nil)
	if renewed.Leaf.Equal(first.Leaf) {
		t.Fatal("certificate not renewed while running")
	}
	if renewed.Leaf.NotAfter.Before(first.Leaf.NotAfter) {
		t.Fatal("renewed certificate expires sooner")
	}
}
