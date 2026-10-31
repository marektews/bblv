package gcode

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the geometry fixture shared with the frontend tests")

// fixtureDir holds the encoded sample and what it contains, which the
// frontend test web/src/viewer/geometry.test.ts decodes and compares.
var fixtureDir = filepath.Join("..", "..", "web", "src", "viewer", "testdata")

// fixtureSummary is what the frontend has to read from the fixture.
type fixtureSummary struct {
	Points    int          `json:"points"`
	Layers    []Layer      `json:"layers"`
	Features  []string     `json:"features"`
	Filaments []string     `json:"filaments"`
	Bounds    [6]float32   `json:"bounds"`
	Tools     []int        `json:"tools"` // tools of extruding segments
	Data      [][7]float32 `json:"data"`  // every point as the frontend sees it
}

func TestGeometryFixture(t *testing.T) {
	m, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	var bin bytes.Buffer
	if err = m.Encode(&bin); err != nil {
		t.Fatal(err)
	}
	want := fixtureSummary{Points: len(m.Points), Layers: m.Layers, Features: m.Features, Filaments: m.Filaments, Bounds: m.Bounds}
	for _, p := range m.Points {
		want.Data = append(want.Data, [7]float32{p.X, p.Y, p.Z, p.Width, p.Height, float32(p.Layer), float32(int(p.Tool)<<8 | int(p.Feature))})
		if p.Width > 0 && !slices.Contains(want.Tools, int(p.Tool)) {
			want.Tools = append(want.Tools, int(p.Tool))
		}
	}
	slices.Sort(want.Tools)
	binPath, jsonPath := filepath.Join(fixtureDir, "geometry.bin"), filepath.Join(fixtureDir, "geometry.json")

	if *update {
		js, _ := json.MarshalIndent(want, "", "  ")
		if err = os.MkdirAll(fixtureDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(binPath, bin.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(jsonPath, append(js, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	const hint = "; after changing the format or the parser, run go test ./internal/gcode -run TestGeometryFixture -update and then the frontend tests"
	got, err := os.ReadFile(binPath)
	if err != nil || !bytes.Equal(got, bin.Bytes()) {
		t.Fatalf("%s is out of date (%v)%s", binPath, err, hint)
	}
	js, err := os.ReadFile(jsonPath)
	var summary fixtureSummary
	if err != nil || json.Unmarshal(js, &summary) != nil || !reflect.DeepEqual(summary, want) {
		t.Fatalf("%s is out of date (%v)%s", jsonPath, err, hint)
	}
}
