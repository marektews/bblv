// Package gcode turns slicer G-code into compact toolpath geometry for the 3D view.
package gcode

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"io"
	"math"
)

// Point is one vertex of the toolpath. The segment ending at a point uses the
// point's attributes; Width 0 marks a move-to, i.e. no segment ends there.
type Point struct {
	X, Y, Z float32
	Width   float32
	Height  float32
	Layer   uint16 // 0-based index into Model.Layers
	Tool    uint8
	Feature uint8 // index into Model.Features
}

// Layer is a contiguous range of points printed at one layer change.
type Layer struct {
	Z      float32 `json:"z"`
	Height float32 `json:"height"`
	First  int     `json:"first"`
	Count  int     `json:"count"`
}

// Model is the parsed toolpath of one print.
type Model struct {
	Layers    []Layer
	Features  []string // feature names, e.g. "Outer wall"; index 0 is unknown
	Filaments []string // filament colours as #RRGGBB, indexed by tool
	Points    []Point
	Bounds    [6]float32 // min x, y, z, max x, y, z
	Segments  int        // extrusion segments before simplification
}

// Binary format, little endian:
//
//	"BBLG" | uint32 version | uint32 n | n bytes of JSON metadata padded with spaces to 4 bytes |
//	points as PointFloats float32 each: x, y, z, width, height, layer, tool*256+feature
const (
	magic       = "BBLG"
	Version     = 1
	PointFloats = 7
)

type meta struct {
	Version   int        `json:"version"`
	Points    int        `json:"points"`
	Layers    []Layer    `json:"layers"`
	Features  []string   `json:"features"`
	Filaments []string   `json:"filaments"`
	Bounds    [6]float32 `json:"bounds"`
}

// Encode writes the model in the binary format served to the browser.
func (m *Model) Encode(w io.Writer) error {
	js, err := json.Marshal(meta{
		Version:   Version,
		Points:    len(m.Points),
		Layers:    nonNil(m.Layers),
		Features:  nonNil(m.Features),
		Filaments: nonNil(m.Filaments),
		Bounds:    m.Bounds,
	})
	if err != nil {
		return err
	}
	for len(js)%4 != 0 {
		js = append(js, ' ')
	}
	bw := bufio.NewWriter(w)
	head := make([]byte, 12)
	copy(head, magic)
	binary.LittleEndian.PutUint32(head[4:], Version)
	binary.LittleEndian.PutUint32(head[8:], uint32(len(js)))
	bw.Write(head)
	bw.Write(js)
	buf := make([]byte, 4*PointFloats)
	for _, p := range m.Points {
		for i, v := range [PointFloats]float32{p.X, p.Y, p.Z, p.Width, p.Height, float32(p.Layer), float32(int(p.Tool)<<8 | int(p.Feature))} {
			binary.LittleEndian.PutUint32(buf[4*i:], math.Float32bits(v))
		}
		bw.Write(buf)
	}
	return bw.Flush()
}

func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}
