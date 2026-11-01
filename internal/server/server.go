// Package server implements the HTTP API and serves the frontend.
package server

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/marektews/bblv/internal/auth"
	"github.com/marektews/bblv/internal/config"
	"github.com/marektews/bblv/internal/discovery"
	"github.com/marektews/bblv/internal/hms"
	"github.com/marektews/bblv/internal/monitor"
	"github.com/marektews/bblv/internal/printjob"
	"github.com/marektews/bblv/internal/timelapse"
	"github.com/marektews/bblv/internal/webui"
)

const (
	maxUpload     = 512 << 20
	uploadTimeout = 15 * time.Minute
)

// receiving limits the uploads handled at once, each of which takes up to
// maxUpload of temporary disk space until it has been parsed.
var receiving = make(chan struct{}, 2)

// errBusy is returned for an upload while others take all the slots.
const errBusy = "other uploads are in progress; try again when they have finished"

// sessionCheck is how often open streams check that their session is still
// valid, e.g. after the password changed.
var sessionCheck = 15 * time.Second

// streamWriteTimeout is how long one write to a stream may take. A viewer
// that vanished without closing the connection, e.g. a phone that left the
// Wi-Fi, would otherwise keep the stream, and the camera, running until TCP
// gives up, on Linux after a quarter of an hour.
var streamWriteTimeout = 15 * time.Second

// streamDeadlines returns a function setting the write deadline of the
// next write to a stream, and one clearing it when the stream ends, so that
// the end of the response is still written when the last write was long
// ago, e.g. after a camera stopped sending frames.
func streamDeadlines(w http.ResponseWriter) (set, reset func()) {
	rc := http.NewResponseController(w)
	return func() { rc.SetWriteDeadline(time.Now().Add(streamWriteTimeout)) },
		func() { rc.SetWriteDeadline(time.Time{}) }
}

type server struct {
	mon        *monitor.Monitor
	cfg        *config.Store
	auth       *auth.Auth
	disc       *discovery.Listener
	hosts      []string
	version    string
	ntfyClient *http.Client // sends test notifications
	tl         *timelapse.Library
}

// Deps are the services used by the handlers.
type Deps struct {
	Monitor   *monitor.Monitor
	Config    *config.Store
	Auth      *auth.Auth
	Discovery *discovery.Listener
	// AllowedHosts are further names the server may be reached under,
	// besides addresses and local names, e.g. that of a reverse proxy.
	AllowedHosts []string
	Version      string // of the server, sent to the pages
}

// New returns the HTTP handler of the application.
func New(d Deps) http.Handler {
	s := &server{mon: d.Monitor, cfg: d.Config, auth: d.Auth, disc: d.Discovery, version: d.Version, ntfyClient: &http.Client{}, tl: newTimelapses(d.Monitor)}
	for _, h := range d.AllowedHosts {
		s.hosts = append(s.hosts, normalizeHost(h))
	}
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
	mux.HandleFunc("PUT /api/config/ntfy", s.setNtfy)
	mux.HandleFunc("POST /api/config/ntfy/test", s.testNtfy)
	mux.HandleFunc("GET /api/printers", s.printers)
	mux.HandleFunc("GET /api/printers/{id}", s.printer)
	mux.HandleFunc("GET /api/printers/{id}/geometry", s.geometry)
	mux.HandleFunc("GET /api/printers/{id}/thumbnail", s.thumbnail)
	mux.HandleFunc("GET /api/printers/{id}/camera", s.camera)
	mux.HandleFunc("GET /api/printers/{id}/alerts", s.alerts)
	mux.HandleFunc("POST /api/printers/{id}/alerts/dismiss", s.dismissAlerts)
	mux.HandleFunc("POST /api/printers/{id}/alerts/restore", s.restoreAlerts)
	mux.HandleFunc("POST /api/printers/{id}/upload", s.upload)
	mux.HandleFunc("POST /api/printers/{id}/light", s.light)
	mux.HandleFunc("GET /api/printers/{id}/timelapses", s.timelapses)
	mux.HandleFunc("GET /api/printers/{id}/timelapses/{name}/thumbnail", s.timelapseThumbnail)
	mux.HandleFunc("GET /api/printers/{id}/timelapses/{name}/video", s.timelapseVideo)
	mux.HandleFunc("GET /api/printers/{id}/timelapses/{name}/frames", s.timelapseFrames)
	mux.HandleFunc("GET /api/printers/{id}/timelapses/{name}/frames/{n}", s.timelapseFrame)
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	mux.Handle("/", webui.Handler())
	return s.protect(mux)
}

