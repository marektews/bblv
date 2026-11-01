package server

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/marektews/bblv/internal/auth"
	"github.com/marektews/bblv/internal/bambustudio"
	"github.com/marektews/bblv/internal/config"
	"github.com/marektews/bblv/internal/discovery"
	"github.com/marektews/bblv/internal/notify"
)

// printerView is a configured printer as shown to the browser; the access
// code itself never leaves the server.
type printerView struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Model         string `json:"model"`
	Host          string `json:"host"`
	Serial        string `json:"serial"`
	HasAccessCode bool   `json:"hasAccessCode"`
}

func viewOf(p config.Printer) printerView {
	return printerView{ID: p.ID, Name: p.Name, Model: p.Model, Host: p.Host, Serial: p.Serial, HasAccessCode: p.AccessCode != ""}
}

type printerInput struct {
	Name       string `json:"name"`
	Model      string `json:"model"`
	Host       string `json:"host"`
	Serial     string `json:"serial"`
	AccessCode string `json:"accessCode"` // empty keeps the current code when editing
}

// bodyTimeout is how long reading a JSON request body may take.
const bodyTimeout = 30 * time.Second

var errDuplicateSerial = errors.New("a printer with this serial number is already configured")

// errCodeForSerial refuses to give the access code of a printer to another
// serial number, not pinned to a CA yet, which any device can claim.
var errCodeForSerial = errors.New("enter the access code again when changing the serial number")

func (in *printerInput) normalize() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Model = strings.TrimSpace(in.Model)
	in.Host = strings.TrimSpace(in.Host)
	in.Serial = strings.ToUpper(strings.TrimSpace(in.Serial))
	in.AccessCode = strings.TrimSpace(in.AccessCode)
	if in.Host == "" || in.Serial == "" {
		return errors.New("host and serial number are required")
	}
	host, ok := validHost(in.Host)
	if !ok {
		return fmt.Errorf("%q is not an IP address or a host name; enter it without a port or http://", in.Host)
	}
	in.Host = host
	if in.Name == "" {
		in.Name = in.Serial
	}
	return nil
}

// validHost accepts an IPv4 or IPv6 address, the latter also in brackets,
// or a host name, and returns it in the form saved.
func validHost(h string) (string, bool) {
	if ip := net.ParseIP(strings.TrimSuffix(strings.TrimPrefix(h, "["), "]")); ip != nil {
		return ip.String(), true
	}
	h = strings.TrimSuffix(h, ".")
	if h == "" || len(h) > 253 {
		return "", false
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return "", false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
				return "", false
			}
		}
	}
	return h, true
}

func (s *server) getConfig(w http.ResponseWriter, r *http.Request) {
	c := s.cfg.Get()
	printers := make([]printerView, len(c.Printers))
	for i, p := range c.Printers {
		printers[i] = viewOf(p)
	}
	path := bambustudio.ConfigPath()
	_, err := os.Stat(path)
	writeJSON(w, map[string]any{
		"printers":     printers,
		"passwordSet":  c.PasswordHash != "",
		"studioConfig": map[string]any{"path": path, "found": err == nil},
		"ntfy":         ntfyViewOf(c.Ntfy),
	})
}

