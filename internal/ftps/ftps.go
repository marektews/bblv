// Package ftps reads files from a Bambu printer's SD card over implicit FTPS.
package ftps

import (
	"context"
	"crypto/tls"
	"io"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

// Conn is a logged-in FTPS session. It must not be used concurrently.
type Conn struct {
	c *ftp.ServerConn
}

// Dial connects to the printer on port 990 and logs in as bblp.
func Dial(ctx context.Context, host, accessCode string) (*Conn, error) {
	// Same private Bambu CA as MQTT; the session cache lets data connections
	// resume the control connection's TLS session, which the X2D and P2S
	// require ("522 SSL connection failed: session reuse required"). Sessions
	// are cached under ServerName, or under host:port when it is empty, and
	// every data connection uses another port.
	tc := &tls.Config{InsecureSkipVerify: true, ServerName: host, ClientSessionCache: tls.NewLRUClientSessionCache(0)}
	c, err := ftp.Dial(host+":990",
		ftp.DialWithContext(ctx),
		ftp.DialWithTLS(tc),
		ftp.DialWithTimeout(15*time.Second),
		ftp.DialWithDisabledEPSV(true))
	if err != nil {
		return nil, err
	}
	if err = c.Login("bblp", accessCode); err != nil {
		c.Quit()
		return nil, err
	}
	return &Conn{c: c}, nil
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
	return c.c.Quit()
}
