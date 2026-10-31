// Package webui serves the frontend built by Vite into dist/ and embedded in the binary.
package webui

import (
	"bytes"
	"compress/gzip"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
)

//go:embed all:dist
var dist embed.FS

const placeholder = `<!doctype html><meta charset="utf-8"><title>bblv</title>
<p>The frontend is not built into this binary. Run <code>build.ps1</code> / <code>build.sh</code>,
or start the Vite dev server in <code>web/</code>. The API is available at <a href="/api/printers">/api/printers</a>.</p>`

// Handler serves the frontend. Paths without a matching file get index.html
// so the client-side router can handle them. Text files are sent gzipped.
func Handler() http.Handler {
	files, _ := fs.Sub(dist, "dist")
	if _, err := fs.Stat(files, "index.html"); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(placeholder))
		})
	}
	return handler(files)
}

func handler(files fs.FS) http.Handler {
	fileServer := http.FileServerFS(files)
	var gzipped sync.Map // file name -> compressed content
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		} else if _, err := fs.Stat(files, name); err != nil {
			if strings.HasPrefix(name, "assets/") {
				// A file of another build, asked for by a page loaded
				// before an update. It has to fail for the page to reload;
				// index.html in its place fails only as a wrong script.
				http.NotFound(w, r)
				return
			}
			name = "index.html"
		}
		h := w.Header()
		if name == "index.html" {
			h.Set("Cache-Control", "no-cache")
		} else if strings.HasPrefix(name, "assets/") {
			// Vite puts content hashes into asset file names.
			h.Set("Cache-Control", "public, max-age=31536000, immutable")
		}

		ext := path.Ext(name)
		if ext == ".js" || ext == ".css" || ext == ".html" || ext == ".svg" {
			h.Add("Vary", "Accept-Encoding")
			if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				b, ok := gzipped.Load(name)
				if !ok {
					data, err := compress(files, name)
					if err != nil {
						http.Error(w, err.Error(), http.StatusInternalServerError)
						return
					}
					b, _ = gzipped.LoadOrStore(name, data)
				}
				data := b.([]byte)
				h.Set("Content-Type", mime.TypeByExtension(ext))
				h.Set("Content-Encoding", "gzip")
				h.Set("Content-Length", strconv.Itoa(len(data)))
				w.Write(data)
				return
			}
		}
		if name == "index.html" {
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		}
		fileServer.ServeHTTP(w, r)
	})
}

func compress(files fs.FS, name string) ([]byte, error) {
	data, err := fs.ReadFile(files, name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	gz, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	gz.Write(data)
	if err = gz.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
