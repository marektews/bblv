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
	"errors"
	"io"
	"math/big"
	"net"
	"net/url"
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
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
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
	err = stream6000(context.Background(), ln.Addr().String(), "12345678", &tls.Config{InsecureSkipVerify: true}, func(f []byte) { frames = append(frames, f) })
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
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		io.ReadFull(conn, make([]byte, 80))
		head := []byte{8, 0, 0, 0, 0x3f, 1, 3, 0, 0, 0, 0, 0, 0x0c, 0, 0, 0}
		conn.Write(append(head, 0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0))
	}()

	err = stream6000(context.Background(), ln.Addr().String(), "12345678", &tls.Config{InsecureSkipVerify: true}, func([]byte) { t.Error("unexpected frame") })
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
	u := &url.URL{Scheme: "rtsps", Host: addr, Path: "/streaming/live/1"}
	err = streamRTSP(context.Background(), "/nonexistent/ffmpeg", u, func([]byte) {})
	if !errors.Is(err, ErrRTSPUnreachable) || Code(err) != "rtsp-unreachable" {
		t.Fatalf("err = %v", err)
	}
}

func TestRTSPURL(t *testing.T) {
	for reported, want := range map[string]string{
		"":                                      "rtsps://10.0.0.5:322/streaming/live/1",
		"rtsps://192.168.4.44/streaming/live/1": "rtsps://10.0.0.5:322/streaming/live/1",
		"rtsps://192.168.4.44:8322/streaming/x?a": "rtsps://10.0.0.5:8322/streaming/x?a",
	} {
		u, err := rtspURL(reported, "10.0.0.5")
		if err != nil || u.String() != want {
			t.Errorf("rtspURL(%q) = %v, %v; want %s", reported, u, err, want)
		}
	}
	if _, err := rtspURL("disable", "10.0.0.5"); Code(err) != "liveview-disabled" {
		t.Errorf("disabled: err = %v", err)
	}
	// The URL carries the access code, so it never goes out in clear text
	// or to another ffmpeg protocol.
	for _, reported := range []string{"rtsp://192.168.4.44/streaming/live/1", "http://192.168.4.44/x", "file:///etc/passwd"} {
		if u, err := rtspURL(reported, "10.0.0.5"); err == nil {
			t.Errorf("rtspURL(%q) = %v, want an error", reported, u)
		}
	}
}

func TestFFmpegReportsWhyItCannotStart(t *testing.T) {
	for _, tc := range []struct {
		ffmpeg, reported, code string
	}{
		{"/usr/bin/ffmpeg", "disable", "liveview-disabled"},
		{"", "", "ffmpeg-missing"},
	} {
		src := FFmpeg(tc.ffmpeg, "10.0.0.5", "12345678", func() string { return tc.reported })
		if err := src(context.Background(), func([]byte) {}); Code(err) != tc.code {
			t.Errorf("ffmpeg=%q reported=%q: err = %v, want %s", tc.ffmpeg, tc.reported, err, tc.code)
		}
	}
}

func TestHubReportsErrorsUntilFramesArrive(t *testing.T) {
	var calls atomic.Int32
	h := NewHub("test", func(ctx context.Context, frame func([]byte)) error {
		if calls.Add(1) == 1 {
			return ErrRefused
		}
		frame([]byte("img"))
		<-ctx.Done()
		return ctx.Err()
	})
	h.retry = time.Millisecond
	var changes atomic.Int32
	h.OnChange(func() { changes.Add(1) })

	_, unsub := h.Subscribe()
	defer unsub()
	waitFor(t, func() bool { return changes.Load() == 2 }) // failed, then recovered
	if err := h.Err(); err != nil {
		t.Fatalf("err = %v after a frame", err)
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

// streamOf returns a source sending img once and then waiting to be stopped.
func streamOf(img string, running *atomic.Int32) Source {
	return func(ctx context.Context, frame func([]byte)) error {
		running.Add(1)
		defer running.Add(-1)
		frame([]byte(img))
		<-ctx.Done()
		return ctx.Err()
	}
}

func TestHubSetSourceKeepsViewers(t *testing.T) {
	var running atomic.Int32
	h := NewHub("test", streamOf("old", &running))
	frames, unsub := h.Subscribe()
	defer unsub()
	if got := string(<-frames); got != "old" {
		t.Fatalf("got %q", got)
	}
	h.SetSource(streamOf("new", &running))
	if got := string(<-frames); got != "new" {
		t.Fatalf("got %q after the source changed", got)
	}
	waitFor(t, func() bool { return running.Load() == 1 })
}

func TestHubCloseEndsViewers(t *testing.T) {
	var running atomic.Int32
	h := NewHub("test", streamOf("img", &running))
	frames, unsub := h.Subscribe()
	defer unsub()
	<-frames
	h.Close()
	if _, ok := <-frames; ok {
		t.Fatal("channel open after Close")
	}
	late, unsubLate := h.Subscribe()
	defer unsubLate()
	if _, ok := <-late; ok {
		t.Fatal("subscribed to a closed hub")
	}
	waitFor(t, func() bool { return running.Load() == 0 })
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
