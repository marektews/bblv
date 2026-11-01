package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/marektews/bblv/internal/config"
)

// DefaultNtfyURL is the public ntfy server.
const DefaultNtfyURL = "https://ntfy.sh"

// WithDefaults fills in what the ntfy settings leave out: the public
// server, every kind of event and English.
func WithDefaults(n config.Ntfy) config.Ntfy {
	if n.URL == "" {
		n.URL = DefaultNtfyURL
	}
	if n.Events == nil {
		n.Events = Kinds
	}
	if n.Lang == "" {
		n.Lang = "en"
	}
	return n
}

const (
	sendTimeout = 10 * time.Second
	queueSize   = 32
)

// Ntfy sends events to the ntfy topic of the configuration, one at a time,
// so that a slow or unreachable server never holds up the printers.
type Ntfy struct {
	cfg    func() config.Ntfy
	client *http.Client
	queue  chan Event
}

// NewNtfy returns a sender reading its settings from cfg at every event.
func NewNtfy(cfg func() config.Ntfy) *Ntfy {
	return &Ntfy{cfg: cfg, client: &http.Client{Timeout: sendTimeout}, queue: make(chan Event, queueSize)}
}

// Send queues an event; it is dropped when too many wait already.
func (n *Ntfy) Send(e Event) {
	select {
	case n.queue <- e:
	default:
		slog.Warn("ntfy notification dropped, too many waiting", "printer", e.Printer)
	}
}

// Run sends the queued events until ctx ends.
func (n *Ntfy) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case e := <-n.queue:
			c := WithDefaults(n.cfg())
			if c.Topic == "" {
				continue
			}
			m, ok := Compose(e, c.Lang, c.Events)
			if !ok {
				continue
			}
			if c.Link != "" {
				m.Click = c.Link + "/printer/" + url.PathEscape(e.Printer)
			}
			if err := Publish(ctx, n.client, c, m); err != nil {
				slog.Warn("ntfy notification not sent", "printer", e.Printer, "err", err)
			}
		}
	}
}

// truncate shortens s to at most n runes.
func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// Publish sends one message to the ntfy topic of c.
func Publish(ctx context.Context, client *http.Client, c config.Ntfy, m Message) error {
	body, err := json.Marshal(map[string]any{
		"topic": c.Topic, "title": m.Title, "message": m.Body, "priority": m.Priority, "tags": m.Tags, "click": m.Click,
	})
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	// Publishing as JSON to the root of the server keeps the Polish
	// letters of the title, which headers could not carry.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(c.URL, "/")+"/", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		// Only the error ntfy explains in its JSON answer, shortened: the
		// address may be anything, whose answers must not be passed on.
		status := fmt.Sprintf("ntfy answered %d %s", resp.StatusCode, http.StatusText(resp.StatusCode))
		var answer struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 4<<10)).Decode(&answer) == nil && answer.Error != "" {
			return fmt.Errorf("%s: %s", status, truncate(answer.Error, 120))
		}
		return errors.New(status)
	}
	return nil
}
