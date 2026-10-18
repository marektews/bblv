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
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/marektews/bblv/internal/auth"
	"github.com/marektews/bblv/internal/config"
	"github.com/marektews/bblv/internal/discovery"
	"github.com/marektews/bblv/internal/hms"
	"github.com/marektews/bblv/internal/monitor"
	"github.com/marektews/bblv/internal/printjob"
	"github.com/marektews/bblv/internal/webui"
)

const maxUpload = 512 << 20

type server struct {
	mon  *monitor.Monitor
	cfg  *config.Store
	auth *auth.Auth
	disc *discovery.Listener
}

// Deps are the services used by the handlers.
type Deps struct {
	Monitor   *monitor.Monitor
	Config    *config.Store
	Auth      *auth.Auth
	Discovery *discovery.Listener
}

// New returns the HTTP handler of the application.
func New(d Deps) http.Handler {
	s := &server{mon: d.Monitor, cfg: d.Config, auth: d.Auth, disc: d.Discovery}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/auth", s.authStatus)
	mux.HandleFunc("POST /api/login", s.login)
	mux.HandleFunc("POST /api/logout", s.logout)
	mux.HandleFunc("GET /api/config", s.getConfig)
	mux.HandleFunc("POST /api/config/printers", s.addPrinter)
	mux.HandleFunc("PUT /api/config/printers/{id}", s.updatePrinter)
	mux.HandleFunc("DELETE /api/config/printers/{id}", s.deletePrinter)
	mux.HandleFunc("GET /api/config/discovered", s.discovered)
	mux.HandleFunc("POST /api/config/import-codes", s.importCodes)
	mux.HandleFunc("PUT /api/config/password", s.setPassword)
	mux.HandleFunc("GET /api/printers", s.printers)
	mux.HandleFunc("GET /api/printers/{id}", s.printer)
	mux.HandleFunc("GET /api/printers/{id}/geometry", s.geometry)
	mux.HandleFunc("GET /api/printers/{id}/thumbnail", s.thumbnail)
	mux.HandleFunc("GET /api/printers/{id}/camera", s.camera)
	mux.HandleFunc("GET /api/printers/{id}/alerts", s.alerts)
	mux.HandleFunc("POST /api/printers/{id}/upload", s.upload)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	mux.Handle("/", webui.Handler())
	return s.protect(mux)
}

// protect requires a session for the API when a password is set, and
// rejects changes requested by pages of other sites.
func (s *server) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if origin := r.Header.Get("Origin"); origin != "" {
				if u, err := url.Parse(origin); err != nil || u.Host != r.Host {
					writeError(w, http.StatusForbidden, "cross-origin request")
					return
				}
			}
		}
		p := r.URL.Path
		if strings.HasPrefix(p, "/api/") && p != "/api/auth" && p != "/api/login" && !s.auth.Valid(r) {
			writeError(w, http.StatusUnauthorized, "login required")
			return
		}
		next.ServeHTTP(w, r)
	})
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

// alerts describes the current HMS alerts and print error of a printer in
// the language given by ?lang= (Polish or English).
func (s *server) alerts(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	info, ok := s.mon.Printer(id)
	if !ok {
		writeError(w, http.StatusNotFound, "unknown printer")
		return
	}
	lang := r.URL.Query().Get("lang")
	if lang != "pl" {
		lang = "en"
	}
	serial := s.mon.Serial(id)
	type alert struct {
		Kind     string `json:"kind"`
		Code     string `json:"code"`
		Severity string `json:"severity,omitempty"`
		Text     string `json:"text"`
		URL      string `json:"url"`
	}
	out := []alert{}
	if e := info.Status.PrintError; e != 0 {
		code := fmt.Sprintf("%08X", uint32(e))
		code = code[:4] + "_" + code[4:]
		out = append(out, alert{Kind: hms.KindError, Code: code, Text: hms.Text(lang, serial, hms.KindError, code), URL: hms.HelpURL(lang, serial, hms.KindError, code)})
	}
	for _, h := range info.Status.HMS {
		code := h.String()
		out = append(out, alert{Kind: hms.KindHMS, Code: code, Severity: hms.Severity(h.Code), Text: hms.Text(lang, serial, hms.KindHMS, code), URL: hms.HelpURL(lang, serial, hms.KindHMS, code)})
	}
	writeJSON(w, out)
}

// camera streams the printer camera as MJPEG (multipart/x-mixed-replace),
// which browsers show in a plain <img>.
func (s *server) camera(w http.ResponseWriter, r *http.Request) {
	hub := s.mon.Camera(r.PathValue("id"))
	if hub == nil {
		writeError(w, http.StatusNotFound, "no camera")
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	frames, cancel := hub.Subscribe()
	defer cancel()
	h := w.Header()
	h.Set("Content-Type", "multipart/x-mixed-replace; boundary=frame")
	h.Set("Cache-Control", "no-cache, no-store")
	h.Set("X-Accel-Buffering", "no")
	for {
		select {
		case <-r.Context().Done():
			return
		case img := <-frames:
			if _, err := fmt.Fprintf(w, "--frame\r\nContent-Type: image/jpeg\r\nContent-Length: %d\r\n\r\n", len(img)); err != nil {
				return
			}
			// img is shared by all viewers and must not be modified.
			if _, err := w.Write(img); err != nil {
				return
			}
			if _, err := io.WriteString(w, "\r\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
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

// events streams Server-Sent Events: a "printer" event with the printer info
// for every printer on connect and after every change, and a "removed" event
// with the id of a printer that is no longer configured.
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
	send := func(e monitor.Event) error {
		var data any = e.Info
		if e.Type == "removed" {
			data = map[string]string{"id": e.Info.ID}
		}
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, b)
		return err
	}
	for _, info := range s.mon.Printers() {
		if send(monitor.Event{Type: "printer", Info: info}) != nil {
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
		case e := <-updates:
			if send(e) != nil {
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
