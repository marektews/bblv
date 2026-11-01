package monitor

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/marektews/bblv/internal/camera"
	"github.com/marektews/bblv/internal/config"
	"github.com/marektews/bblv/internal/ftps"
	"github.com/marektews/bblv/internal/notify"
	"github.com/marektews/bblv/internal/printer"
	"github.com/marektews/bblv/internal/printjob"
	"github.com/marektews/bblv/internal/recording"
	"github.com/marektews/bblv/internal/trust"
)

const (
	retryInterval = 30 * time.Second
	maxAttempts   = 10
	// unreachableReminder: every so many failed connection attempts, an
	// hour at retryInterval, the warning about it is repeated.
	unreachableReminder = 120
)

// testedModels are verified on real hardware; the others are experimental.
// The camera of the X2D, P2S, H2D and H2S is not verified yet.
var testedModels = []string{"A1", "X2D", "P2S", "H2D", "H2S"}

// Printer is the runtime state of one monitored printer.
type Printer struct {
	m               *Monitor
	id, name, model string
	cfg             config.Printer
	src             source
	fetcher         func(context.Context, job) Geometry // nil when geometry can only be uploaded
	camera          *camera.Hub                         // nil when the camera is not available
	rtsp            bool                                // the camera streams RTSP through ffmpeg
	stop            context.CancelFunc                  // set while running
	removed         atomic.Bool

	mu          sync.Mutex
	online      bool
	connErr     string // why the last connection attempt failed
	status      printer.Status
	reported    bool // status comes from a report, which the next one is compared with
	geom        Geometry
	job         string // print the geometry belongs to
	fetching    bool
	attempts    int
	lastAttempt time.Time
}

type job struct {
	id, subtask, gcodeFile string
}

func newPrinter(m *Monitor, id, name, model string, src source) *Printer {
	// Until the first report the status is that of an empty one, which has
	// empty lists where the zero Status would give null in the JSON.
	return &Printer{m: m, id: id, name: displayName(id, name), model: model, src: src, status: printer.NewState().Status(), geom: Geometry{State: GeometryNone}}
}

// displayName is the name shown for a printer: the configured one, or its ID.
func displayName(id, name string) string {
	if name == "" {
		return id
	}
	return name
}

// run follows the printer until ctx ends, which also interrupts a download
// of print files in progress.
func (p *Printer) run(ctx context.Context) {
	p.src.run(ctx, func(s printer.Status) { p.onStatus(ctx, s) }, p.onConnection)
}

// retire stops the printer after it was replaced; its camera passes on to
// the printer replacing it.
func (p *Printer) retire() {
	p.removed.Store(true)
	if p.stop != nil {
		p.stop()
	}
}

// close stops the printer after it was removed.
func (p *Printer) close() {
	p.retire()
	if p.camera != nil {
		p.camera.Close()
	}
}

// publish sends an update unless the printer is no longer monitored, which
// may happen while its last report is still being handled.
func (p *Printer) publish(info Info) {
	if !p.removed.Load() {
		p.m.publish(Event{Type: "printer", Info: info})
	}
}

func (p *Printer) info() Info {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.infoLocked()
}

func (p *Printer) infoLocked() Info {
	experimental := true
	for _, m := range testedModels {
		if strings.EqualFold(m, p.model) {
			experimental = false
		}
	}
	info := Info{
		ID: p.id, Name: p.name, Model: p.model, Experimental: experimental,
		Online: p.online, Camera: p.camera != nil, Timelapse: p.fetcher != nil, Status: p.status, Geometry: p.geom,
	}
	if !p.online {
		info.ConnectionError = p.connErr
	}
	info.DismissedAlerts = p.m.dismissed.list(p.id)
	if err := p.cameraErrLocked(); err != nil {
		info.CameraError, info.CameraDetail = camera.Code(err), err.Error()
	}
	// Printers without .bbl files on the SD card report the mapping instead.
	if len(info.Geometry.AMSMapping) == 0 {
		info.Geometry.AMSMapping = p.status.AMSMapping
	}
	info.Status.AMS, info.Status.External = p.m.filaments.nameTrays(p.status.AMS, p.status.External)
	return info
}

