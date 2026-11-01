// Package notify tells when a print ends or a printer needs attention. It
// finds the events in consecutive states of a printer, which the server
// passes on to the browsers, and sends them to an ntfy topic.
package notify

import (
	"slices"
	"strconv"
	"strings"

	"github.com/marektews/bblv/internal/hms"
	"github.com/marektews/bblv/internal/printer"
)

// Kinds of events.
const (
	Failed = "failed" // the print failed or was cancelled
	Pause  = "pause"  // the print was paused, e.g. at the end of the filament
	Finish = "finish" // the print finished
	Alert  = "hms"    // a new HMS alert or print error
)

// Kinds lists the kinds of events, the most important first.
var Kinds = []string{Failed, Pause, Finish, Alert}

// Code is an alert of an event: an HMS code or a print error.
type Code struct {
	Kind string `json:"kind"` // hms.KindHMS or hms.KindError
	Code string `json:"code"` // e.g. 0300_0100_0001_0007 or 0700_8011
}

// Event is what happened to a printer between two of its states.
type Event struct {
	ID      int64    `json:"id"`          // tells events apart, e.g. in several tabs
	Kinds   []string `json:"kinds"`       // in the order of Kinds
	Printer string   `json:"printerId"`   // its ID
	Name    string   `json:"printerName"` // its name
	Subtask string   `json:"subtask,omitempty"`
	Codes   []Code   `json:"codes,omitempty"` // the new alerts
	Serial  string   `json:"-"`               // selects the texts of the alerts
}

func active(state string) bool {
	return state == "PREPARE" || state == "RUNNING" || state == "PAUSE"
}

// Detect returns what happened between two consecutive states of a
// printer, nil when nothing worth telling. An alert that stays is told
// once; one that goes and comes back again.
func Detect(prev, cur printer.Status) *Event {
	var kinds []string
	if cur.State != prev.State {
		switch {
		case cur.State == "FAILED" && active(prev.State):
			kinds = append(kinds, Failed)
		case cur.State == "PAUSE" && active(prev.State):
			kinds = append(kinds, Pause)
		case cur.State == "FINISH" && active(prev.State):
			kinds = append(kinds, Finish)
		}
	}
	var codes []Code
	if cur.PrintError != 0 && cur.PrintError != prev.PrintError {
		codes = append(codes, Code{Kind: hms.KindError, Code: hms.ErrorCode(cur.PrintError)})
	}
	for _, h := range cur.HMS {
		if !slices.Contains(prev.HMS, h) {
			codes = append(codes, Code{Kind: hms.KindHMS, Code: h.String()})
		}
	}
	if len(codes) > 0 {
		kinds = append(kinds, Alert)
	}
	if len(kinds) == 0 {
		return nil
	}
	return &Event{Kinds: kinds, Subtask: cur.Subtask, Codes: codes}
}

// Message is an event put into words for one recipient.
type Message struct {
	Title    string
	Body     string
	Priority int      // ntfy priority: 3 default, 4 high
	Tags     []string // ntfy tags, shown as emoji
	Click    string   // address opened by tapping the notification, "" for none
}

var titles = map[string]map[string]string{
	"pl": {Failed: "wydruk nieudany", Pause: "wydruk wstrzymany", Finish: "wydruk zakończony", Alert: "nowy alert"},
	"en": {Failed: "print failed", Pause: "print paused", Finish: "print finished", Alert: "new alert"},
}

var tags = map[string]string{Failed: "x", Pause: "pause_button", Finish: "white_check_mark", Alert: "warning"}

// Compose puts an event into words in lang (pl or en) for a recipient who
// wants the kinds in selected; false when it wants none of them. The title
// names the most important kind selected; the body the print and the new
// alerts with their descriptions.
func Compose(e Event, lang string, selected []string) (Message, bool) {
	if lang != "pl" {
		lang = "en"
	}
	i := slices.IndexFunc(e.Kinds, func(k string) bool { return slices.Contains(selected, k) })
	if i < 0 {
		return Message{}, false
	}
	kind := e.Kinds[i]
	m := Message{Title: e.Name + ": " + titles[lang][kind], Priority: 3, Tags: []string{tags[kind]}}
	if kind == Failed || kind == Pause {
		m.Priority = 4
	}
	var lines []string
	if e.Subtask != "" {
		lines = append(lines, e.Subtask)
	}
	for _, c := range e.Codes {
		label := "HMS"
		if c.Kind == hms.KindError {
			label = map[string]string{"pl": "Błąd", "en": "Error"}[lang]
		} else if s := hms.Severity(codeOf(c.Code)); s == "fatal" || s == "serious" {
			m.Priority = 4
		}
		line := label + " " + c.Code
		if text := hms.Text(lang, e.Serial, c.Kind, c.Code); text != "" {
			line += ": " + text
		}
		lines = append(lines, line)
	}
	m.Body = strings.Join(lines, "\n")
	return m, true
}

// codeOf returns the code part (the last 8 hex digits) of an HMS string.
func codeOf(s string) uint32 {
	s = strings.ReplaceAll(s, "_", "")
	if len(s) < 8 {
		return 0
	}
	v, _ := strconv.ParseUint(s[len(s)-8:], 16, 32)
	return uint32(v)
}

// TestMessage is what the test notification says.
func TestMessage(lang string) Message {
	if lang == "pl" {
		return Message{Title: "bblv: powiadomienie testowe", Body: "Powiadomienia z bblv działają.", Priority: 3, Tags: []string{"bell"}}
	}
	return Message{Title: "bblv: test notification", Body: "Notifications from bblv work.", Priority: 3, Tags: []string{"bell"}}
}