func (s *server) addPrinter(w http.ResponseWriter, r *http.Request) {
	var in printerInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := in.normalize(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var p config.Printer
	err := s.cfg.Update(func(c *config.Config) error {
		for _, e := range c.Printers {
			if strings.EqualFold(e.Serial, in.Serial) {
				return errDuplicateSerial
			}
		}
		p = config.Printer{ID: c.NewID(in.Name), Name: in.Name, Model: in.Model, Host: in.Host, Serial: in.Serial, AccessCode: in.AccessCode}
		c.Printers = append(c.Printers, p)
		return nil
	})
	if s.configError(w, err) {
		return
	}
	writeJSON(w, viewOf(p))
}

func (s *server) updatePrinter(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in printerInput
	if !decodeJSON(w, r, &in) {
		return
	}
	if err := in.normalize(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var p config.Printer
	err := s.cfg.Update(func(c *config.Config) error {
		target, err := c.Printer(id)
		if err != nil {
			return err
		}
		for _, e := range c.Printers {
			if e.ID != id && strings.EqualFold(e.Serial, in.Serial) {
				return errDuplicateSerial
			}
		}
		if in.AccessCode != "" {
			target.AccessCode = in.AccessCode
		} else if target.AccessCode != "" && !strings.EqualFold(target.Serial, in.Serial) {
			return errCodeForSerial
		}
		target.Name, target.Model, target.Host, target.Serial = in.Name, in.Model, in.Host, in.Serial
		p = *target
		return nil
	})
	if s.configError(w, err) {
		return
	}
	writeJSON(w, viewOf(p))
}

func (s *server) deletePrinter(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	err := s.cfg.Update(func(c *config.Config) error {
		if _, err := c.Printer(id); err != nil {
			return err
		}
		c.Printers = removeID(c.Printers, id)
		return nil
	})
	if s.configError(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func removeID(printers []config.Printer, id string) []config.Printer {
	out := printers[:0]
	for _, p := range printers {
		if p.ID != id {
			out = append(out, p)
		}
	}
	return out
}

// discovered lists printers announcing themselves in the network.
func (s *server) discovered(w http.ResponseWriter, r *http.Request) {
	configured := map[string]string{}
	for _, p := range s.cfg.Get().Printers {
		configured[strings.ToUpper(p.Serial)] = p.ID
	}
	type device struct {
		discovery.Device
		ConfiguredAs string `json:"configuredAs,omitempty"`
	}
	devices := []device{}
	if s.disc == nil {
		writeJSON(w, map[string]any{"devices": devices})
		return
	}
	for _, d := range s.disc.Devices() {
		devices = append(devices, device{Device: d, ConfiguredAs: configured[strings.ToUpper(d.Serial)]})
	}
	resp := map[string]any{"devices": devices}
	if err := s.disc.Err(); err != nil {
		resp["error"] = err.Error()
	}
	writeJSON(w, resp)
}

// importCodes sets the access codes of configured printers from a
// BambuStudio.conf uploaded as "file" or, without a file, from the copy of
// the user running the server. Uploaded files are not stored.
func (s *server) importCodes(w http.ResponseWriter, r *http.Request) {
	var data []byte
	var err error
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct == "multipart/form-data" {
		r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
		http.NewResponseController(w).SetReadDeadline(time.Now().Add(4 * bodyTimeout))
		file, _, ferr := r.FormFile("file")
		if ferr != nil {
			writeError(w, http.StatusBadRequest, "missing file: "+ferr.Error())
			return
		}
		data, err = io.ReadAll(file)
		file.Close()
	} else {
		data, err = os.ReadFile(bambustudio.ConfigPath())
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "BambuStudio.conf not found at "+bambustudio.ConfigPath())
			return
		}
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	codes, err := bambustudio.AccessCodes(data)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	var updated []config.Printer
	var missing []string
	err = s.cfg.Update(func(c *config.Config) error {
		for i := range c.Printers {
			p := &c.Printers[i]
			code := ""
			for serial, cd := range codes {
				if strings.EqualFold(serial, p.Serial) {
					code = cd
				}
			}
			switch {
			case code != "" && code != p.AccessCode:
				p.AccessCode = code
				updated = append(updated, *p)
			case code == "" && p.AccessCode == "":
				missing = append(missing, p.ID)
			}
		}
		return nil
	})
	if s.configError(w, err) {
		return
	}
	ids := []string{}
	for _, p := range updated {
		ids = append(ids, p.ID)
	}
	if missing == nil {
		missing = []string{}
	}
	writeJSON(w, map[string]any{"updated": ids, "found": len(codes), "missing": missing})
}

func (s *server) setPassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Current  string `json:"current"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	err := s.auth.SetPassword(w, r, in.Current, in.Password)
	if wait := (*auth.WaitError)(nil); errors.As(err, &wait) {
		w.Header().Set("Retry-After", strconv.Itoa(wait.Seconds()))
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	if errors.Is(err, auth.ErrWrongPassword) {
		writeError(w, http.StatusForbidden, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) authStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]bool{"required": s.auth.Required(), "authenticated": s.auth.Valid(r)})
}

func (s *server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	ok, err := s.auth.Login(w, r, in.Password)
	if wait := (*auth.WaitError)(nil); errors.As(err, &wait) {
		w.Header().Set("Retry-After", strconv.Itoa(wait.Seconds()))
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !ok {
		writeError(w, http.StatusUnauthorized, auth.ErrWrongPassword.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.auth.Logout(w, r); err != nil {
		slog.Warn("session not revoked", "err", err)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// configError writes the response for a failed configuration change.
func (s *server) configError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, errDuplicateSerial):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, errCodeForSerial), errors.Is(err, errTokenForServer):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, config.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, err.Error())
	}
	return true
}

// decodeJSON reads a JSON body. Requiring the JSON content type also keeps
// other sites from posting plain HTML forms to the API.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); ct != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "expected application/json")
		return false
	}
	// A body sent byte by byte, e.g. to a login without a session, must not
	// hold the connection for ever.
	http.NewResponseController(w).SetReadDeadline(time.Now().Add(bodyTimeout))
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

// ntfyView is the ntfy configuration as shown to the browser; like the
// access codes, the token never leaves the server.
type ntfyView struct {
	URL      string   `json:"url"`
	Topic    string   `json:"topic"`
	HasToken bool     `json:"hasToken"`
	Events   []string `json:"events"`
	Lang     string   `json:"lang"`
	Link     string   `json:"link"`
}

func ntfyViewOf(n config.Ntfy) ntfyView {
	n = notify.WithDefaults(n)
	return ntfyView{URL: n.URL, Topic: n.Topic, HasToken: n.Token != "", Events: n.Events, Lang: n.Lang, Link: n.Link}
}

type ntfyInput struct {
	URL        string   `json:"url"`
	Topic      string   `json:"topic"`
	Token      string   `json:"token"`      // empty keeps the current token
	ClearToken bool     `json:"clearToken"` // removes it
	Events     []string `json:"events"`
	Lang       string   `json:"lang"`
	Link       string   `json:"link"`
}

// errTokenForServer refuses to give the ntfy token to another server.
var errTokenForServer = errors.New("enter the token again when changing the ntfy server")

var ntfyTopic = regexp.MustCompile(`^[-_A-Za-z0-9]{1,64}$`)

// webAddress checks an http or https address without a query and returns
// it without a final slash.
func webAddress(s string) (string, bool) {
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", false
	}
	return strings.TrimSuffix(s, "/"), true
}

// settings checks the input and returns the configuration without the token.
func (in ntfyInput) settings() (config.Ntfy, error) {
	n := config.Ntfy{Topic: strings.TrimSpace(in.Topic), Lang: in.Lang, Events: []string{}}
	if n.Topic != "" && !ntfyTopic.MatchString(n.Topic) {
		return n, errors.New("the topic may only have letters, digits, - and _, at most 64")
	}
	var ok bool
	if n.URL, ok = webAddress(cmp.Or(strings.TrimSpace(in.URL), notify.DefaultNtfyURL)); !ok {
		return n, errors.New("the ntfy server must be an http or https address")
	}
	if link := strings.TrimSpace(in.Link); link != "" {
		if n.Link, ok = webAddress(link); !ok {
			return n, errors.New("the address of bblv must be an http or https address")
		}
	}
	for _, k := range notify.Kinds {
		if slices.Contains(in.Events, k) {
			n.Events = append(n.Events, k)
		}
	}
	if n.Topic != "" && len(n.Events) == 0 {
		return n, errors.New("choose at least one event")
	}
	if n.Lang != "pl" && n.Lang != "en" {
		return n, errors.New("the language must be pl or en")
	}
	return n, nil
}

func (s *server) setNtfy(w http.ResponseWriter, r *http.Request) {
	var in ntfyInput
	if !decodeJSON(w, r, &in) {
		return
	}
	n, err := in.settings()
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	err = s.cfg.Update(func(c *config.Config) error {
		old := notify.WithDefaults(c.Ntfy)
		switch {
		case strings.TrimSpace(in.Token) != "":
			n.Token = strings.TrimSpace(in.Token)
		case in.ClearToken:
		case old.Token != "" && old.URL != n.URL:
			return errTokenForServer
		default:
			n.Token = old.Token
		}
		c.Ntfy = n
		return nil
	})
	if s.configError(w, err) {
		return
	}
	writeJSON(w, ntfyViewOf(n))
}

// testNtfy sends a test notification with the saved settings.
func (s *server) testNtfy(w http.ResponseWriter, r *http.Request) {
	n := notify.WithDefaults(s.cfg.Get().Ntfy)
	if n.Topic == "" {
		writeError(w, http.StatusConflict, "ntfy is not set up")
		return
	}
	m := notify.TestMessage(n.Lang)
	m.Click = n.Link
	if err := notify.Publish(r.Context(), s.ntfyClient, n, m); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
