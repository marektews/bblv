// Package tlscert keeps the self-signed certificate bblv serves HTTPS with.
// Browsers warn about it until it is accepted once on every device; it only
// keeps the password and the session cookie from travelling in clear text.
package tlscert

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/marektews/bblv/internal/atomicfile"
)

const (
	CertFile = "https-cert.pem"
	KeyFile  = "https-key.pem"

	// validity stays below the 398 days Apple and browsers accept for
	// server certificates.
	validity = 397 * 24 * time.Hour
	// renewBefore is how long before it expires the certificate is renewed.
	renewBefore = 30 * 24 * time.Hour
)

// Load returns the certificate kept in dir, creating a new one when there
// is none, it expires within renewBefore or it lacks one of names, the host
// names and IP addresses the server is reached under.
func Load(dir string, names []string, now time.Time) (tls.Certificate, error) {
	certPath, keyPath := filepath.Join(dir, CertFile), filepath.Join(dir, KeyFile)
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		if leaf, parseErr := x509.ParseCertificate(cert.Certificate[0]); parseErr == nil &&
			now.Add(renewBefore).Before(leaf.NotAfter) && covers(leaf, names) {
			return cert, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		slog.Warn("HTTPS certificate not loaded, making a new one", "err", err)
	}
	certPEM, keyPEM, err := create(names, now)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err = atomicfile.Write(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	if err = atomicfile.Write(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, err
	}
	slog.Info("made a self-signed HTTPS certificate", "names", names, "file", certPath)
	return tls.X509KeyPair(certPEM, keyPEM)
}

// covers tells whether the certificate names every host name and address.
func covers(c *x509.Certificate, names []string) bool {
	for _, n := range names {
		if ip := net.ParseIP(n); ip != nil {
			if !slices.ContainsFunc(c.IPAddresses, ip.Equal) {
				return false
			}
		} else if !slices.Contains(c.DNSNames, n) {
			return false
		}
	}
	return true
}

func create(names []string, now time.Time) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: "Bambu Live View", Organization: []string{"bblv"}},
		NotBefore:    now.Add(-time.Hour), // tolerates clocks a little behind
		NotAfter:     now.Add(validity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, n := range names {
		if ip := net.ParseIP(n); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, n)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("creating the HTTPS certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), nil
}

// LocalNames returns the names the server can be reached under on this
// computer: localhost, its host name and its IPv4 addresses, followed by
// extra, e.g. the names allowed with -allow-host. IPv6 addresses other than
// ::1 are left out: they change, temporary ones daily, and every change
// would make a new certificate the browsers warn about again.
func LocalNames(extra []string) []string {
	var names []string
	add := func(n string) {
		if n != "" && !slices.Contains(names, n) {
			names = append(names, n)
		}
	}
	add("localhost")
	if h, err := os.Hostname(); err == nil {
		add(h)
	}
	add("127.0.0.1")
	add("::1")
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				add(ipnet.IP.String())
			}
		}
	}
	for _, n := range extra {
		add(n)
	}
	return names
}
