// Package hmsfile defines the files of HMS and error code texts that the
// generator in ../gen writes and package hms embeds: gzip-compressed JSON,
// one file per language.
package hmsfile

import (
	"compress/gzip"
	"encoding/json"
	"io"
)

// Table maps a code to texts by serial prefix; "" holds the default text,
// the most common one.
type Table map[string]map[string]string

// File holds the texts of one language.
type File struct {
	Version string `json:"version"`
	HMS     Table  `json:"hms"`
	Errors  Table  `json:"errors"`
}

// Name returns the name of the file of a language, e.g. "hms_en.json.gz".
func Name(lang string) string {
	return "hms_" + lang + ".json.gz"
}

// Encode writes a file.
func Encode(w io.Writer, f File) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	gz, _ := gzip.NewWriterLevel(w, gzip.BestCompression)
	if _, err = gz.Write(b); err != nil {
		return err
	}
	return gz.Close()
}

// Decode reads a file.
func Decode(r io.Reader) (*File, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer gz.Close()
	f := &File{}
	if err = json.NewDecoder(gz).Decode(f); err != nil {
		return nil, err
	}
	return f, nil
}