// protect requires a session for the API when a password is set, and
// rejects changes requested by pages of other sites, requests under names
// of other sites and being shown in their frames.
func (s *server) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Content-Security-Policy", "frame-ancestors 'none'")
		h.Set("Referrer-Policy", "no-referrer")
		if !s.allowedHost(r.Host) {
			writeError(w, http.StatusMisdirectedRequest, fmt.Sprintf("unknown host name %q; to open bblv under it, start the server with -allow-host %s", r.Host, normalizeHost(r.Host)))
			return
		}
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

// localSuffixes end names that only resolve in the local network.
var localSuffixes = []string{".localhost", ".local", ".lan", ".home.arpa", ".internal"}

// allowedHost reports whether the Host header names the server by an
// address, a local name or an allowed name. A page of another site can
// reach the server only under a name its owner controls (DNS rebinding),
// which is none of them; without a password it could otherwise change the
// address of a printer and receive its access code.
func (s *server) allowedHost(host string) bool {
	h := normalizeHost(host)
	if h == "" || net.ParseIP(h) != nil || !strings.Contains(h, ".") {
		return true // no Host (HTTP/1.0), an address, localhost or a computer name
	}
	for _, suffix := range localSuffixes {
		if strings.HasSuffix(h, suffix) {
			return true
		}
	}
	return slices.Contains(s.hosts, h)
}

// normalizeHost returns the lower case name of a Host header, without the
// port, brackets and final dot.
func normalizeHost(host string) string {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.Trim(host, "[]"), ".")
	return strings.ToLower(host)
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
		code := hms.ErrorCode(e)
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
	deadline, clearDeadline := streamDeadlines(w)
	defer clearDeadline()
	check := time.NewTicker(sessionCheck)
	defer check.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-check.C:
			if !s.auth.Valid(r) { // the password changed
				return
			}
		case img, streaming := <-frames:
			if !streaming { // the printer was removed
				return
			}
			deadline()
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

// receiveUpload streams the "file" part of a multipart form straight into
// a temporary file, which the caller removes, and reads the "plate" field.
func receiveUpload(r *http.Request) (name string, plate int, err error) {
	mr, err := r.MultipartReader()
	if err != nil {
		return "", 0, err
	}
	defer func() {
		if err != nil && name != "" {
			os.Remove(name)
		}
	}()
	for {
		part, partErr := mr.NextPart()
		if errors.Is(partErr, io.EOF) {
			break
		}
		if partErr != nil {
			return name, 0, partErr
		}
		switch part.FormName() {
		case "plate":
			b, _ := io.ReadAll(io.LimitReader(part, 16))
			plate, _ = strconv.Atoi(strings.TrimSpace(string(b)))
		case "file":
			if name != "" {
				return name, 0, errors.New("more than one file")
			}
			ext := strings.ToLower(filepath.Ext(part.FileName()))
			if ext != ".gcode" && ext != ".3mf" {
				return "", 0, errors.New("expected a .gcode or .3mf file")
			}
			tmp, tmpErr := os.CreateTemp("", "bblv-upload-*"+ext)
			if tmpErr != nil {
				return "", 0, tmpErr
			}
			name = tmp.Name()
			_, err = io.Copy(tmp, part)
			if cerr := tmp.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return name, 0, fmt.Errorf("upload failed: %w", err)
			}
		}
		part.Close()
	}
	if name == "" {
		return "", 0, errors.New("missing file")
	}
	return name, plate, nil
}

