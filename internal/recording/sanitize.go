package recording

import (
	"bytes"
	"encoding/json"
	"strings"
)

// sensitive keys hold identifiers of the printer, the AMS, spools or the network.
var sensitive = map[string]bool{
	"sn": true, "chip_id": true, "tag_uid": true, "tray_uuid": true, "dev_id": true,
	"ip": true, "mask": true, "mac": true, "ssid": true, "rtsp_url": true,
}

// Sanitizer removes identifying values from reports so recordings can be shared.
type Sanitizer struct {
	secrets []string // e.g. serial numbers, replaced wherever they occur
}

// NewSanitizer returns a sanitizer that also replaces the given strings.
func NewSanitizer(secrets ...string) *Sanitizer {
	s := &Sanitizer{}
	for _, v := range secrets {
		if len(v) >= 6 {
			s.secrets = append(s.secrets, v)
		}
	}
	return s
}

// Report returns the payload with sensitive values replaced. Payloads that
// are not JSON only get the secrets replaced.
func (s *Sanitizer) Report(payload []byte) []byte {
	var v any
	dec := json.NewDecoder(bytes.NewReader(payload))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return []byte(s.text(string(payload)))
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s.scrub(v)); err != nil {
		return []byte(s.text(string(payload)))
	}
	return bytes.TrimSpace(buf.Bytes())
}

func (s *Sanitizer) scrub(v any) any {
	switch v := v.(type) {
	case map[string]any:
		for k, x := range v {
			if !sensitive[k] {
				v[k] = s.scrub(x)
				continue
			}
			switch x.(type) {
			case string:
				v[k] = "REDACTED"
			case json.Number:
				v[k] = json.Number("0")
			default:
				v[k] = s.scrub(x)
			}
		}
		return v
	case []any:
		for i := range v {
			v[i] = s.scrub(v[i])
		}
		return v
	case string:
		return s.text(v)
	}
	return v
}

func (s *Sanitizer) text(t string) string {
	for _, secret := range s.secrets {
		t = strings.ReplaceAll(t, secret, "SERIAL")
	}
	return t
}

// Serials returns the serial numbers ("sn" values) found in a report.
func Serials(payload []byte) []string {
	var v any
	if json.Unmarshal(payload, &v) != nil {
		return nil
	}
	var out []string
	var walk func(any)
	walk = func(v any) {
		switch v := v.(type) {
		case map[string]any:
			for k, x := range v {
				if s, ok := x.(string); ok && k == "sn" && s != "" {
					out = append(out, s)
				}
				walk(x)
			}
		case []any:
			for _, x := range v {
				walk(x)
			}
		}
	}
	walk(v)
	return out
}
