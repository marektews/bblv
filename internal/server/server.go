// Package server implements the HTTP API and serves the frontend.
package server

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/marektews/bblv/internal/monitor"
	"github.com/marektews/bblv/internal/printjob"
	"github.com/marektews/bblv/internal/webui"
)

const maxUpload = 512 << 20

type server struct {
	mon *monitor.Monitor
}

// New returns the HTTP handler of the application.
func New(mon *monitor.Monitor) http.Handler {
	s := &server{mon: mon}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/printers", s.printers)
	mux.HandleFunc("GET /api/printers/{id}", s.printer)
	mux.HandleFunc("GET /api/printers/{id}/geometry", s.geometry)
	mux.HandleFunc("GET /api/printers/{id}/thumbnail", s.thumbnail)
	mux.HandleFunc("POST /api/printers/{id}/upload", s.upload)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	mux.Handle("/", webui.Handler())
	return mux
}

func (s *server) printers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.mon.Printers())
}

func (s *server) printer(w http.ResponseWriter, r *http.Request) {
	info, ok := s.mon.Printer(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "unknown printer")
		return
	}
	writeJSON(w, info)
}

func (s *server) geometry(w http.ResponseWriter, r *http.Request) {
	geometry, _, key := s.mon.Files(r.PathValue("id"))
	if geometry == "" {
		writeError(w, http.StatusNotFound, "no geometry")
		return
	}
	serveFile(w, r, geometry, key, "application/octet-stream", true)
}

func (s *server) thumbnail(w http.ResponseWriter, r *http.Request) {
	_, thumbnail, key := s.mon.Files(r.PathValue("id"))
	if thumbnail == "" {
		writeError(w, http.StatusNotFound, "no thumbnail")
		return
	}
	serveFile(w, r, thumbnail, key+"-png", "image/png", false)
}

// upload accepts a multipart form with a .gcode or .3mf "file" and an
// optional "plate" number and makes it the geometry of the current print.
func (s *server) upload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.mon.Printer(id); !ok {
		writeError(w, http.StatusNotFound, "unknown printer")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "missing file: "+err.Error())
		return
	}
	defer file.Close()
	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext != ".gcode" && ext != ".3mf" {
		writeError(w, http.StatusBadRequest, "expected a .gcode or .3mf file")
		return
	}
	plate, _ := strconv.Atoi(r.FormValue("plate"))

	tmp, err := os.CreateTemp("", "bblv-upload-*"+ext)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer os.Remove(tmp.Name())
	_, err = io.Copy(tmp, file)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "upload failed: "+err.Error())
		return
	}
	res, err := printjob.BuildFile(tmp.Name(), plate, s.mon.Store())
	if pe := (*printjob.PlateError)(nil); errors.As(err, &pe) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]any{"error": err.Error(), "code": "plate_required", "plates": pe.Plates})
		return
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if err := s.mon.Upload(id, res); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	info, _ := s.mon.Printer(id)
	writeJSON(w, info)
}

// events streams Server-Sent Events: one "printer" event with the printer
// info for every printer on connect and after every change.
func (s *server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("X-Accel-Buffering", "no")

	updates, cancel := s.mon.Subscribe()
	defer cancel()
	send := func(info monitor.Info) error {
		b, err := json.Marshal(info)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "event: printer\ndata: %s\n\n", b)
		return err
	}
	for _, info := range s.mon.Printers() {
		if send(info) != nil {
			return
		}
	}
	flusher.Flush()

	ping := time.NewTicker(15 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case info := <-updates:
			if send(info) != nil {
				return
			}
		case <-ping.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
		}
		flusher.Flush()
	}
}

// serveFile sends a cached file with an ETag, gzip-compressing it when asked
// and the client accepts it.
func serveFile(w http.ResponseWriter, r *http.Request, path, etag, contentType string, compress bool) {
	tag := `"` + etag + `"`
	h := w.Header()
	h.Set("ETag", tag)
	h.Set("Cache-Control", "no-cache")
	if compress {
		h.Set("Vary", "Accept-Encoding")
	}
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "evicted from cache")
		return
	} else if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer f.Close()
	h.Set("Content-Type", contentType)
	var out io.Writer = w
	if compress && strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		h.Set("Content-Encoding", "gzip")
		gz, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
		defer gz.Close()
		out = gz
	} else if info, err := f.Stat(); err == nil {
		h.Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	}
	if _, err := io.Copy(out, f); err != nil {
		slog.Debug("send file", "path", path, "err", err)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
