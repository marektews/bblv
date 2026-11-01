package monitor

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/marektews/bblv/internal/printer"
	"github.com/marektews/bblv/internal/recording"
)

// The answer to get_version, which comes first after connecting, and the
// reports of changed fields before the full one tell no state and no
// alerts; only statuses from the full report on are passed on.
func TestStatusesFromTheFullReportOn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a1.jsonl")
	rec, err := recording.Create(path, "A1", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, payload := range []string{
		`{"info":{"command":"get_version","module":[{"name":"ota","sw_ver":"01.08.01.00"}]}}`,
		`{"print":{"command":"push_status","msg":1,"mc_percent":55}}`,
		`{"print":{"command":"push_status","msg":0,"gcode_state":"RUNNING","mc_percent":55,"print_error":0,"hms":[{"attr":50335744,"code":131075}]}}`,
		`{"print":{"command":"push_status","msg":1,"mc_percent":56}}`,
	} {
		if err = rec.Write(now, []byte(payload)); err != nil {
			t.Fatal(err)
		}
	}
	rec.Close()

	var got []printer.Status
	src := &replaySource{path: path, speed: 1}
	src.run(context.Background(), func(s printer.Status) { got = append(got, s) }, func(bool) {})
	if len(got) != 2 {
		t.Fatalf("got %d statuses, want those of the last two reports", len(got))
	}
	if got[0].State != "RUNNING" || len(got[0].HMS) != 1 || got[0].Percent != 55 || got[1].Percent != 56 {
		t.Fatalf("statuses %+v", got)
	}
}
