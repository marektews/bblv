package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/marektews/bblv/internal/auth"
	"github.com/marektews/bblv/internal/cache"
	"github.com/marektews/bblv/internal/config"
	"github.com/marektews/bblv/internal/monitor"
)

const recordingFile = `{"t":"2026-10-18T20:00:00Z","kind":"meta","model":"A1"}
{"t":"2026-10-18T20:00:01Z","kind":"report","payload":{"print":{"gcode_state":"RUNNING","subtask_name":"cube","layer_num":3,"print_error":50348044,"hms":[{"attr":50331904,"code":65537}]}}}
`

const gcodeFile = "; CHANGE_LAYER\n; LAYER_HEIGHT: 0.2\n; LINE_WIDTH: 0.4\nG90\nM83\nG1 X0 Y0 Z0.2\nG1 X10 E1\n"

func setup(t *testing.T) *httptest.Server {
	ts, _ := setupWithConfig(t)
	return ts
}

func setupWithConfig(t *testing.T) (*httptest.Server, *config.Store) {
	t.Helper()
	dir := t.TempDir()
	rec := filepath.Join(dir, "rec.jsonl")
	if err := os.WriteFile(rec, []byte(recordingFile), 0o600); err != nil {
		t.Fatal(err)
	}
	store, err := cache.Open(filepath.Join(dir, "cache"), cache.DefaultLimit)
	if err != nil {
		t.Fatal(err)
	}
	mon := monitor.New(store, "")
	if err = mon.AddReplay(rec, 0); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go mon.Run(ctx)
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		if info, _ := mon.Printer("replay"); info.Status.Subtask == "cube" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("replay did not start")
		}
	}
	cfg, err := config.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(New(Deps{Monitor: mon, Config: cfg, Auth: auth.New(cfg)}))
	t.Cleanup(ts.Close)
	return ts, cfg
}

func getJSON(t *testing.T, url string, v any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if v != nil {
		json.NewDecoder(resp.Body).Decode(v)
	}
	return resp.StatusCode
}

func TestUploadAndGeometry(t *testing.T) {
	ts := setup(t)

	var list []monitor.Info
	if code := getJSON(t, ts.URL+"/api/printers", &list); code != 200 || len(list) != 1 || list[0].ID != "replay" || list[0].Model != "A1" {
		t.Fatalf("printers: %d %+v", code, list)
	}
	if code := getJSON(t, ts.URL+"/api/printers/replay/geometry", nil); code != http.StatusNotFound {
		t.Fatalf("geometry before upload: %d", code)
	}

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "cube.gcode")
	io.WriteString(fw, gcodeFile)
	mw.Close()
	resp, err := http.Post(ts.URL+"/api/printers/replay/upload", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	var info monitor.Info
	json.NewDecoder(resp.Body).Decode(&info)
	resp.Body.Close()
	if resp.StatusCode != 200 || info.Geometry.State != monitor.GeometryReady || !info.Geometry.Uploaded {
		t.Fatalf("upload: %d %+v", resp.StatusCode, info.Geometry)
	}

	resp, err = http.Get(ts.URL + "/api/printers/replay/geometry")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !bytes.HasPrefix(data, []byte("BBLG")) || !resp.Uncompressed {
		t.Fatalf("geometry: magic %q, gzip %v", data[:min(4, len(data))], resp.Uncompressed)
	}
	etag := resp.Header.Get("ETag")
	if etag != `"`+info.Geometry.Key+`"` {
		t.Fatalf("etag %s, key %s", etag, info.Geometry.Key)
	}
	req, _ := http.NewRequest("GET", ts.URL+"/api/printers/replay/geometry", nil)
	req.Header.Set("If-None-Match", etag)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotModified {
		t.Fatalf("conditional get: %d", resp.StatusCode)
	}
}

func TestUploadRejectsOtherFiles(t *testing.T) {
	ts := setup(t)
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "model.stl")
	io.WriteString(fw, "solid")
	mw.Close()
	resp, err := http.Post(ts.URL+"/api/printers/replay/upload", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestEvents(t *testing.T) {
	ts := setup(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content type %q", ct)
	}
	sc := bufio.NewScanner(resp.Body)
	if !sc.Scan() || sc.Text() != "event: printer" {
		t.Fatalf("first line %q", sc.Text())
	}
	if !sc.Scan() || !strings.HasPrefix(sc.Text(), "data: ") || !strings.Contains(sc.Text(), `"subtask":"cube"`) {
		t.Fatalf("data line %q", sc.Text())
	}
	// The printers sent on connect end with their ids.
	var lines []string
	for len(lines) < 3 && sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if want := []string{"", "event: ready", `data: ["replay"]`}; !reflect.DeepEqual(lines, want) {
		t.Fatalf("lines after the printer = %q, want %q", lines, want)
	}
}

func TestFrontendPlaceholderAndUnknownAPI(t *testing.T) {
	ts := setup(t)
	resp, err := http.Get(ts.URL + "/printer/replay")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(b), "<html") && !strings.Contains(string(b), "<!doctype html>") {
		t.Fatalf("frontend: %d %q", resp.StatusCode, b)
	}
	if code := getJSON(t, ts.URL+"/api/nope", nil); code != http.StatusNotFound {
		t.Fatalf("unknown api: %d", code)
	}
}

func TestAlerts(t *testing.T) {
	ts := setup(t)
	var alerts []struct{ Kind, Code, Severity, Text, URL string }
	if code := getJSON(t, ts.URL+"/api/printers/replay/alerts?lang=pl", &alerts); code != 200 || len(alerts) != 2 {
		t.Fatalf("alerts: %d %+v", code, alerts)
	}
	e, h := alerts[0], alerts[1]
	if e.Kind != "error" || e.Code != "0300_400C" || !strings.Contains(e.URL, "s=device_error") {
		t.Errorf("print error = %+v", e)
	}
	if h.Kind != "hms" || h.Code != "0300_0100_0001_0001" || h.Severity != "fatal" || h.Text == "" || !strings.Contains(h.URL, "lang=pl") {
		t.Errorf("hms = %+v", h)
	}
}
