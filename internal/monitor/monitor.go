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
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/marektews/bblv/internal/cache"
	"github.com/marektews/bblv/internal/camera"
	"github.com/marektews/bblv/internal/config"
	"github.com/marektews/bblv/internal/ftps"
	"github.com/marektews/bblv/internal/gcode"
	"github.com/marektews/bblv/internal/notify"
	"github.com/marektews/bblv/internal/printer"
	"github.com/marektews/bblv/internal/printjob"
	"github.com/marektews/bblv/internal/recording"
	"github.com/marektews/bblv/internal/trust"
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

	Usage    *printjob.Usage `json:"usage,omitempty"`    // filament the slicer planned
	Settings *gcode.Settings `json:"settings,omitempty"` // slicer profiles

	path, thumbPath string
}

func ready(res *printjob.Result) Geometry {
	return Geometry{
		State: GeometryReady, Key: res.Key, Thumbnail: res.Thumbnail != "", Usage: res.Usage, Settings: res.Settings,
		path: res.Geometry, thumbPath: res.Thumbnail,
	}
}

// Info is the public view of a printer.
type Info struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Model        string `json:"model"`
	Experimental bool   `json:"experimental"`
	Online       bool   `json:"online"`
	Camera       bool   `json:"camera"`
	Timelapse    bool   `json:"timelapse"`              // its SD card can be read, for the timelapses
	CameraError  string `json:"cameraError,omitempty"`  // camera.Code of why the camera does not work
	CameraDetail string `json:"cameraDetail,omitempty"` // the error message
	// ConnectionError tells why an offline printer cannot be reached.
	ConnectionError string `json:"connectionError,omitempty"`
	// DismissedAlerts are the alerts of Status someone has hidden.
	DismissedAlerts []string       `json:"dismissedAlerts"`
	Status          printer.Status `json:"status"`
	Geometry        Geometry       `json:"geometry"`
}

// Event is sent to subscribers: Type "printer" carries the current info of
// a printer, Type "removed" only its ID and Type "notification" Notice,
// something that happened to a printer worth telling.
type Event struct {
	Type   string
	Info   Info
	Notice *notify.Event
}

// Monitor tracks a set of printers.
type Monitor struct {
	store     *cache.Cache
	ffmpeg    string
	filaments filamentNames
	trust     *trust.Store // checks the printers' certificates; nil accepts any
	onNotice  func(notify.Event)
	dismissed dismissedAlerts

	mu       sync.Mutex
	ctx      context.Context // set by Run; printers added later start at once
	wg       sync.WaitGroup
	printers []*Printer
	subs     map[chan Event]struct{}
}

// New creates an empty monitor storing geometry in store. ffmpeg is the path
// of the ffmpeg binary used for RTSP cameras, "" when not available.
func New(store *cache.Cache, ffmpeg string) *Monitor {
	m := &Monitor{store: store, ffmpeg: ffmpeg, subs: map[chan Event]struct{}{}}
	if store != nil {
		store.KeepInUse(m.geometryKeys)
	}
	return m
}

// TrustPrinters makes the connections to the printers check their
// certificates against cas. It has to be called before printers are added.
func (m *Monitor) TrustPrinters(cas *trust.Store) {
	m.trust = cas
}

// OnNotice sets a function called with every notification, besides the
// subscribers, e.g. to send it to ntfy. It has to be called before Run and
// must not block.
func (m *Monitor) OnNotice(f func(notify.Event)) {
	m.onNotice = f
}

// notice tells the subscribers and OnNotice what happened to a printer.
func (m *Monitor) notice(e notify.Event) {
	e.ID = time.Now().UnixNano()
	m.publish(Event{Type: "notification", Notice: &e})
	if m.onNotice != nil {
		m.onNotice(e)
	}
}

// geometryKeys returns the cache keys of the geometry the printers show,
// which the cache must not evict.
func (m *Monitor) geometryKeys() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	var keys []string
	for _, p := range m.printers {
		p.mu.Lock()
		if p.geom.State == GeometryReady {
			keys = append(keys, p.geom.Key)
		}
		p.mu.Unlock()
	}
	return keys
}

