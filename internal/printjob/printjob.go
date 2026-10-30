// Package printjob finds the files of a print on the printer's SD card and
// turns them into cached geometry and thumbnails.
package printjob

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jlaffaye/ftp"

	"github.com/marektews/bblv/internal/cache"
	"github.com/marektews/bblv/internal/ftps"
	"github.com/marektews/bblv/internal/gcode"
)

// ErrNotFound means the SD card holds no files for the print.
var ErrNotFound = errors.New("print files not found on the SD card")

// PlateError means a .3mf holds several sliced plates and one must be chosen.
type PlateError struct {
	Plates []int
}

func (e *PlateError) Error() string {
	return fmt.Sprintf("3mf has %d sliced plates, choose one of %v", len(e.Plates), e.Plates)
}

// Files are the SD card files of one print. For prints sent from Bambu Studio
// or the cloud the printer keeps in cache/:
//
//	N_<name>.bbl          JSON with the .3mf path and AMS mapping; N is the plate
//	<name>.3mf            project with Metadata/plate_N.gcode and plate_N.png
//	<name>_plate_N.gcode  the plate G-code
//
// The X2D and P2S keep only <name>.gcode.3mf there and report the G-code
// file as the path inside the printer, /data/Metadata/plate_N.gcode.
type Files struct {
	Plate      int    // 0 when unknown
	ThreeMF    string // relative path or ""
	Gcode      string // relative path or ""
	AMSMapping []int  // filament index -> AMS slot, -1 for none
	stamp      string // sizes and times of the files, part of the cache key
}

type bbl struct {
	FilePath   string `json:"file path"`
	AMSMapping []int  `json:"ams mapping"`
}

// sdCard is the part of ftps.Conn that Find needs.
type sdCard interface {
	List(dir string) ([]*ftp.Entry, error)
	Open(name string) (io.ReadCloser, error)
}

// Find locates the files of the print named subtask (subtask_name in the
// reports); gcodeFile (gcode_file) is used when there is no .bbl file.
func Find(c sdCard, subtask, gcodeFile string) (*Files, error) {
	entries, err := c.List("cache")
	if err != nil {
		return nil, err
	}
	// The A1 may report the subtask as "<name>.3mf", and its gcode_file is
	// empty once the print has finished.
	name := strings.TrimSuffix(subtask, ".3mf")
	byName := map[string]*ftp.Entry{}
	var meta *ftp.Entry
	f := &Files{}
	for _, e := range entries {
		byName[e.Name] = e
		num, rest, ok := strings.Cut(e.Name, "_")
		if !ok || rest != name+".bbl" {
			continue
		}
		if n, convErr := strconv.Atoi(num); convErr == nil && (meta == nil || e.Time.After(meta.Time)) {
			meta, f.Plate = e, n
		}
	}

	base := name
	if meta != nil {
		var b bbl
		if data, readErr := readAll(c, "cache/"+meta.Name, 1<<20); readErr == nil && json.Unmarshal(data, &b) == nil {
			f.AMSMapping = b.AMSMapping
			if b.FilePath != "" {
				f.ThreeMF = ftps.Path(b.FilePath)
				base = strings.TrimSuffix(path.Base(b.FilePath), ".3mf")
			}
		}
	}
	if f.Plate == 0 {
		// No .bbl: take the newest <name>_plate_N.gcode.
		var newest *ftp.Entry
		for _, e := range entries {
			if n, ok := plateOf(e.Name, base+"_plate_"); ok && (newest == nil || e.Time.After(newest.Time)) {
				newest, f.Plate = e, n
			}
		}
	}
	if f.Plate > 0 {
		if e, ok := byName[fmt.Sprintf("%s_plate_%d.gcode", base, f.Plate)]; ok {
			f.Gcode = "cache/" + e.Name
		}
	}
	if f.ThreeMF == "" && strings.HasSuffix(gcodeFile, ".3mf") {
		if _, ok := byName[gcodeFile]; ok {
			f.ThreeMF = "cache/" + gcodeFile
		} else {
			f.ThreeMF = ftps.Path(gcodeFile)
		}
	}
	if f.ThreeMF == "" {
		if e, found := byName[subtask+".gcode.3mf"]; found {
			f.ThreeMF = "cache/" + e.Name
			if n, ok := plateOf(path.Base(gcodeFile), "plate_"); ok && f.Plate == 0 {
				f.Plate = n
			}
		}
	}
	if f.ThreeMF == "" && f.Gcode == "" {
		return nil, ErrNotFound
	}
	for _, p := range []string{f.ThreeMF, f.Gcode} {
		if e := entryOf(c, byName, p); e != nil {
			f.stamp += fmt.Sprintf("%s:%d:%d;", e.Name, e.Size, e.Time.Unix())
		}
	}
	return f, nil
}

