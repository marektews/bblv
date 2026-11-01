package timelapse

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marektews/bblv/internal/cache"
)

// Card is a session on a printer's SD card.
type Card interface {
	Lister
	Open(name string) (io.ReadCloser, error)
	Close() error
}

// Opener opens a session on the SD card of a printer and returns it with
// the printer's serial number, which tells its files apart in the cache.
// The session ends with ctx.
type Opener func(ctx context.Context, printer string) (Card, string, error)

var (
	// ErrNotFound is returned for a video that is not on the card.
	ErrNotFound = errors.New("no such timelapse on the SD card")
	// ErrNotReady is returned for a video not brought into the cache yet.
	ErrNotReady = errors.New("the timelapse has not been fetched from the printer yet")
)

const (
	// downloadTimeout bounds a download: the A1 sends some 120 KB/s
	// while printing, minutes for a long timelapse.
	downloadTimeout = 30 * time.Minute
	listTimeout     = time.Minute
	maxThumbnail    = 4 << 20
	// listedThumbnails is how many of the newest videos get their
	// thumbnails with the list.
	listedThumbnails = 50
	// keptClips is how many videos keep their frame index in memory.
	keptClips = 8
	// watchedFor is how long after its last frame was read a video stays
	// in the cache whatever else comes in.
	watchedFor = 15 * time.Minute
)

// Clip is a video in the cache.
type Clip struct {
	Key   string // in the cache, from the size it had on the card
	Path  string
	Index *Index       // nil for a video that is no AVI
	used  atomic.Int64 // unix time in nanoseconds it was last read
}

// Progress tells how far a download has come.
type Progress struct {
	Done   atomic.Int64 // bytes
	Size   int64
	Queued atomic.Bool // waiting for another download from the printer to end
}

// Library lists the timelapses of the printers and brings the ones watched
// into the cache in the background, each once however many viewers ask.
// A printer gets one session for lists and one for downloads at a time, so
// that a long download does not hold up the list.
type Library struct {
	open  Opener
	store *cache.Cache

	mu        sync.Mutex
	slots     map[string]chan struct{} // a session's turn, by printer and use
	clips     map[string]*Clip         // by printer and name
	order     []string                 // keys of clips, least recently used first
	fetching  map[string]*Progress     // downloads in progress, by printer and name
	lastError map[string]error         // of the last download, until told, by printer and name
}

// NewLibrary keeps the videos in store.
func NewLibrary(open Opener, store *cache.Cache) *Library {
	l := &Library{
		open: open, store: store,
		slots: map[string]chan struct{}{}, clips: map[string]*Clip{},
		fetching: map[string]*Progress{}, lastError: map[string]error{},
	}
	store.KeepInUse(l.watched)
	return l
}

// watched returns the cache keys of the videos read lately, which a player
// may still be showing.
func (l *Library) watched() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var keys []string
	since := time.Now().Add(-watchedFor).UnixNano()
	for _, c := range l.clips {
		if c.used.Load() >= since {
			keys = append(keys, c.Key)
		}
	}
	return keys
}

// session waits for the printer's turn for use, which ctx may give up, and
// opens a session living until done or timeout, not ctx.
func (l *Library) session(ctx context.Context, printer, use string, timeout time.Duration) (card Card, serial string, done func(), err error) {
	l.mu.Lock()
	slot := l.slots[printer+"\x00"+use]
	if slot == nil {
		slot = make(chan struct{}, 1)
		l.slots[printer+"\x00"+use] = slot
	}
	l.mu.Unlock()
	select {
	case slot <- struct{}{}:
	case <-ctx.Done():
		return nil, "", nil, ctx.Err()
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	card, serial, err = l.open(sctx, printer)
	if err != nil {
		cancel()
		<-slot
		return nil, "", nil, err
	}
	return card, serial, func() {
		card.Close()
		cancel()
		<-slot
	}, nil
}

// List returns the timelapses of a printer, the newest first, and brings
// their thumbnails into the cache on the way, so that showing them needs
// no further sessions.
func (l *Library) List(ctx context.Context, printer string) ([]Video, error) {
	card, _, done, err := l.session(ctx, printer, "list", listTimeout)
	if err != nil {
		return nil, err
	}
	defer done()
	videos, err := List(card)
	if err != nil {
		return nil, err
	}
	for _, v := range videos[:min(len(videos), listedThumbnails)] {
		if _, ok := l.store.Get(thumbnailKey(printer, v.Name), ".jpg"); v.Thumbnail && !ok {
			l.fetchThumbnail(card, printer, v.Name) // shown as missing when it fails
		}
	}
	return videos, nil
}

// thumbnailKey identifies a thumbnail in the cache. Names hold the time of
// the recording, so the printer's ID and the name suffice, and a cached
// thumbnail is shown without asking the printer.
func thumbnailKey(printer, name string) string {
	return cache.Key("timelapse-thumbnail", printer, name)
}

// Thumbnail returns the path of a video's thumbnail in the cache.
func (l *Library) Thumbnail(ctx context.Context, printer, name string) (string, error) {
	if !ValidName(name) {
		return "", ErrNotFound
	}
	if p, ok := l.store.Get(thumbnailKey(printer, name), ".jpg"); ok {
		return p, nil
	}
	card, _, done, err := l.session(ctx, printer, "list", listTimeout)
	if err != nil {
		return "", err
	}
	defer done()
	return l.fetchThumbnail(card, printer, name)
}

func (l *Library) fetchThumbnail(card Card, printer, name string) (string, error) {
	rc, err := card.Open(Dir + "/thumbnail/" + ThumbnailName(name))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrNotFound, err)
	}
	defer rc.Close()
	return l.store.Put(thumbnailKey(printer, name), ".jpg", func(w io.Writer) error {
		n, copyErr := io.Copy(w, io.LimitReader(rc, maxThumbnail+1))
		if copyErr == nil && n > maxThumbnail {
			copyErr = errors.New("thumbnail too large")
		}
		return copyErr
	})
}