// upload accepts a multipart form with a .gcode or .3mf "file" and an
// optional "plate" number and makes it the geometry of the current print.
func (s *server) upload(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := s.mon.Printer(id); !ok {
		writeError(w, http.StatusNotFound, "unknown printer")
		return
	}
	select {
	case receiving <- struct{}{}:
		defer func() { <-receiving }()
	default:
		w.Header().Set("Retry-After", "30")
		writeError(w, http.StatusTooManyRequests, errBusy)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	// A large file over a slow connection takes a while, but not forever.
	http.NewResponseController(w).SetReadDeadline(time.Now().Add(uploadTimeout))
	name, plate, err := receiveUpload(r)
	if err != nil {
		status := http.StatusBadRequest
		if tooLarge := (*http.MaxBytesError)(nil); errors.As(err, &tooLarge) {
			status = http.StatusRequestEntityTooLarge
		}
		writeError(w, status, err.Error())
		return
	}
	defer os.Remove(name)
	res, err := printjob.BuildFile(name, plate, s.mon.Store())
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
	if err = s.mon.Upload(id, res); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	info, _ := s.mon.Printer(id)
	writeJSON(w, info)
}

// events streams Server-Sent Events: on connect a "version" event with the
// server version, then a "printer" event with the printer info
// for every printer on connect and after every change, a "removed" event
// with the id of a printer that is no longer configured and a "notification"
// event with a notify.Event when a print ended or a printer needs
// attention, which the page may show. A "ready" event with
// the ids in configuration order follows the printers sent on connect, so that
// a client reconnecting drops those removed meanwhile.
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
	deadline, clearDeadline := streamDeadlines(w)
	defer clearDeadline()
	deadline() // for the whole state sent on connect
	emit := func(e monitor.Event) error {
		var data any = e.Info
		switch e.Type {
		case "removed":
			data = map[string]string{"id": e.Info.ID}
		case "notification":
			data = e.Notice
		}
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, b)
		return err
	}
	// The page reloads when the server it was loaded from has been updated.
	v, _ := json.Marshal(s.version)
	if _, err := fmt.Fprintf(w, "event: version\ndata: %s\n\n", v); err != nil {
		return
	}
	ids := []string{}
	for _, info := range s.mon.Printers() {
		if emit(monitor.Event{Type: "printer", Info: info}) != nil {
			return
		}
		ids = append(ids, info.ID)
	}
	b, _ := json.Marshal(ids)
	if _, werr := fmt.Fprintf(w, "event: ready\ndata: %s\n\n", b); werr != nil {
		return
	}
	flusher.Flush()

	ping := time.NewTicker(sessionCheck)
	defer ping.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case e, subscribed := <-updates:
			// Fell too far behind: the browser reconnects and gets every
			// printer again.
			if !subscribed {
				return
			}
			deadline()
			if emit(e) != nil {
				return
			}
		case <-ping.C:
			// Ends the stream after the password changed; the browser
			// reconnects, is refused and shows the login.
			if !s.auth.Valid(r) {
				return
			}
			deadline()
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
	} else if info, statErr := f.Stat(); statErr == nil {
		h.Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	}
	if _, err = io.Copy(out, f); err != nil {
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

// dismissAlerts hides alerts of a printer for everyone until they go away:
// {"keys": ["hms:0300_0100_0001_0007", "error:0700_8011"]}; restoreAlerts
// shows them again.
func (s *server) dismissAlerts(w http.ResponseWriter, r *http.Request) {
	s.changeAlerts(w, r, s.mon.DismissAlerts)
}

func (s *server) restoreAlerts(w http.ResponseWriter, r *http.Request) {
	s.changeAlerts(w, r, s.mon.RestoreAlerts)
}

func (s *server) changeAlerts(w http.ResponseWriter, r *http.Request, change func(id string, keys []string) error) {
	var in struct {
		Keys []string `json:"keys"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := change(r.PathValue("id"), in.Keys); err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
