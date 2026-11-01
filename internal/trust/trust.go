// Package trust decides whether a TLS certificate is that of a configured
// printer. A Bambu printer presents, on MQTT, FTPS and its camera port, a
// certificate for its serial number signed by a private Bambu CA, so no
// public chain verifies it. Instead the CA of a printer is learned on its
// first connection (trust on first use) and the printer is pinned to it;
// later connections must present a certificate for its serial number
// signed by that CA. CAs and pins are kept in a PEM file.
package trust

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/marektews/bblv/internal/atomicfile"
)

// pinsHeader names, in the PEM block of a CA, the printers pinned to it.
const pinsHeader = "Printers"

// Store holds the learned CAs; it is safe for concurrent use.
type Store struct {
	path string // "" keeps them only in memory
	// broken tells why the file could not be read. Learning the CAs again
	// would trust whatever device answers first, so then no certificate is
	// accepted and the file is left as it is.
	broken error

	mu  sync.Mutex
	cas []*authority
}

type authority struct {
	cert    *x509.Certificate
	serials []string // upper case
}

// Load reads the CAs kept at path; a missing file gives an empty store. A
// file that cannot be read or parsed gives a store that refuses every
// certificate, with the reason, and never overwrites the file.
func Load(path string) (*Store, error) {
	s := &Store{path: path}
	cas, err := read(path)
	if err != nil {
		s.broken = &FileError{Name: filepath.Base(path), Err: err}
		return s, s.broken
	}
	s.cas = cas
	return s, nil
}

// FileError is returned for every certificate while the file of the CAs
// cannot be read.
type FileError struct {
	Name string // of the file
	Err  error
}

func (e *FileError) Error() string {
	return fmt.Sprintf("%s cannot be read (%v); printers are not connected until it is fixed, or deleted to learn their CAs again, and bblv restarted", e.Name, e.Err)
}

func (e *FileError) Unwrap() error {
	return e.Err
}

func read(path string) ([]*authority, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cas []*authority
	rest := b
	for {
		var block *pem.Block
		if block, rest = pem.Decode(rest); block == nil {
			break
		}
		c, parseErr := x509.ParseCertificate(block.Bytes)
		if parseErr != nil {
			return nil, parseErr
		}
		a := &authority{cert: c}
		for serial := range strings.SplitSeq(block.Headers[pinsHeader], ",") {
			if serial = strings.TrimSpace(serial); serial != "" {
				a.serials = append(a.serials, strings.ToUpper(serial))
			}
		}
		cas = append(cas, a)
	}
	// pem.Decode stops at the first block it cannot read and leaves it.
	if len(bytes.TrimSpace(rest)) > 0 {
		return nil, errors.New("it holds something other than certificates")
	}
	return cas, nil
}

// Count returns the number of known CAs.
func (s *Store) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cas)
}

// ErrUntrusted is returned for a certificate not signed by the CA the
// printer is pinned to.
var ErrUntrusted = errors.New("the certificate is not signed by the CA learned for this printer; if its certificates changed, e.g. with new firmware, delete printer-ca.pem in the data directory and restart bblv")

