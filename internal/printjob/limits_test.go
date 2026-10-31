package printjob

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/marektews/bblv/internal/gcode"
)

// A crafted .3mf may unpack to far more than its size; the thumbnail is
// then left out instead of being read whole.
func TestOversizedZipEntryNotRead(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("Metadata/plate_1.png")
	w.Write(make([]byte, maxZipEntry+1)) // compresses to a few kB
	w, _ = zw.Create("Metadata/plate_1.gcode")
	w.Write([]byte(plateGcode))
	zw.Close()
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	if b, readErr := readZip(zr, "Metadata/plate_1.png"); readErr == nil || b != nil {
		t.Fatalf("read %d bytes, err %v", len(b), readErr)
	}
	m, png, err := parse3MF(zr, 0)
	if err != nil || m == nil || png != nil {
		t.Fatalf("model %v, %d bytes of thumbnail, err %v", m != nil, len(png), err)
	}
}

func TestTooManyMovesRejected(t *testing.T) {
	old := gcode.MaxPoints
	gcode.MaxPoints = 100
	t.Cleanup(func() { gcode.MaxPoints = old })
	var b strings.Builder
	b.WriteString("; CHANGE_LAYER\n; LAYER_HEIGHT: 0.2\n; LINE_WIDTH: 0.4\nG90\nM83\nG1 X0 Y0 Z0.2\n")
	for i := range 200 {
		fmt.Fprintf(&b, "G1 X%d Y%d E1\n", i%2*10, i) // a zigzag, which cannot be merged
	}
	if _, err := gcode.Parse(strings.NewReader(b.String())); !errors.Is(err, gcode.ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}
