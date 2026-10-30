package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/marektews/bblv/internal/recording"
)

// runAnonymize removes identifiers from an existing recording.
func runAnonymize(args []string) error {
	fs := flag.NewFlagSet("anonymize", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: bblv anonymize <in.jsonl> [out.jsonl]")
		fmt.Fprintln(fs.Output(), "Writes <in>-anonymized.jsonl when out is not given.")
	}
	fs.Parse(args)
	if fs.NArg() < 1 || fs.NArg() > 2 {
		fs.Usage()
		os.Exit(2)
	}
	in := fs.Arg(0)
	out := fs.Arg(1)
	if out == "" {
		out = strings.TrimSuffix(in, ".jsonl") + "-anonymized.jsonl"
	}
	entries, err := readEntries(in)
	if err != nil {
		return err
	}
	// The printer serial is not known here; every "sn" value is replaced.
	var serials []string
	for _, e := range entries {
		serials = append(serials, recording.Serials(e.Payload)...)
	}
	san := recording.NewSanitizer(serials...)

	f, err := os.Create(out)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, e := range entries {
		if e.Kind == "meta" {
			e.Anonymized = true
		} else if e.Payload != nil {
			e.Payload = san.Report(e.Payload)
		}
		if err = enc.Encode(e); err != nil {
			f.Close()
			return err
		}
	}
	if err = f.Close(); err != nil {
		return err
	}
	fmt.Println("wrote", out)
	return nil
}

func readEntries(path string) ([]recording.Entry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := json.NewDecoder(bufio.NewReader(f))
	var out []recording.Entry
	for {
		var e recording.Entry
		if err = dec.Decode(&e); errors.Is(err, io.EOF) {
			return out, nil
		} else if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, e)
	}
}