// entryOf returns the listing entry of the file at p, from the listing of
// cache/ or, for a file elsewhere, its own directory; nil when missing.
func entryOf(c sdCard, inCache map[string]*ftp.Entry, p string) *ftp.Entry {
	if p == "" {
		return nil
	}
	dir, name := path.Split(p)
	if dir == "cache/" {
		return inCache[name]
	}
	entries, err := c.List(strings.TrimSuffix(dir, "/"))
	if err != nil {
		return nil
	}
	for _, e := range entries {
		if e.Name == name {
			return e
		}
	}
	return nil
}

// plateOf parses N from "<prefix>N.gcode".
func plateOf(name, prefix string) (int, bool) {
	rest, ok := strings.CutPrefix(name, prefix)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSuffix(rest, ".gcode"))
	return n, err == nil && strings.HasSuffix(rest, ".gcode")
}

// Result points to the cached files of a print.
type Result struct {
	Key       string          // cache key; changes whenever the geometry changes
	Geometry  string          // encoded geometry
	Thumbnail string          // plate thumbnail (PNG), "" when unavailable
	Model     *gcode.Model    // nil when the geometry came from the cache
	Usage     *Usage          // nil when the files do not tell
	Settings  *gcode.Settings // slicer profiles, nil when the G-code does not name them
}

// Usage is the filament the slicer planned for the print.
type Usage struct {
	Grams     float64             `json:"grams"`
	Meters    float64             `json:"meters"`
	Filaments []gcode.FilamentUse `json:"filaments,omitempty"` // only from a .3mf
}

func usageOf(m *gcode.Model) *Usage {
	u := &Usage{Grams: m.UsedG, Meters: m.UsedM, Filaments: m.Usage}
	if len(m.Usage) > 0 {
		u.Grams, u.Meters = 0, 0
		for _, f := range m.Usage {
			u.Grams += f.Grams
			u.Meters += f.Meters
		}
	}
	if u.Grams == 0 && u.Meters == 0 {
		return nil
	}
	return u
}

func settingsOf(m *gcode.Model) *gcode.Settings {
	s := m.Settings
	if s.PrintProfile == "" && s.PrinterProfile == "" && len(s.FilamentProfiles) == 0 {
		return nil
	}
	return &s
}

// details is what the cache keeps beside the geometry, as JSON.
type details struct {
	Version  int             `json:"version"`
	Usage    *Usage          `json:"usage,omitempty"`
	Settings *gcode.Settings `json:"settings,omitempty"`
}

// detailsVersion changes when the details gain something; entries cached
// by older versions are parsed again.
const detailsVersion = 2

// Build returns cached geometry for the print, creating it when needed. The
// .3mf is preferred because it is compressed and also holds the thumbnail;
// the plain G-code is the fallback.
func Build(c *ftps.Conn, f *Files, store *cache.Cache) (*Result, error) {
	key := cache.Key(f.ThreeMF, f.Gcode, strconv.Itoa(f.Plate), f.stamp)
	if r, ok := cached(store, key); ok {
		return r, nil
	}

	var model *gcode.Model
	var png []byte
	var errs []error
	if f.ThreeMF != "" {
		m, img, err := from3MF(c, f.ThreeMF, f.Plate)
		model, png = m, img
		if err != nil {
			errs = append(errs, err)
		}
	}
	if model == nil && f.Gcode != "" {
		rc, err := c.Open(f.Gcode)
		if err == nil {
			model, err = gcode.Parse(rc)
			rc.Close()
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", f.Gcode, err))
		}
	}
	if model == nil {
		return nil, errors.Join(errs...)
	}
	return save(store, key, model, png)
}

// BuildFile returns cached geometry for a local (e.g. uploaded) .gcode or
// .3mf file, creating it when needed.
func BuildFile(name string, plate int, store *cache.Cache) (*Result, error) {
	f, err := os.Open(name)
	if err != nil {
		return nil, err
	}
	h := sha256.New()
	_, err = io.Copy(h, f)
	f.Close()
	if err != nil {
		return nil, err
	}
	key := cache.Key("file", hex.EncodeToString(h.Sum(nil)), strconv.Itoa(plate))
	if r, ok := cached(store, key); ok {
		return r, nil
	}
	model, png, err := ParseFile(name, plate)
	if err != nil {
		return nil, err
	}
	return save(store, key, model, png)
}

// cached returns a print parsed before. Entries cached by older versions,
// without the current details, are parsed again.
func cached(store *cache.Cache, key string) (*Result, bool) {
	p, ok := store.Get(key, ".bin")
	if !ok {
		return nil, false
	}
	name, ok := store.Get(key, ".json")
	if !ok {
		return nil, false
	}
	b, err := os.ReadFile(name)
	var d details
	if err != nil || json.Unmarshal(b, &d) != nil || d.Version != detailsVersion {
		return nil, false
	}
	r := &Result{Key: key, Geometry: p, Usage: d.Usage, Settings: d.Settings}
	r.Thumbnail, _ = store.Get(key, ".png")
	return r, true
}

