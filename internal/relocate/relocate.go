// Package relocate finds printers again after their address changed, e.g.
// when the DHCP server gave them another one, and updates the configuration.
//
// The MQTT broker of a printer (port 8883) presents a certificate whose
// common name is the printer's serial number, signed by a Bambu Lab CA. The
// CA certificates are taken from the printers at their configured addresses,
// and a printer is trusted at a new address only when its certificate names
// its serial and is signed by one of them: no other device can obtain the
// access code by claiming a serial number. Printers announcing themselves
// with SSDP are found at once; the announcements do not cross routers, so
// the subnet of the last address is searched as well.
package relocate

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"log/slog"
	"maps"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/marektews/bblv/internal/config"
	"github.com/marektews/bblv/internal/discovery"
	"github.com/marektews/bblv/internal/monitor"
	"github.com/marektews/bblv/internal/trust"
)

const (
	checkEvery   = 30 * time.Second
	offlineFor   = 2 * time.Minute  // before a printer is searched for
	searchAgain  = 10 * time.Minute // between scans for the same printer, doubled after every miss
	maxSearchGap = 2 * time.Hour    // up to this
	probeTimeout = 3 * time.Second
	parallel     = 32 // addresses probed at once
)

// Printers is the part of monitor.Monitor the relocator uses. The monitor
// follows the configuration (config.Store.Watch), so saving a new address
// reconnects the printer.
type Printers interface {
	Printers() []monitor.Info
}

// ConfigStore is the part of config.Store the relocator uses.
type ConfigStore interface {
	Get() config.Config
	Update(fn func(c *config.Config) error) error
}

// Announcements lists the printers announcing themselves in the network.
type Announcements interface {
	Devices() []discovery.Device
}

// Relocator follows the addresses of the configured printers.
type Relocator struct {
	cfg   ConfigStore
	mon   Printers
	disc  Announcements // nil without discovery
	cas   *trust.Store
	probe func(ctx context.Context, host string) []*x509.Certificate

	learned  map[string]bool      // printers whose CA was taken in this run, by ID
	offline  map[string]time.Time // since when a printer is offline, by ID
	searched map[string]time.Time // last scan for a printer, by ID
	misses   map[string]int       // scans in a row that did not find a printer, by ID
}

// New creates a relocator that recognises printers by their certificates,
// checked against the CAs in cas; nil keeps them only in memory.
func New(cfg ConfigStore, mon Printers, disc Announcements, cas *trust.Store) *Relocator {
	if cas == nil {
		cas = &trust.Store{}
	}
	return &Relocator{
		cfg: cfg, mon: mon, disc: disc, cas: cas, probe: certificates,
		learned: map[string]bool{}, offline: map[string]time.Time{}, searched: map[string]time.Time{}, misses: map[string]int{},
	}
}

// Run checks the printers until ctx is cancelled.
func (r *Relocator) Run(ctx context.Context) {
	tick := time.NewTicker(checkEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-tick.C:
			r.check(ctx, now)
		}
	}
}

func (r *Relocator) check(ctx context.Context, now time.Time) {
	online := map[string]bool{}
	for _, info := range r.mon.Printers() {
		online[info.ID] = info.Online
	}
	var lost []config.Printer
	for _, p := range r.cfg.Get().Printers {
		switch {
		case p.Serial == "":
		case online[p.ID]:
			delete(r.offline, p.ID)
			delete(r.misses, p.ID)
			if !r.learned[p.ID] {
				r.learned[p.ID] = r.learn(ctx, p)
			}
		default:
			if since, known := r.offline[p.ID]; !known {
				r.offline[p.ID] = now
			} else if now.Sub(since) >= offlineFor {
				lost = append(lost, p)
			}
		}
	}
	if len(lost) > 0 {
		r.search(ctx, lost, now)
	}
}

// learn makes sure the CA of a printer working at its configured address is
// known; the printer's own connections usually learn it first. It tells
// whether the printer answered; if not, e.g. after a timeout, it is asked
// again at the next check.
func (r *Relocator) learn(ctx context.Context, p config.Printer) bool {
	chain := r.probe(ctx, p.Host)
	if chain == nil {
		return false
	}
	if err := r.cas.Check(chain, p.Serial); err != nil {
		slog.Warn("printer certificate not trusted", "printer", p.ID, "host", p.Host, "err", err)
	}
	return true
}

