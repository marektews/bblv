// Package timelapse lists the timelapse videos on a printer's SD card,
// keeps the ones watched in the cache and reads the frames of AVI videos,
// which browsers cannot play: the A1 and P1 record Motion JPEG.
package timelapse

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/jlaffaye/ftp"
)

// Dir is where printers keep the timelapses, with the thumbnails in
// Dir/thumbnail named like the videos.
const Dir = "timelapse"

// Video is a timelapse on the SD card.
type Video struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Recorded  string `json:"recorded,omitempty"` // printer's local time from the name, e.g. 2025-12-22T05:59:16
	Thumbnail bool   `json:"thumbnail"`
}

var (
	validName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*\.(avi|mp4)$`)
	nameTime  = regexp.MustCompile(`(\d{4}-\d{2}-\d{2})_(\d{2})-(\d{2})-(\d{2})`)
)

// ValidName tells whether name can be a video in Dir; it never leaves it.
func ValidName(name string) bool {
	return validName.MatchString(name)
}

// ThumbnailName returns the name of a video's thumbnail in Dir/thumbnail.
func ThumbnailName(video string) string {
	return strings.TrimSuffix(video, path.Ext(video)) + ".jpg"
}

// Lister is the part of an SD card List needs.
type Lister interface {
	List(dir string) ([]*ftp.Entry, error)
}

// List returns the videos on the card, the newest first.
func List(c Lister) ([]Video, error) {
	entries, err := c.List(Dir)
	if err != nil {
		return nil, err
	}
	thumbs := map[string]bool{}
	if list, thumbErr := c.List(Dir + "/thumbnail"); thumbErr == nil {
		for _, e := range list {
			thumbs[e.Name] = true
		}
	}
	videos := []Video{}
	for _, e := range entries {
		if e.Type != ftp.EntryTypeFile || !ValidName(e.Name) {
			continue
		}
		v := Video{Name: e.Name, Size: int64(e.Size), Thumbnail: thumbs[ThumbnailName(e.Name)]}
		if m := nameTime.FindStringSubmatch(e.Name); m != nil {
			v.Recorded = m[1] + "T" + m[2] + ":" + m[3] + ":" + m[4]
		}
		videos = append(videos, v)
	}
	// The names hold the time; without one, the name decides.
	slices.SortFunc(videos, func(a, b Video) int { return strings.Compare(b.Recorded+b.Name, a.Recorded+a.Name) })
	return videos, nil
}

// Frame is where a JPEG image lies in an AVI file.
type Frame struct {
	Off, Len int64
}

// Index describes the frames of an AVI video.
type Index struct {
	FPS    float64 `json:"fps"`
	Width  int     `json:"width"`
	Height int     `json:"height"`
	Count  int     `json:"frames"`
	Frames []Frame `json:"-"`
}

// maxFrame bounds a frame: one at 4K is a few megabytes.
const maxFrame = 32 << 20

// ReadIndex finds the frames of an AVI file of the given size. It walks the
// chunks of the movi list, so it needs no idx1 index, which the printers
// do not write, and stops at the end of a file still being recorded.
func ReadIndex(r io.ReaderAt, size int64) (*Index, error) {
	head := make([]byte, min(size, 64<<10))
	if _, err := r.ReadAt(head, 0); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if len(head) < 12 || string(head[:4]) != "RIFF" || string(head[8:12]) != "AVI " {
		return nil, errors.New("not an AVI file")
	}
	ix := &Index{FPS: 24}
	if i := bytes.Index(head, []byte("avih")); i >= 0 && i+48 <= len(head) {
		if us := binary.LittleEndian.Uint32(head[i+8:]); us > 0 {
			ix.FPS = 1e6 / float64(us)
		}
		ix.Width = int(binary.LittleEndian.Uint32(head[i+40:]))
		ix.Height = int(binary.LittleEndian.Uint32(head[i+44:]))
	}
	movi := bytes.Index(head, []byte("movi"))
	if movi < 0 {
		return nil, errors.New("AVI file without frames")
	}
	hdr := make([]byte, 8)
	for off := int64(movi + 4); off+8 <= size; {
		if _, err := r.ReadAt(hdr, off); err != nil {
			break
		}
		id, n := string(hdr[:4]), int64(binary.LittleEndian.Uint32(hdr[4:]))
		switch {
		case id == "LIST":
			off += 12 // e.g. a "rec " list of frames: its chunks follow
			continue
		case id == "idx1":
			off = size // the index ends the frames
			continue
		case (strings.HasSuffix(id, "db") || strings.HasSuffix(id, "dc")) && n > 0 && n <= maxFrame && off+8+n <= size:
			ix.Frames = append(ix.Frames, Frame{Off: off + 8, Len: n})
		}
		off += 8 + n + n&1 // chunks are padded to an even size
	}
	ix.Count = len(ix.Frames)
	if ix.Count == 0 {
		return nil, errors.New("AVI file without frames")
	}
	return ix, nil
}