// cameraErrLocked tells why the camera does not work. What the printer
// reports and a missing ffmpeg are known before anyone opens the stream.
func (p *Printer) cameraErrLocked() error {
	switch {
	case p.camera == nil:
		return nil
	case p.rtsp && p.status.LANLiveView == "disabled":
		return camera.ErrLiveViewDisabled
	case p.rtsp && p.m.ffmpeg == "":
		return camera.ErrNoFFmpeg
	}
	return p.camera.Err()
}

// reportedRTSP returns ipcam.rtsp_url from the last report.
func (p *Printer) reportedRTSP() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.status.RTSPURL
}

// onConnectError keeps why connecting to the printer failed, "" once it
// connected, which the view of an offline printer shows.
func (p *Printer) onConnectError(err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	p.mu.Lock()
	changed := p.connErr != msg
	p.connErr = msg
	info := p.infoLocked()
	p.mu.Unlock()
	if changed {
		p.publish(info)
	}
}

func (p *Printer) onConnection(ok bool) {
	p.mu.Lock()
	changed := p.online != ok
	p.online = ok
	info := p.infoLocked()
	p.mu.Unlock()
	if changed {
		p.publish(info)
	}
}

func (p *Printer) onStatus(ctx context.Context, s printer.Status) {
	p.mu.Lock()
	if p.online && reflect.DeepEqual(p.status, s) {
		p.mu.Unlock()
		return
	}
	// The first report after the server started tells nothing new; one
	// after the connection came back tells, e.g., that the print finished
	// meanwhile.
	var notice *notify.Event
	if p.reported {
		notice = notify.Detect(p.status, s)
	}
	p.online = true
	p.status, p.reported = s, true
	// An alert dismissed and gone shows again when it comes back.
	p.m.dismissed.retain(p.id, alertKeys(s))
	j := p.nextJob(s)
	info := p.infoLocked()
	p.mu.Unlock()
	p.publish(info)
	if notice != nil && !p.removed.Load() {
		notice.Printer, notice.Name, notice.Serial = info.ID, info.Name, p.cfg.Serial
		p.m.notice(*notice)
	}
	if j != nil {
		// Called while the printer runs, so the monitor is not yet waiting
		// for its goroutines to end.
		p.m.wg.Add(1)
		go func() {
			defer p.m.wg.Done()
			p.fetch(ctx, *j)
		}()
	}
}

// nextJob decides whether the geometry has to be (re)fetched. A new print
// resets the geometry; a print whose files were not found yet is retried
// while it is active, because the printer may still be downloading them.
func (p *Printer) nextJob(s printer.Status) *job {
	if p.fetcher == nil || s.Subtask == "" || p.fetching {
		return nil
	}
	id := s.Subtask + "\x00" + s.TaskID
	if id != p.job {
		p.job = id
		p.attempts = 0
		p.geom = Geometry{State: GeometryLoading, Subtask: s.Subtask}
	} else {
		failed := p.geom.State == GeometryMissing || p.geom.State == GeometryError
		active := s.State == "PREPARE" || s.State == "RUNNING" || s.State == "PAUSE"
		if !failed || !active || p.attempts >= maxAttempts || time.Since(p.lastAttempt) < retryInterval {
			return nil
		}
	}
	// Without an SD card the printer keeps the files where FTPS cannot reach
	// them; they are fetched once a card is inserted.
	if s.NoSDCard {
		p.geom = Geometry{State: GeometryMissing, Subtask: s.Subtask}
		return nil
	}
	p.fetching = true
	p.attempts++
	p.lastAttempt = time.Now()
	return &job{id: id, subtask: s.Subtask, gcodeFile: s.GcodeFile}
}

func (p *Printer) fetch(ctx context.Context, j job) {
	g := p.fetcher(ctx, j)
	g.Subtask = j.subtask
	// Also when the print has changed meanwhile, its profiles name spools.
	defer p.m.learnFilaments(g.Settings)
	p.mu.Lock()
	p.fetching = false
	if p.job != j.id || p.geom.Uploaded {
		p.mu.Unlock()
		return
	}
	p.geom = g
	info := p.infoLocked()
	p.mu.Unlock()
	p.publish(info)
}

