// Package monitor follows the printers, keeps the geometry of their current
// prints up to date and notifies subscribers about every change.
package monitor

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/marektews/bblv/internal/cache"
	"github.com/marektews/bblv/internal/camera"
	"github.com/marektews/bblv/internal/config"
	"github.com/marektews/bblv/internal/printer"
	"github.com/marektews/bblv/internal/printjob"
	"github.com/marektews/bblv/internal/recording"
)

// Geometry states.
const (
	GeometryNone    = "none"    // no print known
	GeometryLoading = "loading" // being downloaded and parsed
	GeometryReady   = "ready"
	GeometryMissing = "missing" // print files not found on the SD card; upload one
	GeometryError   = "error"
)

// Geometry describes the 3D geometry of a printer's current print.
type Geometry struct {
	State      string `json:"state"`
	Key        string `json:"key,omitempty"` // changes whenever the geometry changes
	Subtask    string `json:"subtask,omitempty"`
	Plate      int    `json:"plate,omitempty"`
	AMSMapping []int  `json:"amsMapping,omitempty"`
	Uploaded   bool   `json:"uploaded,omitempty"`
	Thumbnail  bool   `json:"thumbnail"`
	Error      string `json:"error,omitempty"`

	path, thumbPath string
}

func ready(res *printjob.Result) Geometry {
	return Geometry{State: GeometryReady, Key: res.Key, Thumbnail: res.Thumbnail != "", path: res.Geometry, thumbPath: res.Thumbnail}
}

// Info is the public view of a printer.
type Info struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Model        string         `json:"model"`
	Experimental bool           `json:"experimental"`
	Online       bool           `json:"online"`
	Camera       bool           `json:"camera"`
	Status       printer.Status `json:"status"`
	Geometry     Geometry       `json:"geometry"`
}

// Monitor tracks a set of printers.
type Monitor struct {
	store  *cache.Cache
	ffmpeg string

	mu       sync.Mutex
	printers []*Printer
	subs     map[chan Info]struct{}
}

// New creates an empty monitor storing geometry in store. ffmpeg is the path
// of the ffmpeg binary used for RTSP cameras, "" when not available.
func New(store *cache.Cache, ffmpeg string) *Monitor {
	return &Monitor{store: store, ffmpeg: ffmpeg, subs: map[chan Info]struct{}{}}
}

// AddPrinter adds a printer reached over the local network.
func (m *Monitor) AddPrinter(cfg config.Printer) {
	p := newPrinter(m, cfg.ID, cfg.Name, cfg.Model, &mqttSource{cfg: cfg})
	p.fetcher = p.fetchFTPS
	p.cfg = cfg
	if src := m.cameraSource(cfg); src != nil {
		p.camera = camera.NewHub(cfg.ID, src)
	}
	m.add(p)
}

// cameraSource picks how to read the printer camera: X1 and H2 printers
// stream RTSP, which needs ffmpeg; the others use the JPEG stream on port 6000.
func (m *Monitor) cameraSource(cfg config.Printer) camera.Source {
	model := strings.ToUpper(cfg.Model)
	if strings.HasPrefix(model, "X1") || strings.HasPrefix(model, "H2") {
		if m.ffmpeg == "" {
			return nil
		}
		return camera.FFmpeg(m.ffmpeg, cfg.Host, cfg.AccessCode)
	}
	return camera.Port6000(cfg.Host, cfg.AccessCode)
}

// AddReplay adds a virtual printer with the id "replay" that plays back a
// recording. Its geometry can only be uploaded.
func (m *Monitor) AddReplay(path string, speed float64) error {
	model, err := recordedModel(path)
	if err != nil {
		return err
	}
	name := "Replay: " + strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	m.add(newPrinter(m, "replay", name, model, &replaySource{path: path, speed: speed}))
	return nil
}

func (m *Monitor) add(p *Printer) {
	m.mu.Lock()
	m.printers = append(m.printers, p)
	m.mu.Unlock()
}

// Run follows all printers until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) {
	var wg sync.WaitGroup
	m.mu.Lock()
	for _, p := range m.printers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.run(ctx)
		}()
	}
	m.mu.Unlock()
	wg.Wait()
}

// Printers returns all printers in configuration order.
func (m *Monitor) Printers() []Info {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Info, len(m.printers))
	for i, p := range m.printers {
		out[i] = p.info()
	}
	return out
}

// Printer returns one printer.
func (m *Monitor) Printer(id string) (Info, bool) {
	p := m.find(id)
	if p == nil {
		return Info{}, false
	}
	return p.info(), true
}

// Files returns the geometry and thumbnail paths of the printer's current
// print together with the geometry key; empty paths mean not available.
func (m *Monitor) Files(id string) (geometry, thumbnail, key string) {
	if p := m.find(id); p != nil {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.geom.State == GeometryReady {
			return p.geom.path, p.geom.thumbPath, p.geom.Key
		}
	}
	return "", "", ""
}

// ErrUnknownPrinter is returned for an id that is not configured.
var ErrUnknownPrinter = errors.New("unknown printer")

// Upload makes a parsed local file the geometry of the printer's current
// print, until the printer starts another one.
func (m *Monitor) Upload(id string, res *printjob.Result) error {
	p := m.find(id)
	if p == nil {
		return ErrUnknownPrinter
	}
	p.upload(res)
	return nil
}

// Camera returns the camera of a printer, nil when it has none.
func (m *Monitor) Camera(id string) *camera.Hub {
	if p := m.find(id); p != nil {
		return p.camera
	}
	return nil
}

// Store returns the geometry cache.
func (m *Monitor) Store() *cache.Cache {
	return m.store
}

// Subscribe returns a channel receiving the printer info after every change
// and a function to unsubscribe. Slow subscribers miss updates.
func (m *Monitor) Subscribe() (<-chan Info, func()) {
	ch := make(chan Info, 64)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		delete(m.subs, ch)
		m.mu.Unlock()
	}
}

func (m *Monitor) publish(info Info) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ch := range m.subs {
		select {
		case ch <- info:
		default:
		}
	}
}

func (m *Monitor) find(id string) *Printer {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.printers {
		if p.id == id {
			return p
		}
	}
	return nil
}

func recordedModel(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	var e recording.Entry
	if err := json.NewDecoder(bufio.NewReader(f)).Decode(&e); err != nil {
		return "", err
	}
	return e.Model, nil
}
