package monitor

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/marektews/bblv/internal/config"
	"github.com/marektews/bblv/internal/printer"
)

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if cond() {
			return
		}
	}
	t.Fatal("condition not met")
}

func TestFetchOnNewPrintAndRetry(t *testing.T) {
	m := New(nil, "")
	calls := make(chan job, 10)
	results := make(chan Geometry)
	p := newPrinter(m, "p", "", "A1", nil)
	p.fetcher = func(j job) Geometry {
		calls <- j
		return <-results
	}
	m.add(p)
	state := func() string { return p.info().Geometry.State }

	p.onStatus(printer.Status{State: "PREPARE", Subtask: "a", TaskID: "1"})
	if j := <-calls; j.subtask != "a" {
		t.Fatalf("job = %+v", j)
	}
	if state() != GeometryLoading {
		t.Fatalf("state = %s, want loading", state())
	}
	results <- Geometry{State: GeometryMissing}
	eventually(t, func() bool { return state() == GeometryMissing })

	// Too early for a retry.
	p.onStatus(printer.Status{State: "RUNNING", Subtask: "a", TaskID: "1", Layer: 1})
	select {
	case j := <-calls:
		t.Fatalf("unexpected retry %+v", j)
	case <-time.After(20 * time.Millisecond):
	}

	p.mu.Lock()
	p.lastAttempt = time.Now().Add(-time.Minute)
	p.mu.Unlock()
	p.onStatus(printer.Status{State: "RUNNING", Subtask: "a", TaskID: "1", Layer: 2})
	<-calls
	results <- Geometry{State: GeometryReady, Key: "k1"}
	eventually(t, func() bool { return p.info().Geometry.Key == "k1" })
	if g := p.info().Geometry; g.Subtask != "a" {
		t.Fatalf("geometry = %+v", g)
	}

	// Another print with the same name but a new task id is fetched again.
	p.onStatus(printer.Status{State: "PREPARE", Subtask: "a", TaskID: "2"})
	<-calls
	results <- Geometry{State: GeometryReady, Key: "k2"}
	eventually(t, func() bool { return p.info().Geometry.Key == "k2" })
}

func TestFinishedPrintIsNotRetried(t *testing.T) {
	m := New(nil, "")
	calls := make(chan job, 10)
	p := newPrinter(m, "p", "", "X1C", nil)
	p.fetcher = func(j job) Geometry {
		calls <- j
		return Geometry{State: GeometryMissing}
	}
	p.onStatus(printer.Status{State: "FINISH", Subtask: "a"})
	<-calls
	eventually(t, func() bool { return p.info().Geometry.State == GeometryMissing })
	p.mu.Lock()
	p.lastAttempt = time.Now().Add(-time.Minute)
	p.mu.Unlock()
	p.onStatus(printer.Status{State: "FINISH", Subtask: "a", Percent: 100})
	select {
	case <-calls:
		t.Fatal("finished print retried")
	case <-time.After(20 * time.Millisecond):
	}
	if !p.info().Experimental {
		t.Error("X1C should be experimental")
	}
}

// The UI expects lists, not null, also before the printer's first report.
func TestStatusBeforeFirstReport(t *testing.T) {
	p := newPrinter(New(nil, ""), "p", "", "A1", nil)
	b, err := json.Marshal(p.info().Status)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"hms":[]`, `"ams":[]`, `"activeTray":-1`, `"external":null`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("status %s lacks %s", b, want)
		}
	}
}

func TestCameraErrorFromReport(t *testing.T) {
	for _, tc := range []struct {
		model, ffmpeg, liveView, want string
	}{
		{"A1", "", "", ""}, // JPEG camera, needs no ffmpeg
		{"P2S", "/usr/bin/ffmpeg", "disabled", "liveview-disabled"}, // as the printer reports
		{"H2D", "", "enabled", "ffmpeg-missing"},
		{"X2D", "/usr/bin/ffmpeg", "enabled", ""}, // not known before the stream runs
	} {
		p := New(nil, tc.ffmpeg).networkPrinter(config.Printer{ID: "p", Model: tc.model, Host: "10.0.0.5"})
		p.status.LANLiveView = tc.liveView
		if got := p.info().CameraError; got != tc.want {
			t.Errorf("%s ffmpeg=%q liveView=%q: cameraError = %q, want %q", tc.model, tc.ffmpeg, tc.liveView, got, tc.want)
		}
	}
}

func TestAMSMappingFromReportWithoutBBL(t *testing.T) {
	p := newPrinter(New(nil, ""), "p", "", "P2S", nil)
	p.status.AMSMapping = []int{3}
	if got := p.info().Geometry.AMSMapping; len(got) != 1 || got[0] != 3 {
		t.Fatalf("mapping = %v, want the reported [3]", got)
	}
	p.geom.AMSMapping = []int{1} // from a .bbl file
	if got := p.info().Geometry.AMSMapping; len(got) != 1 || got[0] != 1 {
		t.Fatalf("mapping = %v, want the .bbl one", got)
	}
}
