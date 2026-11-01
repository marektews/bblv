package server

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strconv"

	"github.com/marektews/bblv/internal/monitor"
	"github.com/marektews/bblv/internal/timelapse"
)

// newTimelapses reads the timelapses through the printers' SD cards and
// keeps the ones watched in the geometry cache.
func newTimelapses(mon *monitor.Monitor) *timelapse.Library {
	open := func(ctx context.Context, id string) (timelapse.Card, string, error) {
		conn, serial, err := mon.SDCard(ctx, id)
		if err != nil {
			return nil, "", err
		}
		return conn, serial, nil
	}
	return timelapse.NewLibrary(open, mon.Store())
}

// timelapseError writes the response for a failed timelapse request.
func timelapseError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case r.Context().Err() != nil:
		// The viewer left.
	case errors.Is(err, monitor.ErrUnknownPrinter), errors.Is(err, timelapse.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, monitor.ErrNotSupported), errors.Is(err, timelapse.ErrNotReady):
		writeError(w, http.StatusConflict, err.Error())
	default:
		slog.Warn("timelapse not read", "printer", r.PathValue("id"), "err", err)
		writeError(w, http.StatusBadGateway, err.Error())
	}
}

// timelapses lists the timelapses on a printer's SD card, the newest first.
func (s *server) timelapses(w http.ResponseWriter, r *http.Request) {
	videos, err := s.tl.List(r.Context(), r.PathValue("id"))
	if err != nil {
		timelapseError(w, r, err)
		return
	}
	writeJSON(w, videos)
}

func (s *server) timelapseThumbnail(w http.ResponseWriter, r *http.Request) {
	p, err := s.tl.Thumbnail(r.Context(), r.PathValue("id"), r.PathValue("name"))
	if err != nil {
		timelapseError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	serveCached(w, r, p, "image/jpeg")
}

// timelapseVideo sends a video prepared before as recorded, with
// ?download=1 as a file to save. Ranges let an MP4 be played and seeked in
// the browser.
func (s *server) timelapseVideo(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	c, err := s.tl.Ready(r.PathValue("id"), name)
	if err != nil {
		timelapseError(w, r, err)
		return
	}
	if r.URL.Query().Get("download") == "1" {
		// ValidName allows no quotes or other characters to escape.
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	}
	contentType := "video/mp4"
	if path.Ext(name) == ".avi" {
		contentType = "video/x-msvideo"
	}
	serveCached(w, r, c.Path, contentType)
}

// timelapseFrames prepares a video: while it is being brought from the
// printer, which takes minutes for a long one, it answers 202 with how far
// it has come, {done, size}, for the page to ask again; then the frames of
// an AVI video, which the player asks for one by one, or frames 0 for
// another, which the browser plays itself.
func (s *server) timelapseFrames(w http.ResponseWriter, r *http.Request) {
	c, p, err := s.tl.Prepare(r.Context(), r.PathValue("id"), r.PathValue("name"))
	switch {
	case err != nil:
		timelapseError(w, r, err)
	case p != nil:
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]int64{"done": p.Done.Load(), "size": p.Size})
	case c.Index == nil:
		writeJSON(w, timelapse.Index{})
	default:
		writeJSON(w, c.Index)
	}
}

func (s *server) timelapseFrame(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid frame number")
		return
	}
	b, err := s.tl.Frame(r.PathValue("id"), r.PathValue("name"), n)
	if err != nil {
		timelapseError(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("Cache-Control", "private, max-age=3600")
	h.Set("Content-Length", strconv.Itoa(len(b)))
	w.Write(b)
}

// serveCached sends a file of the cache with ranges and Last-Modified.
func serveCached(w http.ResponseWriter, r *http.Request, p, contentType string) {
	f, err := os.Open(p)
	if err != nil {
		writeError(w, http.StatusNotFound, "evicted from cache")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", contentType)
	http.ServeContent(w, r, "", info.ModTime(), f)
}
