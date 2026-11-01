package timelapse

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jlaffaye/ftp"

	"github.com/marektews/bblv/internal/cache"
)

func chunk(id string, data []byte) []byte {
	b := append([]byte(id), binary.LittleEndian.AppendUint32(nil, uint32(len(data)))...)
	b = append(b, data...)
	if len(data)%2 == 1 {
		b = append(b, 0) // padding
	}
	return b
}

// avi builds a Motion JPEG AVI like the printers write: no idx1 index.
func avi(frames ...[]byte) []byte {
	avih := make([]byte, 56)
	binary.LittleEndian.PutUint32(avih[0:], 41666) // 24 fps
	binary.LittleEndian.PutUint32(avih[32:], 1536)
	binary.LittleEndian.PutUint32(avih[36:], 1080)
	hdrl := append([]byte("hdrl"), chunk("avih", avih)...)
	movi := []byte("movi")
	for _, f := range frames {
		movi = append(movi, chunk("00db", f)...)
	}
	body := append([]byte("AVI "), chunk("LIST", hdrl)...)
	body = append(body, chunk("LIST", movi)...)
	return chunk("RIFF", body)
}

func jpeg(n int) []byte {
	return append([]byte{0xFF, 0xD8, byte(n)}, bytes.Repeat([]byte{0xAB}, n)...)
}

