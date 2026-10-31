package trust

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"path/filepath"
	"testing"
	"time"
)

var serialNumber int64

func certificate(t *testing.T, cn string, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serialNumber++
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serialNumber), Subject: pkix.Name{CommonName: cn},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: parent == nil, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
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

// chainOf returns the chain a printer with the given serial number presents:
// its certificate signed by ca, and ca.
func chainOf(t *testing.T, serial string, ca *x509.Certificate, caKey *ecdsa.PrivateKey) []*x509.Certificate {
	leaf, _ := certificate(t, serial, ca, caKey)
	return []*x509.Certificate{leaf, ca}
}

func TestFirstUsePinsThePrinter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "printer-ca.pem")
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	bbl, bblKey := certificate(t, "BBL CA", nil, nil)
	rogue, rogueKey := certificate(t, "BBL CA", nil, nil) // looks the same

	if err = s.Check(chainOf(t, "A1SERIAL", bbl, bblKey), "a1serial"); err != nil {
		t.Fatalf("first connection: %v", err)
	}
	if err = s.Check(chainOf(t, "A1SERIAL", bbl, bblKey), "A1SERIAL"); err != nil {
		t.Fatalf("second connection: %v", err)
	}
	if err = s.Check(chainOf(t, "A1SERIAL", rogue, rogueKey), "A1SERIAL"); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("impostor with its own CA: %v", err)
	}
	if err = s.Check(chainOf(t, "OTHER", bbl, bblKey), "A1SERIAL"); err == nil {
		t.Fatal("accepted the certificate of another printer")
	}

	// Another printer with another CA is pinned to that one.
	if err = s.Check(chainOf(t, "H2SERIAL", rogue, rogueKey), "H2SERIAL"); err != nil {
		t.Fatalf("second printer: %v", err)
	}
	if s.Verify(chainOf(t, "A1SERIAL", rogue, rogueKey), "A1SERIAL") || !s.Verify(chainOf(t, "A1SERIAL", bbl, bblKey), "A1SERIAL") {
		t.Fatal("Verify ignores the pins")
	}

	// A restart keeps the CAs and the pins.
	loaded, err := Load(path)
	if err != nil || loaded.Count() != 2 {
		t.Fatalf("loaded %d CAs, %v", loaded.Count(), err)
	}
	if err = loaded.Check(chainOf(t, "A1SERIAL", rogue, rogueKey), "A1SERIAL"); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("pin lost on restart: %v", err)
	}
}

// printer-ca.pem written before pinning lists CAs only; the printers are
// pinned on their next connection.
func TestCAsWithoutPins(t *testing.T) {
	bbl, bblKey := certificate(t, "BBL CA", nil, nil)
	other, otherKey := certificate(t, "Other CA", nil, nil)
	s := &Store{cas: []*authority{{cert: bbl}}}
	if !s.Verify(chainOf(t, "A1SERIAL", bbl, bblKey), "A1SERIAL") {
		t.Fatal("a known CA does not verify")
	}
	if err := s.Check(chainOf(t, "A1SERIAL", bbl, bblKey), "A1SERIAL"); err != nil {
		t.Fatal(err)
	}
	if err := s.Check(chainOf(t, "A1SERIAL", other, otherKey), "A1SERIAL"); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("not pinned on its connection: %v", err)
	}
}

func TestChainWithoutCARefused(t *testing.T) {
	bbl, bblKey := certificate(t, "BBL CA", nil, nil)
	s := &Store{}
	if err := s.Check(chainOf(t, "A1SERIAL", bbl, bblKey)[:1], "A1SERIAL"); err == nil {
		t.Fatal("learned nothing but accepted")
	}
}

// Bambu CAs expire before the certificates they sign; an expired CA still
// identifies its printers.
func TestExpiredCAStillIdentifies(t *testing.T) {
	now := time.Now()
	issue := func(cn string, from, to time.Time, parent *x509.Certificate, parentKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey) {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		serialNumber++
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(serialNumber), Subject: pkix.Name{CommonName: cn}, NotBefore: from, NotAfter: to,
			IsCA: parent == nil, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		}
		if parent == nil {
			parent, parentKey = tmpl, key
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &key.PublicKey, parentKey)
		if err != nil {
			t.Fatal(err)
		}
		c, _ := x509.ParseCertificate(der)
		return c, key
	}
	ca, caKey := issue("BBL CA", now.AddDate(-10, 0, 0), now.AddDate(-1, 0, 0), nil, nil)
	leaf, _ := issue("A1SERIAL", now.AddDate(-3, 0, 0), now.AddDate(7, 0, 0), ca, caKey)
	s := &Store{}
	for range 2 { // first use, then as the pinned CA
		if err := s.Check([]*x509.Certificate{leaf, ca}, "A1SERIAL"); err != nil {
			t.Fatalf("expired CA refused: %v", err)
		}
	}
}