func save(store *cache.Cache, key string, model *gcode.Model, png []byte) (*Result, error) {
	r := &Result{Key: key, Model: model, Usage: usageOf(model), Settings: settingsOf(model)}
	var err error
	if r.Geometry, err = store.Put(key, ".bin", model.Encode); err != nil {
		return nil, err
	}
	d := details{Version: detailsVersion, Usage: r.Usage, Settings: r.Settings}
	if _, err = store.Put(key, ".json", func(w io.Writer) error { return json.NewEncoder(w).Encode(d) }); err != nil {
		return nil, err
	}
	if png != nil {
		r.Thumbnail, err = store.Put(key, ".png", func(w io.Writer) error {
			_, werr := w.Write(png)
			return werr
		})
		if err != nil {
			return nil, err
		}
	}
	return r, nil
}

// from3MF downloads a .3mf and returns its plate thumbnail and, when the
// archive contains it, the parsed plate G-code.
func from3MF(c *ftps.Conn, p string, plate int) (*gcode.Model, []byte, error) {
	tmp, err := os.CreateTemp("", "bblv-*.3mf")
	if err != nil {
		return nil, nil, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	rc, err := c.Open(p)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", p, err)
	}
	size, err := io.Copy(tmp, rc)
	if cerr := rc.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", p, err)
	}
	zr, err := zip.NewReader(tmp, size)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", p, err)
	}
	return parse3MF(zr, plate)
}

// ParseFile parses a local .gcode or .3mf file. For a .3mf, plate selects the
// plate (0 = the only one) and its thumbnail is returned as well.
func ParseFile(name string, plate int) (*gcode.Model, []byte, error) {
	if strings.EqualFold(filepath.Ext(name), ".3mf") {
		zr, err := zip.OpenReader(name)
		if err != nil {
			return nil, nil, err
		}
		defer zr.Close()
		return parse3MF(&zr.Reader, plate)
	}
	f, err := os.Open(name)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	m, err := gcode.Parse(f)
	return m, nil, err
}

// parse3MF returns the plate thumbnail (nil if missing) and the parsed plate
// G-code; the model is nil with an error when the archive has no G-code.
func parse3MF(zr *zip.Reader, plate int) (*gcode.Model, []byte, error) {
	if plate == 0 {
		var plates []int
		for _, zf := range zr.File {
			if n, ok := plateOf(zf.Name, "Metadata/plate_"); ok {
				plates = append(plates, n)
			}
		}
		switch len(plates) {
		case 0:
			return nil, nil, errors.New("3mf has no sliced G-code; in Bambu Studio export it with File > Export > Export plate sliced file")
		case 1:
		default:
			sort.Ints(plates)
			return nil, nil, &PlateError{Plates: plates}
		}
		plate = plates[0]
	}
	png, _ := readZip(zr, fmt.Sprintf("Metadata/plate_%d.png", plate))
	zf, err := zr.Open(fmt.Sprintf("Metadata/plate_%d.gcode", plate))
	if err != nil {
		return nil, png, fmt.Errorf("3mf has no G-code for plate %d", plate)
	}
	defer zf.Close()
	m, err := gcode.Parse(zf)
	if err != nil {
		return nil, png, err
	}
	if info, zerr := readZip(zr, "Metadata/slice_info.config"); zerr == nil {
		m.Usage = plateUsage(info, plate)
	}
	return m, png, nil
}

// sliceInfo is the part of Metadata/slice_info.config that lists, for every
// sliced plate, the filaments it uses.
type sliceInfo struct {
	Plates []struct {
		Metadata []struct {
			Key   string `xml:"key,attr"`
			Value string `xml:"value,attr"`
		} `xml:"metadata"`
		Filaments []struct {
			ID    int     `xml:"id,attr"`
			Type  string  `xml:"type,attr"`
			Color string  `xml:"color,attr"`
			UsedM float64 `xml:"used_m,attr"`
			UsedG float64 `xml:"used_g,attr"`
		} `xml:"filament"`
	} `xml:"plate"`
}

func plateUsage(data []byte, plate int) []gcode.FilamentUse {
	var info sliceInfo
	if xml.Unmarshal(data, &info) != nil {
		return nil
	}
	for _, p := range info.Plates {
		for _, md := range p.Metadata {
			if md.Key != "index" || md.Value != strconv.Itoa(plate) {
				continue
			}
			var out []gcode.FilamentUse
			for _, f := range p.Filaments {
				out = append(out, gcode.FilamentUse{Filament: f.ID, Type: f.Type, Color: f.Color, Grams: f.UsedG, Meters: f.UsedM})
			}
			return out
		}
	}
	return nil
}

func readZip(zr *zip.Reader, name string) ([]byte, error) {
	f, err := zr.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(f)
}

func readAll(c sdCard, p string, limit int64) ([]byte, error) {
	rc, err := c.Open(p)
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(rc, limit))
	if cerr := rc.Close(); err == nil {
		err = cerr
	}
	return b, err
}