// Check accepts a chain presented as the printer with the given serial
// number: its certificate must name the serial number and, once the printer
// is pinned to a CA, be signed by it. On the printer's first connection the
// CA the chain comes with is learned and the printer pinned to it.
func (s *Store) Check(chain []*x509.Certificate, serial string) error {
	if len(chain) == 0 {
		return errors.New("no certificate")
	}
	serial = strings.ToUpper(serial)
	if cn := chain[0].Subject.CommonName; !strings.EqualFold(cn, serial) {
		return fmt.Errorf("the certificate is for %q, not the printer %s", cn, serial)
	}
	if s.broken != nil {
		return s.broken
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pinned := s.pinnedLocked(serial)
	for _, a := range s.cas {
		if (pinned == nil || a == pinned) && signs(a.cert, chain) {
			if pinned == nil {
				a.serials = append(a.serials, serial)
				s.saveLocked()
			}
			return nil
		}
	}
	if pinned != nil {
		return ErrUntrusted
	}
	// First use: the chain has to come with the CA that signs it.
	if len(chain) < 2 {
		return errors.New("the certificate comes without its CA")
	}
	roots := x509.NewCertPool()
	for _, c := range chain[1:] {
		roots.AddCert(c)
	}
	chains, err := chain[0].Verify(verifyOptions(chain[0], roots, nil))
	if err != nil {
		return err
	}
	ca := chains[0][len(chains[0])-1]
	i := slices.IndexFunc(s.cas, func(a *authority) bool { return a.cert.Equal(ca) })
	if i < 0 {
		s.cas = append(s.cas, &authority{cert: ca})
		i = len(s.cas) - 1
	}
	s.cas[i].serials = append(s.cas[i].serials, serial)
	slog.Info("trusting the CA of a printer on its first connection", "serial", serial, "ca", ca.Subject.CommonName)
	s.saveLocked()
	return nil
}

// Retain unpins the printers whose serial numbers are not in configured,
// e.g. after they were removed or given another serial number, and drops
// the CAs that no printer is pinned to any more: a CA learned for a device
// that is gone must not vouch for printers added later. CAs that never had
// a pin, kept by versions before pinning, stay until a printer is pinned.
func (s *Store) Retain(configured []string) {
	keep := map[string]bool{}
	for _, serial := range configured {
		keep[strings.ToUpper(serial)] = true
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	kept := s.cas[:0]
	for _, a := range s.cas {
		had := len(a.serials)
		a.serials = slices.DeleteFunc(a.serials, func(serial string) bool { return !keep[serial] })
		changed = changed || len(a.serials) != had
		if had > 0 && len(a.serials) == 0 {
			slog.Info("forgetting a printer CA no configured printer uses", "ca", a.cert.Subject.CommonName)
			continue
		}
		kept = append(kept, a)
	}
	clear(s.cas[len(kept):])
	s.cas = kept
	if changed {
		s.saveLocked()
	}
}

// Verify tells whether a chain presented at some address belongs to the
// printer with the given serial number, without learning anything: it must
// be signed by the CA the printer is pinned to or, for a printer not pinned
// yet, by a known one.
func (s *Store) Verify(chain []*x509.Certificate, serial string) bool {
	if len(chain) == 0 || !strings.EqualFold(chain[0].Subject.CommonName, serial) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pinned := s.pinnedLocked(strings.ToUpper(serial))
	for _, a := range s.cas {
		if (pinned == nil || a == pinned) && signs(a.cert, chain) {
			return true
		}
	}
	return false
}

// TLS returns a configuration for connecting to the printer with the given
// serial number, which checks its certificate with Check. A nil store
// accepts any certificate.
func (s *Store) TLS(serial string) *tls.Config {
	c := &tls.Config{InsecureSkipVerify: true} // checked by VerifyConnection
	if s != nil {
		c.VerifyConnection = func(cs tls.ConnectionState) error {
			return s.Check(cs.PeerCertificates, serial)
		}
	}
	return c
}

func (s *Store) pinnedLocked(serial string) *authority {
	for _, a := range s.cas {
		if slices.Contains(a.serials, serial) {
			return a
		}
	}
	return nil
}

func signs(ca *x509.Certificate, chain []*x509.Certificate) bool {
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	intermediates := x509.NewCertPool()
	for _, c := range chain[1:] {
		intermediates.AddCert(c)
	}
	_, err := chain[0].Verify(verifyOptions(chain[0], roots, intermediates))
	return err == nil
}

// saveLocked writes the file; the CAs stay in memory when that fails.
func (s *Store) saveLocked() {
	if s.path == "" || s.broken != nil {
		return
	}
	var b []byte
	for _, a := range s.cas {
		block := &pem.Block{Type: "CERTIFICATE", Bytes: a.cert.Raw}
		if len(a.serials) > 0 {
			block.Headers = map[string]string{pinsHeader: strings.Join(a.serials, ",")}
		}
		b = append(b, pem.EncodeToMemory(block)...)
	}
	if err := atomicfile.Write(s.path, b, 0o600); err != nil {
		slog.Warn("printer CAs not saved", "file", s.path, "err", err)
	}
}

// verifyOptions checks a printer certificate at the time it was issued:
// what matters is that the CA the printer is pinned to signed it, and Bambu
// CAs expire before the certificates they sign; the BBL CA of the A1 in
// 2032, its printers' certificates in 2035.
func verifyOptions(leaf *x509.Certificate, roots, intermediates *x509.CertPool) x509.VerifyOptions {
	return x509.VerifyOptions{
		Roots: roots, Intermediates: intermediates, CurrentTime: leaf.NotBefore,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageAny},
	}
}