// KeepFilamentNames loads the names of filament profiles learned before
// from path, where the names learned from now on are saved too.
func (m *Monitor) KeepFilamentNames(path string) error {
	return m.filaments.load(path)
}

// KeepDismissedAlerts loads the alerts dismissed before from path, where
// they are saved from now on too.
func (m *Monitor) KeepDismissedAlerts(path string) error {
	return m.dismissed.load(path)
}

// DismissAlerts hides alerts of a printer, keys like "hms:0300_0100_0001_0007"
// or "error:0700_8011", until it stops reporting them; RestoreAlerts shows
// them again.
func (m *Monitor) DismissAlerts(id string, keys []string) error {
	p := m.find(id)
	if p == nil {
		return ErrUnknownPrinter
	}
	p.mu.Lock()
	current := alertKeys(p.status)
	p.mu.Unlock()
	if m.dismissed.dismiss(id, keys, current) {
		p.publish(p.info())
	}
	return nil
}

// RestoreAlerts shows dismissed alerts of a printer again.
func (m *Monitor) RestoreAlerts(id string, keys []string) error {
	p := m.find(id)
	if p == nil {
		return ErrUnknownPrinter
	}
	if m.dismissed.restore(id, keys) {
		p.publish(p.info())
	}
	return nil
}

// learnFilaments remembers the filament profiles of a print and, when they
// name more spools than before, updates every printer.
func (m *Monitor) learnFilaments(s *gcode.Settings) {
	if !m.filaments.learn(s) {
		return
	}
	for _, info := range m.Printers() {
		m.publish(Event{Type: "printer", Info: info})
	}
}

// SyncPrinters makes the printers reached over the network follow the
// configured ones, in their order of appearance: it adds new printers,
// applies changes and stops following printers no longer configured. A new
// name is only shown; other changes reconnect the printer, which keeps the
// geometry of the current print while the serial number stays and passes
// its camera on, so that viewers stay connected. A replay stays as it is.
// config.Store.Watch calls it after every change, in their order.
func (m *Monitor) SyncPrinters(cfgs []config.Printer) {
	var changed, retired, removed []*Printer
	m.mu.Lock()
	configured := map[string]bool{}
	for _, cfg := range cfgs {
		configured[cfg.ID] = true
		i := m.index(cfg.ID)
		if i < 0 {
			p := m.networkPrinter(cfg, nil)
			m.printers = append(m.printers, p)
			m.startLocked(p)
			changed = append(changed, p)
			continue
		}
		old := m.printers[i]
		switch p := m.applyLocked(old, cfg); p {
		case nil: // unchanged
		case old: // renamed
			changed = append(changed, old)
		default:
			m.printers[i] = p
			m.startLocked(p)
			changed = append(changed, p)
			retired = append(retired, old)
		}
	}
	kept := m.printers[:0]
	for _, p := range m.printers {
		if p.cfg.ID != "" && !configured[p.id] {
			removed = append(removed, p)
		} else {
			kept = append(kept, p)
		}
	}
	clear(m.printers[len(kept):])
	m.printers = kept
	m.mu.Unlock()

	for _, p := range retired {
		p.retire()
	}
	for _, p := range removed {
		p.close()
		m.dismissed.retain(p.id, nil)
		m.publish(Event{Type: "removed", Info: Info{ID: p.id}})
	}
	for _, p := range changed {
		p.publish(p.info())
	}
}

