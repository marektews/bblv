// Package tlscert keeps the self-signed certificate bblv serves HTTPS with.
// Browsers warn about it until it is accepted once on every device; it only
// keeps the password and the session cookie from travelling in clear text.
package tlscert

import (
	"context"
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
	"sync/atomic"
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
	// checkEvery is how often a running server checks its certificate.
	checkEvery = 24 * time.Hour
)

// Names are the host names and IP addresses a certificate is made for.
// Every new certificate makes the browsers warn again, so only a missing
// Required name causes one; Extra names, further addresses of the computer,
// go into a certificate made anyway.
type Names struct {
	Required []string
	Extra    []string
}

func (n Names) all() []string {
	out := slices.Clone(n.Required)
	for _, e := range n.Extra {
		if !slices.Contains(out, e) {
			out = append(out, e)
		}
	}
	return out
}

// Load returns the certificate kept in dir, creating a new one when there
// is none, it expires within renewBefore or it lacks one of names.Required.
func Load(dir string, names Names, now time.Time) (tls.Certificate, error) {
	certPath, keyPath := filepath.Join(dir, CertFile), filepath.Join(dir, KeyFile)
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		if leaf, parseErr := x509.ParseCertificate(cert.Certificate[0]); parseErr == nil &&
			now.Add(renewBefore).Before(leaf.NotAfter) && len(uncovered(leaf, names.Required)) == 0 {
			return cert, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		slog.Warn("HTTPS certificate not loaded, making a new one", "err", err)
	}
	all := names.all()
	certPEM, keyPEM, err := create(all, now)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err = atomicfile.Write(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, err
	}
	if err = atomicfile.Write(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, err
	}
	slog.Info("made a self-signed HTTPS certificate", "names", all, "file", certPath)
	return tls.X509KeyPair(certPEM, keyPEM)
}

// uncovered returns the host names and addresses the certificate lacks.
func uncovered(c *x509.Certificate, names []string) []string {
	var out []string
	for _, n := range names {
		if ip := net.ParseIP(n); ip != nil {
			if !slices.ContainsFunc(c.IPAddresses, ip.Equal) {
				out = append(out, n)
			}
		} else if !slices.Contains(c.DNSNames, n) {
			out = append(out, n)
		}
	}
	return out
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
// computer. Required are localhost, its host name, the loopback addresses,
// allowed (e.g. the names given with -allow-host) and the IPv4 address the
// computer reaches other networks from, i.e. its address in the local
// network. The other IPv4 addresses are Extra: those of virtual adapters,
// such as Hyper-V's Default Switch, change at every boot, and a new
// certificate for each would make the browsers warn again. Link-local and
// point-to-point (VPN) addresses are left out, as are IPv6 addresses
// other than ::1, which change too, temporary ones daily.
func LocalNames(allowed []string) Names {
	var n Names
	add := func(list *[]string, name string) {
		if name != "" && !slices.Contains(n.Required, name) && !slices.Contains(n.Extra, name) {
			*list = append(*list, name)
		}
	}
	add(&n.Required, "localhost")
	if h, err := os.Hostname(); err == nil {
		add(&n.Required, h)
	}
	add(&n.Required, "127.0.0.1")
	add(&n.Required, "::1")
	add(&n.Required, PrimaryIPv4())
	for _, a := range allowed {
		add(&n.Required, a)
	}
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&(net.FlagLoopback|net.FlagPointToPoint) != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil && !ipnet.IP.IsLinkLocalUnicast() {
				add(&n.Extra, ipnet.IP.String())
			}
		}
	}
	return n
}

// PrimaryIPv4 returns the address the computer sends from to other
// networks, "" without a route to them, e.g. while the network is still
// starting. Connecting a UDP socket only chooses the route; nothing is sent.
func PrimaryIPv4() string {
	c, err := net.Dial("udp4", "192.0.2.1:9") // TEST-NET-1, never reached
	if err != nil {
		return ""
	}
	defer c.Close()
	a, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok || a.IP.IsLoopback() || a.IP.IsUnspecified() || a.IP.IsLinkLocalUnicast() {
		return ""
	}
	return a.IP.String()
}

// Keeper serves the certificate and renews it while the server runs, which
// may be longer than the certificate is valid.
type Keeper struct {
	dir     string
	allowed []string
	cert    atomic.Pointer[tls.Certificate]
}

// NewKeeper loads the certificate kept in dir, making one when needed;
// allowed are further names for LocalNames.
func NewKeeper(dir string, allowed []string) (*Keeper, error) {
	k := &Keeper{dir: dir, allowed: allowed}
	names := LocalNames(allowed)
	cert, err := Load(dir, names, time.Now())
	if err != nil {
		return nil, err
	}
	k.cert.Store(&cert)
	if cert.Leaf == nil {
		return k, nil
	}
	if missing := uncovered(cert.Leaf, names.Extra); len(missing) > 0 {
		slog.Info("the HTTPS certificate does not name some addresses of this computer; to make one that does, delete "+
			CertFile+" and "+KeyFile+" in the data directory and restart bblv", "addresses", missing)
	}
	return k, nil
}

// GetCertificate is the tls.Config callback serving the current certificate.
func (k *Keeper) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	return k.cert.Load(), nil
}

// Run checks the certificate every day until ctx ends.
func (k *Keeper) Run(ctx context.Context) {
	tick := time.NewTicker(checkEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			k.check(now)
		}
	}
}

// check replaces the certificate when it expires soon or lacks a required
// name, e.g. after the computer got another address in the local network.
func (k *Keeper) check(now time.Time) {
	cert, err := Load(k.dir, LocalNames(k.allowed), now)
	if err != nil {
		slog.Warn("HTTPS certificate not renewed", "err", err)
		return
	}
	k.cert.Store(&cert)
}
