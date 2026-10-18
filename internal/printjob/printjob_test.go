package printjob

import (
	"archive/zip"
	"bytes"
	"testing"
)

const plateGcode = "; CHANGE_LAYER\n; LAYER_HEIGHT: 0.2\n; LINE_WIDTH: 0.4\nG90\nM83\nG1 X0 Y0 Z0.2\nG1 X10 E1\n"

func make3MF(t *testing.T, files map[string]string) *zip.Reader {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

func TestParse3MFPicksTheOnlyPlate(t *testing.T) {
	zr := make3MF(t, map[string]string{
		"Metadata/plate_2.gcode":     plateGcode,
		"Metadata/plate_2.gcode.md5": "x",
		"Metadata/plate_2.png":       "png",
		"Metadata/plate_1.png":       "other",
	})
	m, png, err := parse3MF(zr, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Points) != 2 || string(png) != "png" {
		t.Fatalf("points=%d png=%q", len(m.Points), png)
	}
}

func TestParse3MFNeedsPlateWhenAmbiguous(t *testing.T) {
	zr := make3MF(t, map[string]string{
		"Metadata/plate_1.gcode": plateGcode,
		"Metadata/plate_2.gcode": plateGcode,
	})
	if _, _, err := parse3MF(zr, 0); err == nil {
		t.Fatal("expected an error for two plates")
	}
	if m, _, err := parse3MF(zr, 2); err != nil || len(m.Points) != 2 {
		t.Fatalf("plate 2: %v", err)
	}
}

func TestPlateOf(t *testing.T) {
	for name, want := range map[string]int{
		"Most_plate_1.gcode":         1,
		"Most_plate_12.gcode":        12,
		"Most_plate_1.gcode.md5":     0,
		"Other_plate_1.gcode":        0,
		"Most_plate_2_plate_2.gcode": 0,
	} {
		if got, _ := plateOf(name, "Most_plate_"); got != want {
			t.Errorf("plateOf(%q) = %d, want %d", name, got, want)
		}
	}
}