// applyLocked applies cfg to the printer old. It returns nil when nothing
// changed, old when only the name did, or the printer replacing old.
func (m *Monitor) applyLocked(old *Printer, cfg config.Printer) *Printer {
	if old.cfg == cfg {
		return nil
	}
	renamed := old.cfg
	renamed.Name = cfg.Name
	if renamed == cfg {
		// cfg.Name is only used with the monitor locked; the other
		// fields, read while the printer runs, stay as they are.
		old.cfg.Name = cfg.Name
		old.mu.Lock()
		old.name = displayName(cfg.ID, cfg.Name)
		old.mu.Unlock()
		return old
	}
	p := m.networkPrinter(cfg, old.camera)
	if strings.EqualFold(old.cfg.Serial, cfg.Serial) {
		old.mu.Lock()
		if old.geom.State == GeometryReady {
			p.geom, p.job = old.geom, old.job
		}
		// A print that finished while the printer moved is still told.
		p.status, p.reported = old.status, old.reported
		old.mu.Unlock()
	}
	return p
}

// networkPrinter creates a printer reached over the network. A printer
// replacing another takes over its camera hub, switched to the new source.
func (m *Monitor) networkPrinter(cfg config.Printer, hub *camera.Hub) *Printer {
	mqtt := &mqttSource{cfg: cfg, cas: m.trust}
	p := newPrinter(m, cfg.ID, cfg.Name, cfg.Model, mqtt)
	mqtt.failed = p.onConnectError
	p.fetcher = p.fetchFTPS
	p.cfg = cfg
	p.rtsp = rtspCamera(cfg.Model)
	src := camera.Port6000(cfg.Host, cfg.AccessCode, m.trust.TLS(cfg.Serial))
	if p.rtsp {
		src = camera.FFmpeg(m.ffmpeg, cfg.Host, cfg.AccessCode, p.reportedRTSP)
	}
	if hub != nil {
		hub.SetSource(src)
	} else {
		hub = camera.NewHub(cfg.ID, src)
		// The hub outlives the printers it passes between, so it updates
		// whichever printer has the ID now.
		hub.OnChange(func() {
			if q := m.find(cfg.ID); q != nil {
				q.publish(q.info())
			}
		})
	}
	p.camera = hub
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

// Run follows the printers until ctx is cancelled. It returns once they are
// disconnected, their downloads ended and their cameras stopped.
func (m *Monitor) Run(ctx context.Context) {
	m.mu.Lock()
	m.ctx = ctx
	for _, p := range m.printers {
		m.startLocked(p)
	}
	m.mu.Unlock()
	<-ctx.Done()
	m.mu.Lock()
	printers := slices.Clone(m.printers)
	m.mu.Unlock()
	// A camera runs on its own while anyone watches, an ffmpeg process for
	// some models, which would outlive the server.
	for _, p := range printers {
		if p.camera != nil {
			p.camera.Close()
		}
	}
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

// SetLight switches the chamber light of a printer on or off; it fails with
// ErrNotSupported for a printer without one or a replay.
func (m *Monitor) SetLight(id string, on bool) error {
	p := m.find(id)
	if p == nil {
		return ErrUnknownPrinter
	}
	return p.setLight(on)
}

// SDCard opens a session on the SD card of a printer, ending with ctx, and
// returns it with the printer's serial number; ErrNotSupported for a
// replay, which has no card.
func (m *Monitor) SDCard(ctx context.Context, id string) (*ftps.Conn, string, error) {
	p := m.find(id)
	if p == nil {
		return nil, "", ErrUnknownPrinter
	}
	if p.fetcher == nil {
		return nil, "", ErrNotSupported
	}
	conn, err := ftps.Dial(ctx, p.cfg.Host, p.cfg.AccessCode, m.trust.TLS(p.cfg.Serial).VerifyConnection)
	return conn, p.cfg.Serial, err
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

// subscriberBuffer is how many events a subscriber may fall behind.
const subscriberBuffer = 64

// Subscribe returns a channel receiving an event after every change
// and a function to unsubscribe. The channel of a subscriber that falls
// too far behind is closed, as it would otherwise miss events; it has to
// subscribe again and fetch the current state.
func (m *Monitor) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)
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
			delete(m.subs, ch)
			close(ch)
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
	if err = json.NewDecoder(bufio.NewReader(f)).Decode(&e); err != nil {
		return "", err
	}
	return e.Model, nil
}