// Prepare returns a video in the cache with the size it has on the card
// now, its frames indexed. Otherwise it starts bringing it there in the
// background, unless that is under way, and returns how far it has come;
// a failed download is reported once. A video still being recorded grows,
// so it is fetched again when watched again.
func (l *Library) Prepare(ctx context.Context, printer, name string) (*Clip, *Progress, error) {
	if !ValidName(name) {
		return nil, nil, ErrNotFound
	}
	id := printer + "/" + name
	l.mu.Lock()
	p, err := l.fetching[id], l.lastError[id]
	delete(l.lastError, id)
	l.mu.Unlock()
	if p != nil {
		return nil, p, nil
	}
	if err != nil {
		return nil, nil, err
	}
	card, serial, done, err := l.session(ctx, printer, "list", listTimeout)
	if err != nil {
		return nil, nil, err
	}
	videos, err := List(card)
	done()
	if err != nil {
		return nil, nil, err
	}
	i := slices.IndexFunc(videos, func(v Video) bool { return v.Name == name })
	if i < 0 {
		return nil, nil, ErrNotFound
	}
	key := cache.Key("timelapse", serial, name, strconv.FormatInt(videos[i].Size, 10))
	if c, cerr := l.cached(id, key, path.Ext(name)); c != nil || cerr != nil {
		return c, nil, cerr
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if p = l.fetching[id]; p == nil {
		p = &Progress{Size: videos[i].Size}
		p.Queued.Store(true)
		l.fetching[id] = p
		go l.download(ctx, printer, name, key, p)
	}
	return nil, p, nil
}

// cached returns the clip of key from the cache, nil when it is not there.
func (l *Library) cached(id, key, ext string) (*Clip, error) {
	l.mu.Lock()
	c := l.clips[id]
	l.mu.Unlock()
	if c != nil && c.Key == key {
		if _, err := os.Stat(c.Path); err == nil {
			l.keep(id, c)
			return c, nil
		}
	}
	p, ok := l.store.Get(key, ext)
	if !ok {
		return nil, nil
	}
	c = &Clip{Key: key, Path: p}
	if ext == ".avi" {
		var err error
		if c.Index, err = indexFile(p); err != nil {
			return nil, err
		}
	}
	l.keep(id, c)
	return c, nil
}

// download brings a video into the cache, the next Prepare finds it there.
func (l *Library) download(ctx context.Context, printer, name, key string, p *Progress) {
	id := printer + "/" + name
	err := func() error {
		card, _, done, err := l.session(context.WithoutCancel(ctx), printer, "download", downloadTimeout)
		p.Queued.Store(false)
		if err != nil {
			return err
		}
		defer done()
		rc, err := card.Open(Dir + "/" + name)
		if err != nil {
			return err
		}
		defer rc.Close()
		_, err = l.store.Put(key, path.Ext(name), func(w io.Writer) error {
			_, copyErr := io.Copy(w, &counter{r: rc, n: &p.Done})
			return copyErr
		})
		return err
	}()
	if err != nil {
		slog.Warn("timelapse not fetched", "printer", printer, "video", name, "err", err)
	}
	l.mu.Lock()
	delete(l.fetching, id)
	if err != nil {
		l.lastError[id] = err
	}
	l.mu.Unlock()
}

// counter counts the bytes read.
type counter struct {
	r io.Reader
	n *atomic.Int64
}

func (c *counter) Read(b []byte) (int, error) {
	n, err := c.r.Read(b)
	c.n.Add(int64(n))
	return n, err
}

func indexFile(p string) (*Index, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	return ReadIndex(f, info.Size())
}

func (l *Library) keep(id string, c *Clip) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.order = slices.DeleteFunc(l.order, func(k string) bool { return k == id })
	l.order = append(l.order, id)
	l.clips[id] = c
	for len(l.order) > keptClips {
		delete(l.clips, l.order[0])
		l.order = l.order[1:]
	}
}

// Ready returns a video prepared before, reading only the cache: a player
// asks for many frames a second, which must not each open a session on the
// printer. ErrNotReady when it has not been prepared or was evicted.
func (l *Library) Ready(printer, name string) (*Clip, error) {
	l.mu.Lock()
	c := l.clips[printer+"/"+name]
	l.mu.Unlock()
	if c == nil {
		return nil, ErrNotReady
	}
	if _, err := os.Stat(c.Path); err != nil {
		return nil, ErrNotReady
	}
	c.used.Store(time.Now().UnixNano())
	return c, nil
}

// Frame returns frame n of an AVI video prepared before.
func (l *Library) Frame(printer, name string, n int) ([]byte, error) {
	c, err := l.Ready(printer, name)
	if err != nil {
		return nil, err
	}
	if c.Index == nil || n < 0 || n >= c.Index.Count {
		return nil, ErrNotFound
	}
	f, err := os.Open(c.Path)
	if err != nil {
		return nil, ErrNotReady
	}
	defer f.Close()
	fr := c.Index.Frames[n]
	b := make([]byte, fr.Len)
	if _, err = f.ReadAt(b, fr.Off); err != nil {
		return nil, err
	}
	return b, nil
}
