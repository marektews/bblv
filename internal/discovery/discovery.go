// Package discovery finds Bambu Lab printers in the local network. Printers
// announce themselves about every 10 s with SSDP NOTIFY messages to UDP port
// 2021; the socket is shared with Bambu Studio, which listens on the same port.
package discovery

import (
	"bufio"
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/textproto"
	"sort"
	"strings"
	"sync"
	"time"
)

const port = ":2021"

// Device is a printer seen in the network.
type Device struct {
	Serial   string    `json:"serial"`
	Host     string    `json:"host"`
	Model    string    `json:"model"`     // product name, e.g. "A1", or the code when unknown
	Code     string    `json:"modelCode"` // e.g. "N2S"
	Name     string    `json:"name"`
	Connect  string    `json:"connect"` // "cloud" or "lan"
	Firmware string    `json:"firmware"`
	LastSeen time.Time `json:"lastSeen"`
}

// Model codes announced in DevModel.bambu.com, the model_id of the machine
// profiles in Bambu Studio (resources/profiles/BBL/machine).
var models = map[string]string{
	"N2S":     "A1",
	"N1":      "A1 mini",
	"N9":      "A2L",
	"C11":     "P1P",
	"C12":     "P1S",
	"N7":      "P2S",
	"BL-P001": "X1C",
	"BL-P002": "X1",
	"C13":     "X1E",
	"N6":      "X2D",
	"O1D":     "H2D",
	"O1E":     "H2D Pro",
	"O1S":     "H2S",
	"O1C2":    "H2C",
}

// ModelName returns the product name for a model code.
func ModelName(code string) string {
	if m, ok := models[code]; ok {
		return m
	}
	return code
}

// Listener collects announcements in the background.
type Listener struct {
	mu      sync.Mutex
	devices map[string]Device
	err     error
}

// Listen starts listening until ctx is cancelled.
func Listen(ctx context.Context) *Listener {
	l := &Listener{devices: map[string]Device{}}
	lc := net.ListenConfig{Control: reuseAddr}
	pc, err := lc.ListenPacket(ctx, "udp4", port)
	if err != nil {
		slog.Warn("printer discovery unavailable", "err", err)
		l.err = err
		return l
	}
	context.AfterFunc(ctx, func() { pc.Close() })
	go func() {
		buf := make([]byte, 4096)
		for {
			n, _, readErr := pc.ReadFrom(buf)
			if readErr != nil {
				return
			}
			if d, ok := Parse(buf[:n]); ok {
				d.LastSeen = time.Now()
				l.mu.Lock()
				l.devices[d.Serial] = d
				l.mu.Unlock()
			}
		}
	}()
	return l
}

// Devices returns printers seen within the last 30 minutes, sorted by name.
func (l *Listener) Devices() []Device {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []Device{}
	for _, d := range l.devices {
		if time.Since(d.LastSeen) < 30*time.Minute {
			out = append(out, d)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Err reports why discovery could not start.
func (l *Listener) Err() error {
	return l.err
}

// Parse decodes a Bambu SSDP NOTIFY message or search response.
func Parse(b []byte) (Device, bool) {
	r := textproto.NewReader(bufio.NewReader(bytes.NewReader(b)))
	first, err := r.ReadLine()
	if err != nil || !(strings.HasPrefix(first, "NOTIFY") || strings.HasPrefix(first, "HTTP/1.1 200")) {
		return Device{}, false
	}
	h, err := r.ReadMIMEHeader()
	if err != nil && len(h) == 0 {
		return Device{}, false
	}
	if !strings.Contains(h.Get("NT")+h.Get("ST"), "bambulab") || h.Get("USN") == "" {
		return Device{}, false
	}
	code := h.Get("DevModel.bambu.com")
	return Device{
		Serial:   h.Get("USN"),
		Host:     hostOf(h.Get("Location")),
		Model:    ModelName(code),
		Code:     code,
		Name:     h.Get("DevName.bambu.com"),
		Connect:  h.Get("DevConnect.bambu.com"),
		Firmware: h.Get("DevVersion.bambu.com"),
	}, true
}

// hostOf returns the address in a Location header, which may also be a URL.
func hostOf(location string) string {
	s := location
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	if h, _, err := net.SplitHostPort(s); err == nil {
		return h
	}
	return s
}