// search looks for lost printers at the addresses they announce, which
// costs a probe and is done at every check, and in the subnets of their last
// addresses, and moves those found. A printer that is simply switched off
// is found nowhere, so its subnet is scanned at growing intervals.
func (r *Relocator) search(ctx context.Context, lost []config.Printer, now time.Time) {
	announced := map[string]string{}
	if r.disc != nil {
		for _, d := range r.disc.Devices() {
			announced[strings.ToUpper(d.Serial)] = d.Host
		}
	}
	candidates := map[string][]*x509.Certificate{} // by host
	subnets := map[string]bool{}
	var scanned []config.Printer
	for _, p := range lost {
		if host := announced[strings.ToUpper(p.Serial)]; host != "" && host != p.Host {
			if chain := r.probe(ctx, host); r.cas.Verify(chain, p.Serial) {
				r.move(p, host)
				continue
			} else if chain != nil {
				candidates[host] = chain // find warns about a device claiming the serial
			}
		}
		if now.Sub(r.searched[p.ID]) < r.searchGap(p.ID) {
			continue
		}
		r.searched[p.ID] = now
		scanned = append(scanned, p)
		if prefix, ok := subnet(p.Host); ok {
			subnets[prefix] = true
		}
	}
	if len(scanned) == 0 {
		return
	}
	for prefix := range subnets {
		slog.Info("searching for offline printers", "subnet", prefix+".0/24")
		maps.Copy(candidates, r.scan(ctx, prefix))
	}
	for _, p := range scanned {
		if host := r.find(candidates, p); host != "" && host != p.Host {
			r.move(p, host)
		} else {
			r.misses[p.ID]++
		}
	}
}

// searchGap is how long after its last scan a lost printer is scanned for
// again: searchAgain after the first scan that missed it, doubled after
// every further one, up to maxSearchGap. A printer switched off for the
// night would otherwise have its subnet probed every few minutes until the
// morning.
func (r *Relocator) searchGap(id string) time.Duration {
	return min(searchAgain<<min(max(r.misses[id]-1, 0), 8), maxSearchGap)
}

// find returns the host whose certificate proves to be the printer's.
func (r *Relocator) find(candidates map[string][]*x509.Certificate, p config.Printer) string {
	for _, host := range slices.Sorted(maps.Keys(candidates)) {
		chain := candidates[host]
		if r.cas.Verify(chain, p.Serial) {
			return host
		}
		if len(chain) > 0 && strings.EqualFold(chain[0].Subject.CommonName, p.Serial) {
			slog.Warn("a device claims a printer's serial number without a certificate of a known printer CA",
				"printer", p.ID, "host", host, "known CAs", r.cas.Count())
		}
	}
	return ""
}

// scan probes every address of a /24 subnet given by its first three octets.
func (r *Relocator) scan(ctx context.Context, prefix string) map[string][]*x509.Certificate {
	var mu sync.Mutex
	var wg sync.WaitGroup
	found := map[string][]*x509.Certificate{}
	sem := make(chan struct{}, parallel)
	for i := 1; i <= 254; i++ {
		host := prefix + "." + strconv.Itoa(i)
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			if chain := r.probe(ctx, host); chain != nil {
				mu.Lock()
				found[host] = chain
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return found
}

var errEdited = errors.New("the address was changed meanwhile")

// move saves the printer's new address, which reconnects it.
func (r *Relocator) move(p config.Printer, host string) {
	err := r.cfg.Update(func(c *config.Config) error {
		target, findErr := c.Printer(p.ID)
		if findErr != nil {
			return findErr
		}
		if target.Host != p.Host {
			return errEdited
		}
		target.Host = host
		return nil
	})
	if err != nil {
		slog.Warn("printer address not updated", "printer", p.ID, "err", err)
		return
	}
	slog.Info("printer found at a new address", "printer", p.ID, "from", p.Host, "to", host)
	delete(r.offline, p.ID)
	delete(r.misses, p.ID)
}

// subnet returns the first three octets of an IPv4 address.
func subnet(host string) (string, bool) {
	ip := net.ParseIP(host).To4()
	if ip == nil {
		return "", false
	}
	return strconv.Itoa(int(ip[0])) + "." + strconv.Itoa(int(ip[1])) + "." + strconv.Itoa(int(ip[2])), true
}

// certificates returns the certificate chain presented on the MQTT port of
// host, nil when nothing answers there with TLS. The chain is checked
// against the learned CAs, not here.
func certificates(ctx context.Context, host string) []*x509.Certificate {
	dctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	d := tls.Dialer{Config: &tls.Config{InsecureSkipVerify: true}}
	conn, err := d.DialContext(dctx, "tcp", net.JoinHostPort(host, "8883"))
	if err != nil {
		return nil
	}
	defer conn.Close()
	return conn.(*tls.Conn).ConnectionState().PeerCertificates
}
