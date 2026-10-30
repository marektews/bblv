package relocate

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// trust holds the CA certificates that sign the printers' certificates,
// kept in a PEM file.
type trust struct {
	path  string // "" keeps them only in memory
	certs []*x509.Certificate
}

func loadTrust(path string) (*trust, error) {
	t := &trust{path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return t, nil
	}
	if err != nil {
		return t, err
	}
	for block, rest := pem.Decode(b); block != nil; block, rest = pem.Decode(rest) {
		c, parseErr := x509.ParseCertificate(block.Bytes)
		if parseErr != nil {
			return t, parseErr
		}
		t.certs = append(t.certs, c)
	}
	return t, nil
}

// add takes the CA certificates of a chain presented by a printer, when they
// sign its certificate, and saves them if any is new.
func (t *trust) add(chain []*x509.Certificate) error {
	cas := x509.NewCertPool()
	for _, c := range chain[1:] {
		cas.AddCert(c)
	}
	if _, err := chain[0].Verify(x509.VerifyOptions{Roots: cas, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}}); err != nil {
		return err
	}
	changed := false
	for _, c := range chain[1:] {
		if !t.has(c) {
			t.certs = append(t.certs, c)
			changed = true
		}
	}
	if !changed || t.path == "" {
		return nil
	}
	return t.save()
}

func (t *trust) has(c *x509.Certificate) bool {
	for _, known := range t.certs {
		if known.Equal(c) {
			return true
		}
	}
	return false
}

// verify tells whether a chain presented at some address belongs to the
// printer with the given serial number.
func (t *trust) verify(chain []*x509.Certificate, serial string) bool {
	if len(chain) == 0 || len(t.certs) == 0 || !strings.EqualFold(chain[0].Subject.CommonName, serial) {
		return false
	}
	roots := x509.NewCertPool()
	for _, c := range t.certs {
		roots.AddCert(c)
	}
	intermediates := x509.NewCertPool()
	for _, c := range chain[1:] {
		intermediates.AddCert(c)
	}
	_, err := chain[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny}})
	return err == nil
}

func (t *trust) save() error {
	var b []byte
	for _, c := range t.certs {
		b = append(b, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})...)
	}
	tmp, err := os.CreateTemp(filepath.Dir(t.path), filepath.Base(t.path)+".*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), t.path)
}
