package camera

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"io"
	"math/big"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func jpeg(body string) []byte {
	return append(append([]byte{0xFF, 0xD8}, body...), 0xFF, 0xD9)
}

func TestSplitJPEG(t *testing.T) {
	stream := append(append([]byte("junk"), jpeg("a\xff\x00b")...), jpeg("second")...)
	var got [][]byte
	// One byte at a time so markers are split across reads.
	err := splitJPEG(&oneByteReader{bytes.NewReader(stream)}, func(f []byte) { got = append(got, f) })
	if err != io.EOF {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 || !bytes.Equal(got[0], jpeg("a\xff\x00b")) || !bytes.Equal(got[1], jpeg("second")) {
		t.Fatalf("frames = %q", got)
	}
}

type oneByteReader struct{ r io.Reader }

func (o *oneByteReader) Read(p []byte) (int, error) { return o.r.Read(p[:1]) }

func selfSigned(t *testing.T) tls.Certificate {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func TestStream6000(t *testing.T) {
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{selfSigned(t)}})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	login := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 80)
		io.ReadFull(conn, buf)
		login <- buf
		for _, body := range []string{"one", "two"} {
			img := jpeg(body)
			head := make([]byte, 16)
			binary.LittleEndian.PutUint32(head, uint32(len(img)))
			conn.Write(append(head, img...))
		}
	}()

	var frames [][]byte
	err = stream6000(context.Background(), ln.Addr().String(), "12345678", func(f []byte) { frames = append(frames, f) })
	if err == nil {
		t.Fatal("expected an error when the server closes the stream")
	}
	if len(frames) != 2 || !bytes.Equal(frames[1], jpeg("two")) {
		t.Fatalf("frames = %q", frames)
	}
	l := <-login
	if binary.LittleEndian.Uint32(l) != 0x40 || binary.LittleEndian.Uint32(l[4:]) != 0x3000 ||
		string(bytes.TrimRight(l[16:48], "\x00")) != "bblp" || string(bytes.TrimRight(l[48:80], "\x00")) != "12345678" {
		t.Fatalf("login packet = %x", l)
	}
}

// The X2D and P2S answer the login with a status packet instead of frames.
func TestStream6000Refused(t *testing.T) {
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{selfSigned(t)}})
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		io.ReadFull(conn, make([]byte, 80))
		head := []byte{8, 0, 0, 0, 0x3f, 1, 3, 0, 0, 0, 0, 0, 0x0c, 0, 0, 0}
		conn.Write(append(head, 0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0))
	}()

	err = stream6000(context.Background(), ln.Addr().String(), "12345678", func([]byte) { t.Error("unexpected frame") })
	if err == nil || !strings.Contains(err.Error(), "refused the stream (status -1)") {
		t.Fatalf("err = %v", err)
	}
}

func TestStreamRTSPClosedPort(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	// ffmpeg must not be started when the port is closed.
	err = streamRTSP(context.Background(), "/nonexistent/ffmpeg", addr, "12345678", func([]byte) {})
	if err == nil || !strings.Contains(err.Error(), "LAN live view") {
		t.Fatalf("err = %v", err)
	}
}

func TestHubStartsOnDemandAndStopsWhenIdle(t *testing.T) {
	var running atomic.Int32
	frames := make(chan []byte)
	h := NewHub("test", func(ctx context.Context, frame func([]byte)) error {
		running.Add(1)
		defer running.Add(-1)
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case f := <-frames:
				frame(f)
			}
		}
	})
	h.grace = 20 * time.Millisecond

	a, unsubA := h.Subscribe()
	b, unsubB := h.Subscribe()
	frames <- []byte("1")
	frames <- []byte("2") // a and b keep only the newest frame
	waitFor(t, func() bool { return len(a) == 1 && string(peek(h)) == "2" })
	if got := string(<-a); got != "2" {
		t.Fatalf("a got %q", got)
	}
	if got := string(<-b); got != "2" {
		t.Fatalf("b got %q", got)
	}

	c, unsubC := h.Subscribe() // a late viewer gets the latest frame at once
	if got := string(<-c); got != "2" {
		t.Fatalf("c got %q", got)
	}
	unsubA()
	unsubB()
	unsubC()
	waitFor(t, func() bool { return running.Load() == 0 })
	if peek(h) != nil {
		t.Fatal("latest frame kept after stop")
	}
	if running.Load() != 0 {
		t.Fatal("source still running")
	}
	_, unsub := h.Subscribe()
	waitFor(t, func() bool { return running.Load() == 1 })
	unsub()
}

func peek(h *Hub) []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.latest
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(2 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal("condition not met")
}
