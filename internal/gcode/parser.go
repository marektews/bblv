package gcode

import (
	"bufio"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
)

const (
	arcTolerance   = 0.01  // max chord deviation when linearizing arcs, mm
	mergeTolerance = 0.005 // max deviation when merging collinear segments, mm
	maxArcSteps    = 720
)

// MaxPoints limits the toolpath to about 150 MB in memory. Real prints
// stay far below: a large one has a few hundred thousand points.
var MaxPoints = 5_000_000

// MaxLayers limits the layers, whose index a point keeps in 16 bits.
var MaxLayers = 1 << 16

// MaxBytes limits the G-code read: a small .3mf can unpack to gigabytes.
// Real prints have at most a few hundred megabytes.
var MaxBytes int64 = 2 << 30

// ErrTooLarge is returned for G-code beyond MaxPoints, MaxLayers or MaxBytes.
var ErrTooLarge = errors.New("the print has too many moves to show")

// Parse reads G-code produced by Bambu Studio (or a slicer using the Prusa
// style ";LAYER_CHANGE" / ";TYPE:" markers) and returns the extruded toolpath.
// Moves before the first layer change and after the end G-code are ignored.
func Parse(r io.Reader) (*Model, error) {
	p := &parser{m: &Model{}, features: map[string]int{}, filamentDiam: 1.75}
	p.feature = p.featureIndex("")
	limited := &io.LimitedReader{R: r, N: MaxBytes + 1}
	sc := bufio.NewScanner(limited)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		p.line(sc.Text())
		if len(p.m.Points) > MaxPoints || len(p.m.Layers) > MaxLayers {
			return nil, ErrTooLarge
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if limited.N <= 0 {
		return nil, ErrTooLarge
	}
	p.finish()
	return p.m, nil
}

type parser struct {
	m *Model

	x, y, z, e   float64
	relXYZ, relE bool
	layer        int // 1-based; 0 before the first layer change
	ended        bool
	height       float64
	width        float64
	tool         int
	feature      int
	features     map[string]int
	filamentDiam float64
}

func (p *parser) line(s string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	if s[0] == ';' {
		p.comment(strings.TrimSpace(s[1:]))
		return
	}
	if i := strings.IndexByte(s, ';'); i >= 0 {
		s = s[:i]
	}
	f := strings.Fields(s)
	if len(f) == 0 {
		return
	}
	switch f[0] {
	case "G0", "G1":
		p.move(f[1:], 0)
	case "G2":
		p.move(f[1:], -1)
	case "G3":
		p.move(f[1:], 1)
	case "G90":
		p.relXYZ, p.relE = false, false
	case "G91":
		p.relXYZ, p.relE = true, true
	case "M82":
		p.relE = false
	case "M83":
		p.relE = true
	case "G92":
		p.setPosition(f[1:])
	default:
		// Bambu start G-code also uses special tools such as T255 and T1000.
		if f[0][0] == 'T' {
			if n, err := strconv.Atoi(f[0][1:]); err == nil && n < 64 {
				p.tool = n
			}
		}
	}
}

func (p *parser) comment(c string) {
	key, val, _ := strings.Cut(c, ":")
	key, val = strings.TrimSpace(key), strings.TrimSpace(val)
	switch key {
	case "CHANGE_LAYER", "LAYER_CHANGE":
		if !p.ended {
			p.closeLayer()
			p.layer++
			p.m.Layers = append(p.m.Layers, Layer{Z: float32(p.z), First: len(p.m.Points)})
		}
	case "Z_HEIGHT", "Z":
		if v, err := strconv.ParseFloat(val, 64); err == nil && p.layer > 0 {
			p.m.Layers[p.layer-1].Z = float32(v)
		}
	case "LAYER_HEIGHT", "HEIGHT":
		if v, err := strconv.ParseFloat(val, 64); err == nil {
			p.height = v
			if p.layer > 0 && p.m.Layers[p.layer-1].Height == 0 {
				p.m.Layers[p.layer-1].Height = float32(v)
			}
		}
	case "FEATURE", "TYPE":
		p.feature = p.featureIndex(val)
	case "LINE_WIDTH", "WIDTH":
		if v, err := strconv.ParseFloat(val, 64); err == nil {
			p.width = v
		}
	case "MACHINE_END_GCODE_START":
		p.closeLayer()
		p.ended = true
	case "total filament weight [g]": // Bambu Studio header
		p.m.UsedG, _ = strconv.ParseFloat(val, 64)
	case "total filament length [mm]":
		if v, err := strconv.ParseFloat(val, 64); err == nil {
			p.m.UsedM = v / 1000
		}
	default:
		// Slicer settings look like "filament_colour = #FFFFFF;#000000".
		if k, v, ok := strings.Cut(c, " = "); ok {
			p.setting(k, v)
		}
	}
}

func (p *parser) setting(key, val string) {
	switch key {
	case "filament_colour":
		p.m.Filaments = strings.Split(val, ";")
	case "filament_diameter":
		first, _, _ := strings.Cut(val, ",")
		if v, err := strconv.ParseFloat(first, 64); err == nil && v > 0 {
			p.filamentDiam = v
		}
	case "print_settings_id":
		p.m.Settings.PrintProfile = profileName(strings.Trim(val, `"`))
	case "printer_settings_id":
		p.m.Settings.PrinterProfile = profileName(strings.Trim(val, `"`))
	case "layer_height":
		p.m.Settings.LayerHeight, _ = strconv.ParseFloat(val, 64)
	case "filament_settings_id":
		names := list(val)
		for i := range names {
			names[i] = profileName(names[i])
		}
		p.m.Settings.FilamentProfiles = names
	case "filament_ids":
		p.m.Settings.FilamentIDs = list(val)
	}
}

// list splits a setting with a value per filament, e.g. GFA00;GFL99 or,
// for names, "Bambu PLA Basic @BBL A1";"Generic PLA @BBL A1".
func list(val string) []string {
	if len(val) >= 2 && strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`) {
		return strings.Split(val[1:len(val)-1], `";"`)
	}
	return strings.Split(val, ";")
}

// profileName drops what Bambu Studio appends to the name of a profile that
// came with a project, e.g. "Generic PLA(model.3mf)(model.3mf)".
func profileName(name string) string {
	for strings.HasSuffix(name, ".3mf)") {
		i := strings.LastIndexByte(name, '(')
		if i < 0 {
			break
		}
		name = name[:i]
	}
	return strings.TrimSpace(name)
}

func (p *parser) featureIndex(name string) int {
	if i, ok := p.features[name]; ok {
		return i
	}
	if len(p.m.Features) > math.MaxUint8 {
		return 0
	}
	i := len(p.m.Features)
	p.features[name] = i
	p.m.Features = append(p.m.Features, name)
	return i
}

func (p *parser) setPosition(args []string) {
	for _, a := range args {
		if len(a) < 2 {
			continue
		}
		v, err := strconv.ParseFloat(a[1:], 64)
		if err != nil {
			continue
		}
		switch a[0] {
		case 'X':
			p.x = v
		case 'Y':
			p.y = v
		case 'Z':
			p.z = v
		case 'E':
			p.e = v
		}
	}
}

// move handles G0-G3; arc is 0 for a line, -1 for clockwise and 1 for counter-clockwise.
func (p *parser) move(args []string, arc int) {
	x, y, z := p.x, p.y, p.z
	var e, i, j float64
	hasE := false
	for _, a := range args {
		if len(a) < 2 {
			continue
		}
		v, err := strconv.ParseFloat(a[1:], 64)
		if err != nil {
			continue
		}
		switch a[0] {
		case 'X':
			x = v + p.offset(p.x)
		case 'Y':
			y = v + p.offset(p.y)
		case 'Z':
			z = v + p.offset(p.z)
		case 'E':
			hasE = true
			if p.relE {
				e = v
			} else {
				e, p.e = v-p.e, v
			}
		case 'I':
			i = v
		case 'J':
			j = v
		}
	}
	if hasE && e > 0 && p.layer > 0 && !p.ended {
		if arc != 0 && (i != 0 || j != 0) {
			p.arc(x, y, z, i, j, arc, e)
		} else if l := math.Sqrt((x-p.x)*(x-p.x) + (y-p.y)*(y-p.y) + (z-p.z)*(z-p.z)); l > 1e-6 {
			p.emit(p.x, p.y, p.z, x, y, z, p.lineWidth(e, l))
		}
	}
	p.x, p.y, p.z = x, y, z
}

func (p *parser) offset(cur float64) float64 {
	if p.relXYZ {
		return cur
	}
	return 0
}

func (p *parser) arc(x1, y1, z1, i, j float64, dir int, e float64) {
	cx, cy := p.x+i, p.y+j
	r := math.Hypot(p.x-cx, p.y-cy)
	a0 := math.Atan2(p.y-cy, p.x-cx)
	sweep := math.Atan2(y1-cy, x1-cx) - a0
	full := math.Abs(x1-p.x) < 1e-4 && math.Abs(y1-p.y) < 1e-4
	switch {
	case dir < 0 && (sweep >= 0 || full):
		sweep -= 2 * math.Pi
	case dir > 0 && (sweep <= 0 || full):
		sweep += 2 * math.Pi
	}
	n := 1
	if r > arcTolerance {
		n = int(math.Ceil(math.Abs(sweep) / (2 * math.Acos(1-arcTolerance/r))))
	}
	n = max(1, min(n, maxArcSteps))
	w := p.lineWidth(e, math.Hypot(math.Abs(sweep)*r, z1-p.z))
	px, py, pz := p.x, p.y, p.z
	for k := 1; k <= n; k++ {
		t := float64(k) / float64(n)
		qx, qy := x1, y1
		if k < n {
			a := a0 + sweep*t
			qx, qy = cx+r*math.Cos(a), cy+r*math.Sin(a)
		}
		qz := p.z + (z1-p.z)*t
		p.emit(px, py, pz, qx, qy, qz, w)
		px, py, pz = qx, qy, qz
	}
}

// lineWidth returns the width announced by the slicer or, when missing,
// estimates it from the extruded volume assuming a rounded rectangle profile.
func (p *parser) lineWidth(e, length float64) float64 {
	if p.width > 0 {
		return p.width
	}
	h := p.segmentHeight()
	if h <= 0 || length <= 0 {
		return 0.4
	}
	area := math.Pi * p.filamentDiam * p.filamentDiam / 4
	return e*area/(length*h) + h*(1-math.Pi/4)
}

func (p *parser) segmentHeight() float64 {
	if p.height > 0 {
		return p.height
	}
	if p.layer > 0 {
		return float64(p.m.Layers[p.layer-1].Height)
	}
	return 0
}

// emit appends the segment (x0,y0,z0)-(x1,y1,z1), continuing the current
// polyline when possible and merging it with the previous collinear segment.
func (p *parser) emit(x0, y0, z0, x1, y1, z1, w float64) {
	p.m.Segments++
	pt := Point{
		X: float32(x1), Y: float32(y1), Z: float32(z1),
		Width:   float32(w),
		Height:  float32(p.segmentHeight()),
		Layer:   uint16(p.layer - 1),
		Tool:    uint8(p.tool),
		Feature: uint8(p.feature),
	}
	pts := p.m.Points
	n := len(pts)
	first := p.m.Layers[p.layer-1].First
	if n > first && near(pts[n-1], x0, y0, z0) {
		if n-1 > first && pts[n-1].Width > 0 && sameAttrs(pts[n-1], pt) && collinear(pts[n-2], pts[n-1], pt) {
			pts[n-1] = pt
			return
		}
		p.m.Points = append(pts, pt)
		return
	}
	p.m.Points = append(pts, Point{X: float32(x0), Y: float32(y0), Z: float32(z0), Layer: pt.Layer}, pt)
}

func (p *parser) closeLayer() {
	if n := len(p.m.Layers); n > 0 && !p.ended {
		l := &p.m.Layers[n-1]
		l.Count = len(p.m.Points) - l.First
	}
}

func (p *parser) finish() {
	p.closeLayer()
	prevZ := float32(0)
	for i := range p.m.Layers {
		l := &p.m.Layers[i]
		if l.Height == 0 {
			l.Height = l.Z - prevZ
		}
		prevZ = l.Z
	}
	if len(p.m.Points) == 0 {
		return
	}
	b := [6]float32{math.MaxFloat32, math.MaxFloat32, math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32, -math.MaxFloat32}
	for _, pt := range p.m.Points {
		b[0], b[1], b[2] = min(b[0], pt.X), min(b[1], pt.Y), min(b[2], pt.Z)
		b[3], b[4], b[5] = max(b[3], pt.X), max(b[4], pt.Y), max(b[5], pt.Z)
	}
	p.m.Bounds = b
}

func near(pt Point, x, y, z float64) bool {
	return math.Abs(float64(pt.X)-x) < 1e-4 && math.Abs(float64(pt.Y)-y) < 1e-4 && math.Abs(float64(pt.Z)-z) < 1e-4
}

func sameAttrs(a, b Point) bool {
	return a.Tool == b.Tool && a.Feature == b.Feature && a.Layer == b.Layer &&
		math.Abs(float64(a.Width-b.Width)) <= 0.01*float64(a.Width) &&
		math.Abs(float64(a.Height-b.Height)) < 1e-4
}

// collinear reports whether b lies on the segment a-c within mergeTolerance.
func collinear(a, b, c Point) bool {
	acx, acy, acz := float64(c.X-a.X), float64(c.Y-a.Y), float64(c.Z-a.Z)
	abx, aby, abz := float64(b.X-a.X), float64(b.Y-a.Y), float64(b.Z-a.Z)
	l := math.Sqrt(acx*acx + acy*acy + acz*acz)
	if l < 1e-9 {
		return false
	}
	if abx*(float64(c.X-b.X))+aby*(float64(c.Y-b.Y))+abz*(float64(c.Z-b.Z)) <= 0 {
		return false
	}
	cx := aby*acz - abz*acy
	cy := abz*acx - abx*acz
	cz := abx*acy - aby*acx
	return math.Sqrt(cx*cx+cy*cy+cz*cz)/l <= mergeTolerance
}