// fetchFTPS downloads and parses the print files. The download ends when
// ctx does or when the printer stops sending for a while.
func (p *Printer) fetchFTPS(ctx context.Context, j job) Geometry {
	fail := func(err error) Geometry {
		slog.Warn("geometry fetch failed", "printer", p.id, "print", j.subtask, "err", err)
		return Geometry{State: GeometryError, Error: err.Error()}
	}
	conn, err := ftps.Dial(ctx, p.cfg.Host, p.cfg.AccessCode, p.m.trust.TLS(p.cfg.Serial).VerifyConnection)
	if err != nil {
		return fail(err)
	}
	defer conn.Close()
	files, err := printjob.Find(conn, j.subtask, j.gcodeFile)
	if errors.Is(err, printjob.ErrNotFound) {
		return Geometry{State: GeometryMissing}
	}
	if err != nil {
		return fail(err)
	}
	res, err := printjob.Build(conn, files, p.m.store)
	if err != nil {
		return fail(err)
	}
	slog.Info("geometry ready", "printer", p.id, "print", j.subtask, "plate", files.Plate)
	g := ready(res)
	g.Plate, g.AMSMapping = files.Plate, files.AMSMapping
	return g
}

func (p *Printer) upload(res *printjob.Result) {
	p.mu.Lock()
	g := ready(res)
	g.Uploaded = true
	g.Subtask = p.status.Subtask
	p.geom = g
	if p.status.Subtask != "" {
		p.job = p.status.Subtask + "\x00" + p.status.TaskID
	}
	info := p.infoLocked()
	p.mu.Unlock()
	p.publish(info)
	p.m.learnFilaments(res.Settings)
}

// source delivers status updates of one printer.
type source interface {
	run(ctx context.Context, status func(printer.Status), online func(bool))
}

// lightSwitch is a source that can switch the printer's chamber light; a
// replay cannot.
type lightSwitch interface {
	setLight(on bool) error
}

// ErrNotSupported is returned for a command a printer cannot carry out.
var ErrNotSupported = errors.New("not supported by this printer")

// setLight switches the chamber light of a printer that reports one.
func (p *Printer) setLight(on bool) error {
	p.mu.Lock()
	hasLight := p.status.ChamberLight != ""
	p.mu.Unlock()
	ls, ok := p.src.(lightSwitch)
	if !ok || !hasLight {
		return ErrNotSupported
	}
	return ls.setLight(on)
}

type mqttSource struct {
	cfg    config.Printer
	cas    *trust.Store
	failed func(error)                    // why connecting failed, nil once connected
	client atomic.Pointer[printer.Client] // while running, for commands
}

// setLight switches the chamber light of the printer.
func (s *mqttSource) setLight(on bool) error {
	c := s.client.Load()
	if c == nil {
		return printer.ErrNotConnected
	}
	return c.SetLight(on)
}

func (s *mqttSource) report(err error) {
	if s.failed != nil {
		s.failed(err)
	}
}

func (s *mqttSource) run(ctx context.Context, status func(printer.Status), online func(bool)) {
	var c *printer.Client
	c = printer.New(s.cfg, s.cas, func(printer.Report) { status(c.Status()) })
	c.OnConnection(online)
	s.client.Store(c)
	defer s.client.Store(nil)
	// Also abandons a connection attempt still in progress, which would
	// otherwise leave a session open on the printer.
	defer c.Close()
	for attempt := 1; ; attempt++ {
		err := c.Connect(ctx)
		if err == nil {
			s.report(nil)
			break
		}
		if ctx.Err() != nil {
			return
		}
		s.report(err)
		// A printer switched off for the night would otherwise fill the
		// log with a warning every 30 seconds.
		level := slog.LevelDebug
		if attempt == 1 || attempt%unreachableReminder == 0 {
			level = slog.LevelWarn
		}
		slog.Log(ctx, level, "printer unreachable, retrying", "printer", s.cfg.ID, "attempt", attempt, "in", retryInterval, "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryInterval):
		}
	}
	<-ctx.Done()
}

type replaySource struct {
	path  string
	speed float64
}

func (s *replaySource) run(ctx context.Context, status func(printer.Status), online func(bool)) {
	online(true)
	state := printer.NewState()
	err := recording.Replay(ctx, s.path, s.speed, func(e recording.Entry) {
		if e.Kind == "report" && state.Apply(e.Payload) == nil {
			status(state.Status())
		}
	})
	if err != nil && ctx.Err() == nil {
		slog.Error("replay failed", "file", s.path, "err", err)
	} else if err == nil {
		slog.Info("replay finished", "file", s.path)
	}
}
