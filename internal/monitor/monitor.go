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

	Usage *printjob.Usage `json:"usage,omitempty"` // filament the slicer planned

	path, thumbPath string
}

func ready(res *printjob.Result) Geometry {
	return Geometry{State: GeometryReady, Key: res.Key, Thumbnail: res.Thumbnail != "", Usage: res.Usage, path: res.Geometry, thumbPath: res.Thumbnail}
}

// Info is the public view of a printer.
type Info struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	Model        string         `json:"model"`
	Experimental bool           `json:"experimental"`
	Online       bool           `json:"online"`
	Camera       bool           `json:"camera"`
	CameraError  string         `json:"cameraError,omitempty"`  // camera.Code of why the camera does not work
	CameraDetail string         `json:"cameraDetail,omitempty"` // the error message
	Status       printer.Status `json:"status"`
	Geometry     Geometry       `json:"geometry"`
}

// Event is sent to subscribers: Type "printer" carries the current info of
// a printer, Type "removed" only its ID.
type Event struct {
	Type string
	Info Info
}

// Monitor tracks a set of printers.
type Monitor struct {
	store  *cache.Cache
	ffmpeg string

	mu       sync.Mutex
	ctx      context.Context // set by Run; printers added later start at once
	wg       sync.WaitGroup
	printers []*Printer
	subs     map[chan Event]struct{}
}

// New creates an empty monitor storing geometry in store. ffmpeg is the path
// of the ffmpeg binary used for RTSP cameras, "" when not available.
func New(store *cache.Cache, ffmpeg string) *Monitor {
	return &Monitor{store: store, ffmpeg: ffmpeg, subs: map[chan Event]struct{}{}}
}

// AddPrinter adds a printer reached over the local network.
func (m *Monitor) AddPrinter(cfg config.Printer) {
	m.add(m.networkPrinter(cfg))
}

// ReplacePrinter applies a changed configuration by reconnecting the printer
// with the same ID, keeping its position.
func (m *Monitor) ReplacePrinter(cfg config.Printer) error {
	p := m.networkPrinter(cfg)
	m.mu.Lock()
	i := m.index(cfg.ID)
	if i < 0 {
		m.mu.Unlock()
		return ErrUnknownPrinter
	}
	old := m.printers[i]
	m.printers[i] = p
	m.startLocked(p)
	m.mu.Unlock()
	old.close()
	m.publish(Event{Type: "printer", Info: p.info()})
	return nil
}

// RemovePrinter stops following a printer.
func (m *Monitor) RemovePrinter(id string) error {
	m.mu.Lock()
	i := m.index(id)
	if i < 0 {
		m.mu.Unlock()
		return ErrUnknownPrinter
	}
	old := m.printers[i]
	m.printers = append(m.printers[:i], m.printers[i+1:]...)
	m.mu.Unlock()
	old.close()
	m.publish(Event{Type: "removed", Info: Info{ID: id}})
	return nil
}

func (m *Monitor) networkPrinter(cfg config.Printer) *Printer {
	p := newPrinter(m, cfg.ID, cfg.Name, cfg.Model, &mqttSource{cfg: cfg})
	p.fetcher = p.fetchFTPS
	p.cfg = cfg
	p.rtsp = rtspCamera(cfg.Model)
	src := camera.Port6000(cfg.Host, cfg.AccessCode)
	if p.rtsp {
		src = camera.FFmpeg(m.ffmpeg, cfg.Host, cfg.AccessCode, p.reportedRTSP)
	}
	p.camera = camera.NewHub(cfg.ID, src)
	p.camera.OnChange(func() { p.publish(p.info()) })
	return p
}

// rtspCamera tells how to read the printer camera: X1, X2, H2 and P2 printers
// stream RTSP, which needs ffmpeg; the others use the JPEG stream on port
// 6000. The X2D, P2S and H2 series refuse logins on port 6000.
func rtspCamera(model string) bool {
	model = strings.ToUpper(model)
	return strings.HasPrefix(model, "X1") || strings.HasPrefix(model, "X2") ||
		strings.HasPrefix(model, "H2") || strings.HasPrefix(model, "P2")
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
	m.startLocked(p)
	m.mu.Unlock()
	m.publish(Event{Type: "printer", Info: p.info()})
}

// startLocked runs the printer when the monitor is running.
func (m *Monitor) startLocked(p *Printer) {
	if m.ctx == nil {
		return
	}
	ctx, cancel := context.WithCancel(m.ctx)
	p.stop = cancel
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		p.run(ctx)
	}()
}

func (m *Monitor) index(id string) int {
	for i, p := range m.printers {
		if p.id == id {
			return i
		}
	}
	return -1
}

// Run follows the printers until ctx is cancelled.
func (m *Monitor) Run(ctx context.Context) {
	m.mu.Lock()
	m.ctx = ctx
	for _, p := range m.printers {
		m.startLocked(p)
	}
	m.mu.Unlock()
	<-ctx.Done()
	m.wg.Wait()
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

// Serial returns the serial number of a printer, "" when unknown.
func (m *Monitor) Serial(id string) string {
	if p := m.find(id); p != nil {
		return p.cfg.Serial
	}
	return ""
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

// Subscribe returns a channel receiving an event after every change
// and a function to unsubscribe. Slow subscribers miss updates.
func (m *Monitor) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 64)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	m.mu.Unlock()
	return ch, func() {
		m.mu.Lock()
		delete(m.subs, ch)
		m.mu.Unlock()
	}
}

func (m *Monitor) publish(e Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for ch := range m.subs {
		select {
		case ch <- e:
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
