package monitor

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/marektews/bblv/internal/camera"
	"github.com/marektews/bblv/internal/config"
	"github.com/marektews/bblv/internal/ftps"
	"github.com/marektews/bblv/internal/printer"
	"github.com/marektews/bblv/internal/printjob"
	"github.com/marektews/bblv/internal/recording"
)

const (
	retryInterval = 30 * time.Second
	maxAttempts   = 10
)

// testedModels are verified on real hardware; the others are experimental.
var testedModels = []string{"A1"}

// Printer is the runtime state of one monitored printer.
type Printer struct {
	m               *Monitor
	id, name, model string
	cfg             config.Printer
	src             source
	fetcher         func(job) Geometry // nil when geometry can only be uploaded
	camera          *camera.Hub        // nil when the camera is not available

	mu          sync.Mutex
	online      bool
	status      printer.Status
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
	if name == "" {
		name = id
	}
	return &Printer{m: m, id: id, name: name, model: model, src: src, geom: Geometry{State: GeometryNone}}
}

func (p *Printer) run(ctx context.Context) {
	p.src.run(ctx, p.onStatus, p.onConnection)
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
	return Info{
		ID: p.id, Name: p.name, Model: p.model, Experimental: experimental,
		Online: p.online, Camera: p.camera != nil, Status: p.status, Geometry: p.geom,
	}
}

func (p *Printer) onConnection(ok bool) {
	p.mu.Lock()
	changed := p.online != ok
	p.online = ok
	info := p.infoLocked()
	p.mu.Unlock()
	if changed {
		p.m.publish(info)
	}
}

func (p *Printer) onStatus(s printer.Status) {
	p.mu.Lock()
	if p.online && reflect.DeepEqual(p.status, s) {
		p.mu.Unlock()
		return
	}
	p.online = true
	p.status = s
	j := p.nextJob(s)
	info := p.infoLocked()
	p.mu.Unlock()
	p.m.publish(info)
	if j != nil {
		go p.fetch(*j)
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
	p.fetching = true
	p.attempts++
	p.lastAttempt = time.Now()
	return &job{id: id, subtask: s.Subtask, gcodeFile: s.GcodeFile}
}

func (p *Printer) fetch(j job) {
	g := p.fetcher(j)
	g.Subtask = j.subtask
	p.mu.Lock()
	p.fetching = false
	if p.job != j.id || p.geom.Uploaded {
		p.mu.Unlock()
		return
	}
	p.geom = g
	info := p.infoLocked()
	p.mu.Unlock()
	p.m.publish(info)
}

func (p *Printer) fetchFTPS(j job) Geometry {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fail := func(err error) Geometry {
		slog.Warn("geometry fetch failed", "printer", p.id, "print", j.subtask, "err", err)
		return Geometry{State: GeometryError, Error: err.Error()}
	}
	conn, err := ftps.Dial(ctx, p.cfg.Host, p.cfg.AccessCode)
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
	p.m.publish(info)
}

// source delivers status updates of one printer.
type source interface {
	run(ctx context.Context, status func(printer.Status), online func(bool))
}

type mqttSource struct {
	cfg config.Printer
}

func (s *mqttSource) run(ctx context.Context, status func(printer.Status), online func(bool)) {
	var c *printer.Client
	c = printer.New(s.cfg, func(printer.Report) { status(c.Status()) })
	c.OnConnection(online)
	for {
		err := c.Connect(ctx)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return
		}
		slog.Warn("printer unreachable, retrying", "printer", s.cfg.ID, "in", retryInterval, "err", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(retryInterval):
		}
	}
	<-ctx.Done()
	c.Close()
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
