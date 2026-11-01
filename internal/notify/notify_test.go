package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/marektews/bblv/internal/config"
	"github.com/marektews/bblv/internal/printer"
)

func TestDetect(t *testing.T) {
	running := printer.Status{State: "RUNNING", Subtask: "Most", HMS: []printer.HMS{}}
	with := func(s printer.Status, f func(*printer.Status)) printer.Status {
		f(&s)
		return s
	}
	runout := printer.HMS{Attr: 0x07008000, Code: 0x00020011}
	for _, tc := range []struct {
		name      string
		prev, cur printer.Status
		kinds     []string
		codes     int
	}{
		{"nothing", running, with(running, func(s *printer.Status) { s.Layer = 5 }), nil, 0},
		{"finished", running, with(running, func(s *printer.Status) { s.State = "FINISH" }), []string{Finish}, 0},
		{"failed", running, with(running, func(s *printer.Status) { s.State = "FAILED" }), []string{Failed}, 0},
		{"paused for filament", running, with(running, func(s *printer.Status) {
			s.State, s.PrintError, s.HMS = "PAUSE", 0x07008011, []printer.HMS{runout}
		}), []string{Pause, Alert}, 2},
		{"alert stays", with(running, func(s *printer.Status) { s.HMS = []printer.HMS{runout} }),
			with(running, func(s *printer.Status) { s.HMS = []printer.HMS{runout}; s.Layer = 6 }), nil, 0},
		{"idle printer finishes nothing", printer.Status{State: "IDLE"}, printer.Status{State: "FINISH"}, nil, 0},
		{"resumed", with(running, func(s *printer.Status) { s.State = "PAUSE" }), running, nil, 0},
	} {
		e := Detect(tc.prev, tc.cur)
		var kinds []string
		var codes int
		if e != nil {
			kinds, codes = e.Kinds, len(e.Codes)
		}
		if !slices.Equal(kinds, tc.kinds) || codes != tc.codes {
			t.Errorf("%s: kinds %v with %d codes, want %v with %d", tc.name, kinds, codes, tc.kinds, tc.codes)
		}
	}
}

func TestCompose(t *testing.T) {
	e := Event{Kinds: []string{Pause, Alert}, Name: "A1", Subtask: "Most", Serial: "03919D000000000",
		Codes: []Code{{Kind: "error", Code: "0700_8011"}}}
	m, ok := Compose(e, "pl", Kinds)
	if !ok || m.Title != "A1: wydruk wstrzymany" || m.Priority != 4 || !strings.HasPrefix(m.Body, "Most\nBłąd 0700_8011") {
		t.Fatalf("pl: %+v", m)
	}
	// Without pauses selected, the alert names the notification.
	if m, ok = Compose(e, "en", []string{Finish, Alert}); !ok || m.Title != "A1: new alert" || !strings.Contains(m.Body, "Error 0700_8011") {
		t.Fatalf("alert only: %+v", m)
	}
	if _, ok = Compose(e, "en", []string{Finish}); ok {
		t.Fatal("composed an event of kinds not selected")
	}
}

func TestPublish(t *testing.T) {
	var got map[string]any
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&got)
		if got["topic"] != "bblv-test" {
			http.Error(w, `{"error":"forbidden"}`, http.StatusForbidden)
		}
	}))
	defer srv.Close()
	c := config.Ntfy{URL: srv.URL, Topic: "bblv-test", Token: "tk_secret"}
	m := Message{Title: "A1: wydruk zakończony", Body: "Most", Priority: 3, Tags: []string{"white_check_mark"}, Click: "https://pc:8443/printer/a1"}
	if err := Publish(context.Background(), srv.Client(), c, m); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer tk_secret" || got["title"] != m.Title || got["message"] != "Most" || got["click"] != m.Click || got["priority"] != float64(3) {
		t.Fatalf("sent %v with %q", got, auth)
	}
	c.Topic = "other"
	if err := Publish(context.Background(), srv.Client(), c, m); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("refused: %v", err)
	}
}

// The sender reads its settings at every event and sends nothing while
// ntfy is off or the kind is not selected.
func TestNtfySendsSelectedEvents(t *testing.T) {
	sent := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		sent <- body["title"].(string) + " " + body["click"].(string)
	}))
	defer srv.Close()
	cfg := config.Ntfy{URL: srv.URL, Topic: "t", Events: []string{Finish}, Lang: "en", Link: "https://pc:8443"}
	n := NewNtfy(func() config.Ntfy { return cfg })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go n.Run(ctx)
	n.Send(Event{Kinds: []string{Pause}, Printer: "a1", Name: "A1"})
	n.Send(Event{Kinds: []string{Finish}, Printer: "a 1", Name: "A1"})
	if got := <-sent; got != "A1: print finished https://pc:8443/printer/a%201" {
		t.Fatalf("sent %q", got)
	}
	select {
	case got := <-sent:
		t.Fatalf("also sent %q", got)
	default:
	}
}
