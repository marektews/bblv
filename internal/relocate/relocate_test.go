package relocate

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/marektews/bblv/internal/config"
	"github.com/marektews/bblv/internal/discovery"
	"github.com/marektews/bblv/internal/monitor"
)

func certificate(t *testing.T, cn string, ca bool, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: ca, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
	}
	if parent == nil {
		parent, parentKey = tmpl, key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
	if err != nil {
		t.Fatal(err)
	}
	c, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return c, key
}

type fakeMonitor struct {
	online map[string]bool
}

func (m *fakeMonitor) Printers() []monitor.Info {
	var out []monitor.Info
	for id, on := range m.online {
		out = append(out, monitor.Info{ID: id, Online: on})
	}
	return out
}

type fakeConfig struct{ c config.Config }

func (f *fakeConfig) Get() config.Config { return f.c }

func (f *fakeConfig) Update(fn func(c *config.Config) error) error {
	c := f.c
	c.Printers = append([]config.Printer(nil), f.c.Printers...)
	if err := fn(&c); err != nil {
		return err
	}
	f.c = c
	return nil
}

type fakeAnnouncements []discovery.Device

func (a fakeAnnouncements) Devices() []discovery.Device { return a }

const testSerial = "0309CA000000001"

func TestRelocatesAPrinterOnlyToItsCertificate(t *testing.T) {
	ca, caKey := certificate(t, "BBL Device CA", true, nil, nil)
	printerCert, _ := certificate(t, testSerial, false, ca, caKey)
	rogue, _ := certificate(t, testSerial, false, nil, nil) // self-signed, claims the serial number
	chains := map[string][]*x509.Certificate{"10.0.0.5": {printerCert, ca}}
	var probes atomic.Int32 // the scan probes in parallel

	trustFile := filepath.Join(t.TempDir(), "printer-ca.pem")
	cfg := &fakeConfig{c: config.Config{Printers: []config.Printer{{ID: "a1", Host: "10.0.0.5", Serial: testSerial}}}}
	mon := &fakeMonitor{online: map[string]bool{"a1": true}}
	r := New(cfg, mon, nil, trustFile)
	r.probe = func(_ context.Context, host string) []*x509.Certificate {
		probes.Add(1)
		return chains[host]
	}

	// The first probe times out and is repeated at the next check.
	saved := chains
	chains = nil
	t0 := time.Now()
	r.check(context.Background(), t0.Add(-checkEvery))
	chains = saved
	r.check(context.Background(), t0)
	if len(r.trust.certs) != 1 || probes.Load() != 2 {
		t.Fatalf("learned %d CAs with %d probes, want the printer's CA from the second probe", len(r.trust.certs), probes.Load())
	}
	r.check(context.Background(), t0)
	if probes.Load() != 2 {
		t.Fatal("probed again after learning the CA")
	}

	// The printer moves; a rogue device claims its serial number at a lower address.
	mon.online["a1"] = false
	chains = map[string][]*x509.Certificate{"10.0.0.7": {rogue}, "10.0.0.9": {printerCert, ca}}
	r.check(context.Background(), t0.Add(time.Minute))
	r.check(context.Background(), t0.Add(2*time.Minute))
	if probes.Load() != 2 {
		t.Fatalf("searched after %d probes while offline for less than %v", probes.Load()-2, offlineFor)
	}
	r.check(context.Background(), t0.Add(3*time.Minute+time.Second))
	if got := cfg.c.Printers[0].Host; got != "10.0.0.9" {
		t.Fatalf("host = %s, want 10.0.0.9", got)
	}

	// Only the rogue device is left: nothing moves, and the search waits.
	chains = map[string][]*x509.Certificate{"10.0.0.7": {rogue}}
	cfg.c.Printers[0].Host = "10.0.0.5"
	r.check(context.Background(), t0.Add(4*time.Minute))
	r.check(context.Background(), t0.Add(7*time.Minute))
	if got := cfg.c.Printers[0].Host; got != "10.0.0.5" {
		t.Fatalf("moved to the rogue device at %s", got)
	}
	before := probes.Load()
	r.check(context.Background(), t0.Add(8*time.Minute))
	if probes.Load() != before {
		t.Fatalf("searched again within %v", searchAgain)
	}

	// The CA survives a restart.
	loaded, err := loadTrust(trustFile)
	if err != nil || !loaded.verify([]*x509.Certificate{printerCert, ca}, testSerial) || loaded.verify([]*x509.Certificate{rogue}, testSerial) {
		t.Fatalf("loaded trust: %d CAs, %v", len(loaded.certs), err)
	}
}

func TestRelocatesToTheAnnouncedAddress(t *testing.T) {
	ca, caKey := certificate(t, "BBL CA", true, nil, nil)
	printerCert, _ := certificate(t, testSerial, false, ca, caKey)
	cfg := &fakeConfig{c: config.Config{Printers: []config.Printer{{ID: "a1", Host: "10.0.0.5", Serial: testSerial}}}}
	mon := &fakeMonitor{online: map[string]bool{"a1": false}}
	r := New(cfg, mon, fakeAnnouncements{{Serial: testSerial, Host: "10.1.0.20"}}, "")
	r.trust.certs = []*x509.Certificate{ca}
	var mu sync.Mutex
	var probed []string
	r.probe = func(_ context.Context, host string) []*x509.Certificate {
		mu.Lock()
		probed = append(probed, host)
		mu.Unlock()
		if host == "10.1.0.20" {
			return []*x509.Certificate{printerCert, ca}
		}
		return nil
	}
	t0 := time.Now()
	r.check(context.Background(), t0)
	r.check(context.Background(), t0.Add(offlineFor))
	if got := cfg.c.Printers[0].Host; got != "10.1.0.20" || len(probed) != 1 {
		t.Fatalf("host = %s after probing %v, want the announced address without a scan", got, probed)
	}
}

func TestSubnet(t *testing.T) {
	if p, ok := subnet("192.168.4.46"); !ok || p != "192.168.4" {
		t.Fatalf("subnet = %q, %v", p, ok)
	}
	if _, ok := subnet("printer.local"); ok {
		t.Fatal("a host name has no subnet")
	}
}
