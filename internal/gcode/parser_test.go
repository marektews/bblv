package gcode

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"math"
	"strconv"
	"strings"
	"testing"
)

const sample = `; filament_colour = #FF0000;#00FF00
G90
M83
G1 X10 Y10 E5 ; purge line before the first layer is ignored
; MACHINE_START_GCODE_END
; CHANGE_LAYER
; Z_HEIGHT: 0.2
; LAYER_HEIGHT: 0.2
G1 Z0.2
G1 X0 Y0
; FEATURE: Outer wall
; LINE_WIDTH: 0.42
G1 X10 Y0 E1
G1 X20 Y0 E1
G1 X20 Y10 E1
G1 X25 Y10 E-0.8
G1 X30 Y10
G1 X40 Y10 E1
; CHANGE_LAYER
; Z_HEIGHT: 0.4
; LAYER_HEIGHT: 0.2
G1 Z0.4
T1
; FEATURE: Sparse infill
G91
G1 X-40 E2
G90
M83
G3 X50 Y10 I5 J0 E1
; MACHINE_END_GCODE_START
G1 X0 Y0 E5
`

func TestParse(t *testing.T) {
	m, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Layers) != 2 {
		t.Fatalf("layers = %d, want 2", len(m.Layers))
	}
	if got := m.Filaments; len(got) != 2 || got[1] != "#00FF00" {
		t.Fatalf("filaments = %v", got)
	}

	// Layer 1: move-to (0,0), merged (20,0), (20,10), move-to (30,10), (40,10).
	l1 := m.Points[m.Layers[0].First : m.Layers[0].First+m.Layers[0].Count]
	want := [][3]float32{{0, 0, 0}, {20, 0, 0.42}, {20, 10, 0.42}, {30, 10, 0}, {40, 10, 0.42}}
	if len(l1) != len(want) {
		t.Fatalf("layer 1 points = %+v", l1)
	}
	for i, w := range want {
		if l1[i].X != w[0] || l1[i].Y != w[1] || l1[i].Width != w[2] || l1[i].Z != 0.2 {
			t.Errorf("layer 1 point %d = %+v, want %v", i, l1[i], w)
		}
	}
	if m.Features[l1[1].Feature] != "Outer wall" {
		t.Errorf("feature = %q", m.Features[l1[1].Feature])
	}

	// Layer 2: relative move to (0,10) with tool 1, then a counter-clockwise
	// half circle around (5,10) ending at (50,10)... which is not on that
	// circle, so only check that the arc was linearized and ends correctly.
	l2 := m.Points[m.Layers[1].First : m.Layers[1].First+m.Layers[1].Count]
	if l2[1].X != 0 || l2[1].Y != 10 || l2[1].Tool != 1 || m.Features[l2[1].Feature] != "Sparse infill" {
		t.Errorf("relative move = %+v", l2[1])
	}
	if last := l2[len(l2)-1]; len(l2) < 4 || last.X != 50 || last.Y != 10 {
		t.Errorf("arc points = %+v", l2)
	}
	if got := m.Segments - (len(l2) - 1); got != 4 {
		t.Errorf("layer 1 segments before merging = %d, want 4", got)
	}
	if m.Layers[1].Z != 0.4 || m.Layers[1].Height != 0.2 {
		t.Errorf("layer 2 = %+v", m.Layers[1])
	}
	if m.Bounds[3] != 50 {
		t.Errorf("bounds = %v", m.Bounds)
	}
}

func TestArcStaysOnCircle(t *testing.T) {
	src := "; CHANGE_LAYER\n; LAYER_HEIGHT: 0.2\n; LINE_WIDTH: 0.4\nG90\nM83\nG1 X10 Y0 Z0.2\nG2 X-10 Y0 I-10 J0 E3\n"
	m, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Points) < 10 {
		t.Fatalf("points = %d, want a finely linearized arc", len(m.Points))
	}
	for _, p := range m.Points {
		if r := math.Hypot(float64(p.X), float64(p.Y)); math.Abs(r-10) > 1e-3 {
			t.Fatalf("point %+v off circle, r=%v", p, r)
		}
		if p.Width > 0 && p.Y > 1e-3 {
			t.Fatalf("clockwise arc from (10,0) to (-10,0) must pass below the X axis, got %+v", p)
		}
	}
}

func TestWidthFromVolume(t *testing.T) {
	// 10 mm line, 0.2 mm high; E chosen so that the rounded rectangle is 0.45 mm wide.
	h, w := 0.2, 0.45
	area := (w-h)*h + math.Pi*h*h/4
	e := area * 10 / (math.Pi * 1.75 * 1.75 / 4)
	src := "; CHANGE_LAYER\n; LAYER_HEIGHT: 0.2\nG90\nM83\nG1 X0 Y0 Z0.2\nG1 X10 E" + strconv.FormatFloat(e, 'f', 8, 64) + "\n"
	m, err := Parse(strings.NewReader(src))
	if err != nil {
		t.Fatal(err)
	}
	if got := m.Points[1].Width; math.Abs(float64(got)-w) > 1e-3 {
		t.Fatalf("width = %v, want %v", got, w)
	}
}

func TestEncode(t *testing.T) {
	m, err := Parse(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := m.Encode(&buf); err != nil {
		t.Fatal(err)
	}
	b := buf.Bytes()
	if string(b[:4]) != magic || binary.LittleEndian.Uint32(b[4:]) != Version {
		t.Fatalf("bad header %q", b[:8])
	}
	n := int(binary.LittleEndian.Uint32(b[8:]))
	if n%4 != 0 {
		t.Fatalf("metadata length %d not aligned", n)
	}
	var md meta
	if err := json.Unmarshal(b[12:12+n], &md); err != nil {
		t.Fatal(err)
	}
	if md.Points != len(m.Points) || len(md.Layers) != 2 {
		t.Fatalf("meta = %+v", md)
	}
	pts := b[12+n:]
	if len(pts) != len(m.Points)*PointFloats*4 {
		t.Fatalf("points bytes = %d", len(pts))
	}
	f := func(i int) float32 { return math.Float32frombits(binary.LittleEndian.Uint32(pts[4*i:])) }
	p := m.Points[len(m.Points)-1]
	base := (len(m.Points) - 1) * PointFloats
	if f(base) != p.X || f(base+5) != float32(p.Layer) || f(base+6) != float32(int(p.Tool)<<8|int(p.Feature)) {
		t.Fatalf("last point encoded as %v %v %v", f(base), f(base+5), f(base+6))
	}
}