func TestReadIndex(t *testing.T) {
	frames := [][]byte{jpeg(3), jpeg(4), jpeg(7)} // odd sizes are padded
	b := avi(frames...)
	ix, err := ReadIndex(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	if ix.Count != 3 || ix.Width != 1536 || ix.Height != 1080 || ix.FPS < 23.9 || ix.FPS > 24.1 {
		t.Fatalf("index %+v", ix)
	}
	for i, f := range ix.Frames {
		if got := b[f.Off : f.Off+f.Len]; !bytes.Equal(got, frames[i]) {
			t.Fatalf("frame %d = %x", i, got)
		}
	}
	// A video still being recorded ends in the middle of a frame.
	cut := b[:len(b)-3]
	if ix, err = ReadIndex(bytes.NewReader(cut), int64(len(cut))); err != nil || ix.Count != 2 {
		t.Fatalf("cut: %v, %v", ix, err)
	}
	if _, err = ReadIndex(strings.NewReader("not a video"), 11); err == nil {
		t.Fatal("read a text as AVI")
	}
}

// fakeCard is an SD card in memory; it counts the sessions opened.
type fakeCard struct {
	mu       sync.Mutex
	files    map[string][]byte
	sessions atomic.Int32
	open     atomic.Int32 // sessions open at once
	maxOpen  atomic.Int32
	reads    map[string]int
	fail     string // a file that cannot be read
}

func (c *fakeCard) opener(ctx context.Context, printer string) (Card, string, error) {
	c.sessions.Add(1)
	if n := c.open.Add(1); n > c.maxOpen.Load() {
		c.maxOpen.Store(n)
	}
	time.Sleep(time.Millisecond) // lets overlapping sessions show
	return c, "SERIAL", nil
}

func (c *fakeCard) Close() error {
	c.open.Add(-1)
	return nil
}

func (c *fakeCard) List(dir string) ([]*ftp.Entry, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []*ftp.Entry
	for p, b := range c.files {
		if d, name := filepath.ToSlash(filepath.Dir(p)), filepath.Base(p); d == dir {
			out = append(out, &ftp.Entry{Name: name, Type: ftp.EntryTypeFile, Size: uint64(len(b))})
		}
	}
	return out, nil
}

func (c *fakeCard) Open(p string) (io.ReadCloser, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b, ok := c.files[p]
	if !ok || p == c.fail {
		return nil, errors.New("550 not found")
	}
	if c.reads == nil {
		c.reads = map[string]int{}
	}
	c.reads[p]++
	return io.NopCloser(bytes.NewReader(b)), nil
}

func TestLibrary(t *testing.T) {
	store, err := cache.Open(t.TempDir(), cache.DefaultLimit)
	if err != nil {
		t.Fatal(err)
	}
	card := &fakeCard{files: map[string][]byte{
		"timelapse/video_2026-01-14_02-19-54.avi":           avi(jpeg(10), jpeg(20)),
		"timelapse/video_2025-12-22_05-59-16.avi":           avi(jpeg(5)),
		"timelapse/notes.txt":                               []byte("x"),
		"timelapse/thumbnail/video_2026-01-14_02-19-54.jpg": jpeg(1),
		"timelapse/thumbnail/video_2025-08-09_19-41-11.jpg": jpeg(1), // of a video deleted
	}}
	lib := NewLibrary(card.opener, store)
	ctx := context.Background()

	videos, err := lib.List(ctx, "a1")
	if err != nil {
		t.Fatal(err)
	}
	if len(videos) != 2 || videos[0].Recorded != "2026-01-14T02:19:54" || !videos[0].Thumbnail || videos[1].Thumbnail {
		t.Fatalf("videos %+v", videos)
	}
	// The thumbnail came with the list: showing it asks the printer no more.
	sessions := card.sessions.Load()
	if _, err = lib.Thumbnail(ctx, "a1", videos[0].Name); err != nil || card.sessions.Load() != sessions {
		t.Fatalf("thumbnail: %v after %d sessions", err, card.sessions.Load()-sessions)
	}
	if _, err = lib.Thumbnail(ctx, "a1", "../config.json"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("path outside: %v", err)
	}

	// Frames are read only from the cache, after the video was prepared.
	if _, err = lib.Frame("a1", videos[0].Name, 0); !errors.Is(err, ErrNotReady) {
		t.Fatalf("frame before preparing: %v", err)
	}

	// Viewers asking at once get one download in the background, while
	// the list keeps a session of its own.
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() { prepared(t, lib, videos[0].Name) })
	}
	wg.Wait()
	if n := card.reads["timelapse/"+videos[0].Name]; n != 1 || card.maxOpen.Load() > 2 {
		t.Fatalf("downloaded %d times, %d sessions at once", n, card.maxOpen.Load())
	}
	sessions = card.sessions.Load()
	if b, frameErr := lib.Frame("a1", videos[0].Name, 1); frameErr != nil || !bytes.Equal(b, jpeg(20)) || card.sessions.Load() != sessions {
		t.Fatalf("frame: %x, %v", b, frameErr)
	}
	if _, err = lib.Frame("a1", videos[0].Name, 2); !errors.Is(err, ErrNotFound) {
		t.Fatalf("frame past the end: %v", err)
	}
	// A video being watched stays in the cache whatever else comes in.
	clip, _ := lib.Ready("a1", videos[0].Name)
	if watched := lib.watched(); len(watched) != 1 || watched[0] != clip.Key {
		t.Fatalf("watched %v, want %s", watched, clip.Key)
	}

	// A video that grew, still being recorded, is fetched again.
	card.mu.Lock()
	card.files["timelapse/"+videos[0].Name] = avi(jpeg(10), jpeg(20), jpeg(30))
	card.mu.Unlock()
	if c := prepared(t, lib, videos[0].Name); c.Index.Count != 3 || card.reads["timelapse/"+videos[0].Name] != 2 {
		t.Fatalf("grown video: %+v", c)
	}

	// A failed download is told once; asking again tries again.
	card.mu.Lock()
	card.fail = "timelapse/" + videos[1].Name
	card.mu.Unlock()
	var failed error
	for deadline := time.Now().Add(5 * time.Second); failed == nil && time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		_, _, failed = lib.Prepare(context.Background(), "a1", videos[1].Name)
	}
	if failed == nil {
		t.Fatal("failure not told")
	}
	card.mu.Lock()
	card.fail = ""
	card.mu.Unlock()
	if c := prepared(t, lib, videos[1].Name); c.Index.Count != 1 {
		t.Fatalf("after the failure: %+v", c)
	}
}

// prepared asks for a video until the download in the background has ended.
func prepared(t *testing.T, lib *Library, name string) *Clip {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(time.Millisecond) {
		c, p, err := lib.Prepare(context.Background(), "a1", name)
		if err != nil {
			t.Error(err)
			return nil
		}
		if c != nil {
			return c
		}
		if p == nil || p.Size == 0 {
			t.Error("neither the video nor its progress")
			return nil
		}
	}
	t.Error("not prepared in time")
	return nil
}
