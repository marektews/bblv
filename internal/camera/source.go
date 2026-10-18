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
	"time"
)

const maxFrame = 10 << 20

// Source delivers JPEG frames until ctx is cancelled or the stream fails.
type Source func(ctx context.Context, frame func([]byte)) error

// Port6000 streams the camera of A1 and P1 printers: TLS on port 6000, an
// 80 byte login packet, then frames of a 16 byte header and a JPEG image.
func Port6000(host, accessCode string) Source {
	return func(ctx context.Context, frame func([]byte)) error {
		return stream6000(ctx, net.JoinHostPort(host, "6000"), accessCode, frame)
	}
}

func stream6000(ctx context.Context, addr, accessCode string, frame func([]byte)) error {
	d := tls.Dialer{
		NetDialer: &net.Dialer{Timeout: 10 * time.Second},
		// Same private Bambu CA as MQTT.
		Config: &tls.Config{InsecureSkipVerify: true},
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
	if _, err := conn.Write(login); err != nil {
		return err
	}
	head := make([]byte, 16)
	for {
		conn.SetReadDeadline(time.Now().Add(15 * time.Second))
		if _, err := io.ReadFull(conn, head); err != nil {
			return err
		}
		size := binary.LittleEndian.Uint32(head)
		if size == 0 || size > maxFrame {
			return fmt.Errorf("invalid frame size %d", size)
		}
		img := make([]byte, size)
		if _, err := io.ReadFull(conn, img); err != nil {
			return err
		}
		if !bytes.HasPrefix(img, soi) {
			return errors.New("frame is not a JPEG image")
		}
		frame(img)
	}
}

// FFmpeg streams the RTSP camera of X1 and H2 printers through ffmpeg,
// which converts it to JPEG frames at about two per second.
func FFmpeg(ffmpeg, host, accessCode string) Source {
	return func(ctx context.Context, frame func([]byte)) error {
		u := url.URL{Scheme: "rtsps", User: url.UserPassword("bblp", accessCode), Host: net.JoinHostPort(host, "322"), Path: "/streaming/live/1"}
		cmd := exec.CommandContext(ctx, ffmpeg, "-loglevel", "error", "-rtsp_transport", "tcp", "-i", u.String(),
			"-an", "-r", "2", "-f", "image2pipe", "-c:v", "mjpeg", "-q:v", "5", "pipe:1")
		out, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			return err
		}
		err = splitJPEG(out, frame)
		if werr := cmd.Wait(); werr != nil && ctx.Err() == nil {
			return fmt.Errorf("ffmpeg: %w: %s", werr, bytes.TrimSpace(stderr.Bytes()))
		}
		return err
	}
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
