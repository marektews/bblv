// Command gen downloads the HMS and error code descriptions from Bambu Lab and
// writes the compressed tables embedded by package hms. Run it from the
// repository root before a release:
//
//	go run ./internal/hms/gen
package main

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Bambu Studio asks for descriptions per printer type, identified by the
// first three characters of the serial number.
var prefixes = []string{
	"039", // A1
	"030", // A1 mini
	"01S", // P1P
	"01P", // P1S
	"00M", // X1, X1C
	"03W", // X1E
	"22E", // P2S
	"093", // H2S
	"094", // H2D
}

var languages = []string{"en", "pl"}

type entry struct {
	Ecode string `json:"ecode"`
	Intro string `json:"intro"`
}

type response struct {
	Result int `json:"result"`
	Data   struct {
		DeviceHMS   map[string]json.RawMessage `json:"device_hms"`
		DeviceError map[string]json.RawMessage `json:"device_error"`
	} `json:"data"`
}

// table maps a code to texts by serial prefix; "" holds the most common text.
type table map[string]map[string]string

type file struct {
	Version string `json:"version"`
	HMS     table  `json:"hms"`
	Errors  table  `json:"errors"`
}

func main() {
	out := filepath.Join("internal", "hms", "data")
	if err := os.MkdirAll(out, 0o755); err != nil {
		log.Fatal(err)
	}
	client := &http.Client{Timeout: time.Minute}
	for _, lang := range languages {
		hms := map[string]map[string]string{} // code -> prefix -> text
		errs := map[string]map[string]string{}
		version := ""
		for _, prefix := range prefixes {
			r, err := fetch(client, lang, prefix)
			if err != nil {
				log.Fatalf("%s/%s: %v", lang, prefix, err)
			}
			for kind, raw := range map[string]map[string]json.RawMessage{"hms": r.Data.DeviceHMS, "errors": r.Data.DeviceError} {
				entries, ver, err := entries(raw, lang)
				if err != nil {
					log.Fatalf("%s/%s %s: %v", lang, prefix, kind, err)
				}
				version = max(version, ver)
				target := hms
				if kind == "errors" {
					target = errs
				}
				for _, e := range entries {
					text := strings.TrimSpace(e.Intro)
					if text == "" {
						continue
					}
					code := strings.ToUpper(e.Ecode)
					if target[code] == nil {
						target[code] = map[string]string{}
					}
					target[code][prefix] = text
				}
			}
			log.Printf("%s %s: done", lang, prefix)
			time.Sleep(500 * time.Millisecond)
		}
		f := file{Version: version, HMS: pack(hms), Errors: pack(errs)}
		path := filepath.Join(out, "hms_"+lang+".json.gz")
		if err := write(path, f); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s: %d HMS codes, %d error codes, version %s", path, len(f.HMS), len(f.Errors), version)
	}
}

func fetch(client *http.Client, lang, prefix string) (*response, error) {
	req, err := http.NewRequest("GET", fmt.Sprintf("https://e.bambulab.com/query.php?lang=%s&d=%s", lang, prefix), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "bblv-hms-gen (+https://github.com/marektews/bblv)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var r response
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	if r.Result != 0 {
		return nil, fmt.Errorf("result %d", r.Result)
	}
	return &r, nil
}

// entries returns the list under the language key, e.g. "en", and the
// table version.
func entries(raw map[string]json.RawMessage, lang string) ([]entry, string, error) {
	var ver json.Number
	if v, ok := raw["ver"]; ok {
		json.Unmarshal(v, &ver)
	}
	for key, v := range raw {
		if strings.EqualFold(key, lang) || strings.HasPrefix(strings.ToLower(key), lang+"_") {
			var list []entry
			if err := json.Unmarshal(v, &list); err != nil {
				return nil, "", err
			}
			return list, ver.String(), nil
		}
	}
	return nil, "", fmt.Errorf("no %q texts", lang)
}

// pack keeps the most common text of each code as the default and the other
// texts only for the printer types that use them.
func pack(in map[string]map[string]string) table {
	out := table{}
	for code, byPrefix := range in {
		count := map[string]int{}
		for _, text := range byPrefix {
			count[text]++
		}
		def := ""
		for text, n := range count {
			if n > count[def] || n == count[def] && text < def {
				def = text
			}
		}
		m := map[string]string{"": def}
		for prefix, text := range byPrefix {
			if text != def {
				m[prefix] = text
			}
		}
		out[code] = m
	}
	return out
}

func write(path string, f file) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	gz, _ := gzip.NewWriterLevel(out, gzip.BestCompression)
	if _, err := gz.Write(b); err != nil {
		out.Close()
		return err
	}
	if err := gz.Close(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
