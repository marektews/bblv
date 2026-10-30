package printjob

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jlaffaye/ftp"

	"github.com/marektews/bblv/internal/cache"
	"github.com/marektews/bblv/internal/gcode"
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

// fakeCard is an SD card; files named without a directory are in cache/.
type fakeCard map[string]string

func (c fakeCard) List(dir string) ([]*ftp.Entry, error) {
	var out []*ftp.Entry
	for name, body := range c {
		d, base := "cache", name
		if i := strings.LastIndexByte(name, '/'); i >= 0 {
			d, base = name[:i], name[i+1:]
		}
		if d == dir {
			out = append(out, &ftp.Entry{Name: base, Size: uint64(len(body)), Time: time.Unix(1700000000, 0)})
		}
	}
	return out, nil
}

func (c fakeCard) Open(p string) (io.ReadCloser, error) {
	body, ok := c[strings.TrimPrefix(p, "cache/")]
	if !ok {
		return nil, errors.New("not found")
	}
	return io.NopCloser(strings.NewReader(body)), nil
}

func TestFind(t *testing.T) {
	for _, tc := range []struct {
		name               string
		card               fakeCard
		subtask, gcodeFile string
		want               Files
	}{
		{"bbl", fakeCard{
			"2_Most.bbl":         `{"file path": "/sdcard/cache/Most.3mf", "ams mapping": [1, -1]}`,
			"Most.3mf":           "3mf",
			"Most_plate_2.gcode": "gcode",
		}, "Most", "", Files{Plate: 2, ThreeMF: "cache/Most.3mf", Gcode: "cache/Most_plate_2.gcode", AMSMapping: []int{1, -1}}},
		{"A1 from the cloud", fakeCard{"Rekaw.3mf": "3mf"},
			"Rekaw.3mf", "Rekaw.3mf", Files{ThreeMF: "cache/Rekaw.3mf"}},
		{"A1 finished", fakeCard{
			"1_Rekaw.bbl":         `{"file path": "/sdcard/cache/Rekaw.3mf"}`,
			"Rekaw.3mf":           "3mf",
			"Rekaw_plate_1.gcode": "gcode",
		}, "Rekaw.3mf", "", Files{Plate: 1, ThreeMF: "cache/Rekaw.3mf", Gcode: "cache/Rekaw_plate_1.gcode"}},
		{"X2D and P2S", fakeCard{"Zaslepka.gcode.3mf": "3mf", "Other.gcode.3mf": "3mf"},
			"Zaslepka", "/data/Metadata/plate_3.gcode", Files{Plate: 3, ThreeMF: "cache/Zaslepka.gcode.3mf"}},
	} {
		got, err := Find(tc.card, tc.subtask, tc.gcodeFile)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		got.stamp = ""
		if !reflect.DeepEqual(*got, tc.want) {
			t.Errorf("%s: got %+v, want %+v", tc.name, *got, tc.want)
		}
	}
	if _, err := Find(fakeCard{"Other.gcode.3mf": "3mf"}, "Zaslepka", "/data/Metadata/plate_1.gcode"); !errors.Is(err, ErrNotFound) {
		t.Errorf("missing print: err = %v", err)
	}
}

// The cache key changes with the file also outside cache/, e.g. after the
// model is sliced again and saved under the same name.
func TestFindStampsFilesOutsideTheCache(t *testing.T) {
	card := fakeCard{"models/Most.3mf": "3mf", "Most.3mf": "an unrelated file in cache/"}
	before, err := Find(card, "Most", "/sdcard/models/Most.3mf")
	if err != nil {
		t.Fatal(err)
	}
	if before.ThreeMF != "models/Most.3mf" || before.stamp != "Most.3mf:3:1700000000;" {
		t.Fatalf("files = %+v", *before)
	}
	card["models/Most.3mf"] = "sliced again"
	after, err := Find(card, "Most", "/sdcard/models/Most.3mf")
	if err != nil {
		t.Fatal(err)
	}
	if after.stamp == before.stamp {
		t.Fatalf("stamp %q did not change with the file", after.stamp)
	}
}

const sliceInfoXML = `<?xml version="1.0" encoding="UTF-8"?>
<config>
  <plate>
    <metadata key="index" value="1"/>
    <filament id="1" type="PLA" color="#FFFFFF" used_m="1.5" used_g="4.5"/>
  </plate>
  <plate>
    <metadata key="index" value="2"/>
    <filament id="1" type="PLA" color="#FFFFFF" used_m="2" used_g="6"/>
    <filament id="3" type="PETG" color="#161616" used_m="10.25" used_g="30.5"/>
  </plate>
</config>`

func TestParse3MFReadsThePlateUsage(t *testing.T) {
	zr := make3MF(t, map[string]string{
		"Metadata/plate_2.gcode":     plateGcode,
		"Metadata/slice_info.config": sliceInfoXML,
	})
	m, _, err := parse3MF(zr, 2)
	if err != nil {
		t.Fatal(err)
	}
	want := []gcode.FilamentUse{
		{Filament: 1, Type: "PLA", Color: "#FFFFFF", Grams: 6, Meters: 2},
		{Filament: 3, Type: "PETG", Color: "#161616", Grams: 30.5, Meters: 10.25},
	}
	if !reflect.DeepEqual(m.Usage, want) {
		t.Fatalf("usage = %+v", m.Usage)
	}
	if u := usageOf(m); u.Grams != 36.5 || u.Meters != 12.25 {
		t.Fatalf("totals = %+v", u)
	}
}

func TestCachedUsage(t *testing.T) {
	store, err := cache.Open(t.TempDir(), cache.DefaultLimit)
	if err != nil {
		t.Fatal(err)
	}
	m := &gcode.Model{UsedG: 235.3, UsedM: 77.03, Settings: gcode.Settings{PrintProfile: "0.20mm Standard @BBL A1"}}
	if _, err = save(store, "k", m, nil); err != nil {
		t.Fatal(err)
	}
	r, ok := cached(store, "k")
	if !ok || r.Usage == nil || r.Usage.Grams != 235.3 || r.Usage.Meters != 77.03 {
		t.Fatalf("cached = %+v, %v", r, ok)
	}
	if r.Settings == nil || r.Settings.PrintProfile != "0.20mm Standard @BBL A1" {
		t.Fatalf("cached settings = %+v", r.Settings)
	}
	// Entries of older versions, without details or with only the usage, are parsed again.
	if _, err = store.Put("old", ".bin", m.Encode); err != nil {
		t.Fatal(err)
	}
	if _, ok = cached(store, "old"); ok {
		t.Fatal("entry without details counted as cached")
	}
	if _, err = store.Put("old", ".json", func(w io.Writer) error { _, werr := io.WriteString(w, `{"grams":235.3}`); return werr }); err != nil {
		t.Fatal(err)
	}
	if _, ok = cached(store, "old"); ok {
		t.Fatal("entry with only the usage counted as cached")
	}
}
