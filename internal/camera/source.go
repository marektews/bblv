// Package camera streams JPEG frames from printer cameras and shares one
// connection between all viewers.
package camera

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os/exec"
	"strings"
	"time"
)

const maxFrame = 10 << 20

// Source delivers JPEG frames until ctx is cancelled or the stream fails.
type Source func(ctx context.Context, frame func([]byte)) error

// Causes of camera failures that the UI explains; see Code.
var (
	ErrLiveViewDisabled = errors.New("LAN live view is disabled on the printer")
	ErrNoFFmpeg         = errors.New("ffmpeg is not installed on the server")
	ErrRefused          = errors.New("camera refused the stream")
	ErrRTSPUnreachable  = errors.New("RTSP port is not reachable")
)

// Code names the cause of a camera error for the UI, "" for nil.
func Code(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrLiveViewDisabled):
		return "liveview-disabled"
	case errors.Is(err, ErrNoFFmpeg):
		return "ffmpeg-missing"
	case errors.Is(err, ErrRefused):
		return "refused"
	case errors.Is(err, ErrRTSPUnreachable):
		return "rtsp-unreachable"
	}
	return "stream"
}

// Port6000 streams the camera of A1 and P1 printers: TLS on port 6000, an
// 80 byte login packet, then frames of a 16 byte header and a JPEG image.
// tlsConfig checks the printer's certificate, which is the one of MQTT.
func Port6000(host, accessCode string, tlsConfig *tls.Config) Source {
	return func(ctx context.Context, frame func([]byte)) error {
		return stream6000(ctx, net.JoinHostPort(host, "6000"), accessCode, tlsConfig, frame)
	}
}

func stream6000(ctx context.Context, addr, accessCode string, tlsConfig *tls.Config, frame func([]byte)) error {
	d := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 10 * time.Second},
		Config:    tlsConfig,
	}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()

	login := make([]byte, 80)
	binary.LittleEndian.PutUint32(login[0:], 0x40)
	binary.LittleEndian.PutUint32(login[4:], 0x3000)
	copy(login[16:48], "bblp")
	copy(login[48:80], accessCode)
	if _, err = conn.Write(login); err != nil {
		return err
	}
	head := make([]byte, 16)
	for {
		conn.SetReadDeadline(time.Now().Add(15 * time.Second))
		if _, err = io.ReadFull(conn, head); err != nil {
			return err
		}
		size := binary.LittleEndian.Uint32(head)
		if size == 0 || size > maxFrame {
			return fmt.Errorf("invalid frame size %d", size)
		}
		img := make([]byte, size)
		if _, err = io.ReadFull(conn, img); err != nil {
			return err
		}
		if !bytes.HasPrefix(img, soi) {
			// Printers without this stream answer the login with a status
			// packet such as ff ff ff ff 00 00 00 00 and close the connection.
			if size == 8 {
				return fmt.Errorf("%w (status %d)", ErrRefused, int32(binary.LittleEndian.Uint32(img)))
			}
			return errors.New("frame is not a JPEG image")
		}
		frame(img)
	}
}

// FFmpeg streams the RTSP camera of X1, X2, H2 and P2 printers through
// ffmpeg, which converts it to JPEG frames at about two per second. reported
// returns ipcam.rtsp_url from the printer's last report; ffmpeg is "" when it
// is not installed.
func FFmpeg(ffmpeg, host, accessCode string, reported func() string) Source {
	return func(ctx context.Context, frame func([]byte)) error {
		u, err := rtspURL(reported(), host)
		if err != nil {
			return err
		}
		if ffmpeg == "" {
			return ErrNoFFmpeg
		}
		u.User = url.UserPassword("bblp", accessCode)
		return streamRTSP(ctx, ffmpeg, u, frame)
	}
}

// rtspURL turns the reported ipcam.rtsp_url ("disable" while LAN live view is
// off) into the stream address at the configured host, which the server
// reaches the printer by; before the first report it assumes the usual one.
// Only rtsps is accepted: the URL carries the access code, which another
// scheme reported by a device at the printer's address could send in clear
// text or to an ffmpeg protocol other than RTSP.
func rtspURL(reported, host string) (*url.URL, error) {
	if reported == "disable" {
		return nil, ErrLiveViewDisabled
	}
	u := &url.URL{Scheme: "rtsps", Path: "/streaming/live/1"}
	port := ""
	if reported != "" {
		r, err := url.Parse(reported)
		if err != nil {
			return nil, fmt.Errorf("reported RTSP address: %w", err)
		}
		if r.Scheme != "rtsps" {
			return nil, fmt.Errorf("reported RTSP address uses %q, not rtsps", r.Scheme)
		}
		u.Path, u.RawQuery, port = r.Path, r.RawQuery, r.Port()
	}
	if port == "" {
		port = "322"
	}
	u.Host = net.JoinHostPort(host, port)
	return u, nil
}

func streamRTSP(ctx context.Context, ffmpeg string, u *url.URL, frame func([]byte)) error {
	// The printer opens the RTSP port only with LAN live view enabled;
	// checking it first gives a clearer error than ffmpeg would.
	d := net.Dialer{Timeout: 10 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRTSPUnreachable, err)
	}
	conn.Close()

	cmd := exec.CommandContext(ctx, ffmpeg, "-loglevel", "error", "-rtsp_transport", "tcp", "-i", u.String(),
		"-an", "-r", "2", "-f", "image2pipe", "-c:v", "mjpeg", "-q:v", "5", "pipe:1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err = cmd.Start(); err != nil {
		return err
	}
	err = splitJPEG(out, frame)
	if werr := cmd.Wait(); werr != nil && ctx.Err() == nil {
		// ffmpeg repeats the URL in its messages, which end up in the log and
		// the browser; the access code in it must not.
		msg := string(bytes.TrimSpace(stderr.Bytes()))
		if pw, ok := u.User.Password(); ok && pw != "" {
			msg = strings.ReplaceAll(msg, pw, "***")
		}
		return fmt.Errorf("ffmpeg: %w: %s", werr, msg)
	}
	return err
}

var (
	soi = []byte{0xFF, 0xD8} // start of image
	eoi = []byte{0xFF, 0xD9} // end of image
)

// splitJPEG calls frame for every JPEG image in a stream of concatenated
// images. Markers cannot occur inside the compressed data, where 0xFF bytes
// are always followed by 0x00.
func splitJPEG(r io.Reader, frame func([]byte)) error {
	br := bufio.NewReaderSize(r, 256<<10)
	chunk := make([]byte, 64<<10)
	var buf []byte
	for {
		n, err := br.Read(chunk)
		buf = append(buf, chunk[:n]...)
		for {
			start := bytes.Index(buf, soi)
			if start < 0 {
				// Keep a trailing 0xFF that may begin the next marker.
				if len(buf) > 0 {
					buf = buf[len(buf)-1:]
				}
				break
			}
			end := bytes.Index(buf[start+2:], eoi)
			if end < 0 {
				buf = buf[start:]
				if len(buf) > maxFrame {
					buf = nil
				}
				break
			}
			end += start + 4
			frame(bytes.Clone(buf[start:end]))
			buf = buf[end:]
		}
		if err != nil {
			return err
		}
	}
}
