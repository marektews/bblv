// Package ftps reads files from a Bambu printer's SD card over implicit FTPS.
package ftps

import (
	"context"
	"crypto/tls"
	"io"
	"net"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jlaffaye/ftp"
)

const (
	dialTimeout = 15 * time.Second
	// idleTimeout ends a session that stops moving data, e.g. when the
	// printer drops off the network in the middle of a download.
	idleTimeout = time.Minute
)

// Conn is a logged-in FTPS session. It must not be used concurrently.
type Conn struct {
	c    *ftp.ServerConn
	stop func() bool // stops watching the context
}

// Dial connects to the printer on port 990 and logs in as bblp; verify,
// unless nil, checks the printer's certificate. When ctx
// ends, every connection of the session is closed, which interrupts a
// transfer in progress.
func Dial(ctx context.Context, host, accessCode string, verify func(tls.ConnectionState) error) (*Conn, error) {
	return dial(ctx, net.JoinHostPort(host, "990"), host, accessCode, verify, idleTimeout)
}

func dial(ctx context.Context, addr, host, accessCode string, verify func(tls.ConnectionState) error, idle time.Duration) (*Conn, error) {
	// Same private Bambu CA as MQTT; the session cache lets data connections
	// resume the control connection's TLS session, which the X2D and P2S
	// require ("522 SSL connection failed: session reuse required"). Sessions
	// are cached under ServerName, or under host:port when it is empty, and
	// every data connection uses another port.
	tc := &tls.Config{InsecureSkipVerify: true, VerifyConnection: verify, ServerName: host, ClientSessionCache: tls.NewLRUClientSessionCache(0)}
	s := &session{ctx: ctx, idle: idle}
	stop := context.AfterFunc(ctx, s.closeAll)
	// The dial function serves the control and the data connections. Like
	// the library's own, it leaves the TLS handshake to the first read or
	// write, which some FTP servers need on data connections.
	c, err := ftp.Dial(addr,
		ftp.DialWithTLS(tc),
		ftp.DialWithDialFunc(func(network, address string) (net.Conn, error) {
			raw, dialErr := s.dial(network, address)
			if dialErr != nil {
				return nil, dialErr
			}
			return tls.Client(raw, tc), nil
		}),
		ftp.DialWithDisabledEPSV(true))
	if err != nil {
		stop()
		s.closeAll()
		return nil, err
	}
	if err = c.Login("bblp", accessCode); err != nil {
		c.Quit()
		stop()
		return nil, err
	}
	return &Conn{c: c, stop: stop}, nil
}

// session tracks the open connections of a session to close them all when
// its context ends.
type session struct {
	ctx  context.Context
	idle time.Duration

	mu     sync.Mutex
	open   []net.Conn
	closed bool
}

func (s *session) dial(network, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: dialTimeout}
	c, err := d.DialContext(s.ctx, network, addr)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed { // the context ended right after the dial
		c.Close()
		return nil, context.Cause(s.ctx)
	}
	s.open = append(s.open, c)
	return &idleConn{Conn: c, s: s}, nil
}

func (s *session) forget(c net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.open = slices.DeleteFunc(s.open, func(o net.Conn) bool { return o == c })
}

func (s *session) closeAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	for _, c := range s.open {
		c.Close()
	}
	s.open = nil
}

// idleConn fails a read or write that makes no progress for the session's
// idle time. The time spent between reads, e.g. parsing, does not count.
type idleConn struct {
	net.Conn
	s *session
}

func (c *idleConn) Read(b []byte) (int, error) {
	if err := c.Conn.SetReadDeadline(time.Now().Add(c.s.idle)); err != nil {
		return 0, err
	}
	return c.Conn.Read(b)
}

func (c *idleConn) Write(b []byte) (int, error) {
	if err := c.Conn.SetWriteDeadline(time.Now().Add(c.s.idle)); err != nil {
		return 0, err
	}
	return c.Conn.Write(b)
}

func (c *idleConn) Close() error {
	c.s.forget(c.Conn)
	return c.Conn.Close()
}

// Path converts a printer path such as "/sdcard/cache/a.3mf" or "/cache/a.3mf"
// to the relative form the printer's FTP server accepts ("cache/a.3mf").
func Path(p string) string {
	p = strings.TrimPrefix(p, "/sdcard/")
	return strings.TrimLeft(p, "/")
}

// List lists a directory.
func (c *Conn) List(dir string) ([]*ftp.Entry, error) {
	return c.c.List(Path(dir))
}

// Open starts downloading a file. The reader must be closed before the next call.
func (c *Conn) Open(path string) (io.ReadCloser, error) {
	return c.c.Retr(Path(path))
}

// Close logs out.
func (c *Conn) Close() error {
	err := c.c.Quit()
	c.stop()
	return err
}
