// Package webui serves the frontend built by Vite into dist/ and embedded in the binary.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed all:dist
var dist embed.FS

const placeholder = `<!doctype html><meta charset="utf-8"><title>bblv</title>
<p>The frontend is not built into this binary. Run <code>build.ps1</code> / <code>build.sh</code>,
or start the Vite dev server in <code>web/</code>. The API is available at <a href="/api/printers">/api/printers</a>.</p>`

// Handler serves the frontend. Paths without a matching file get index.html
// so the client-side router can handle them.
func Handler() http.Handler {
	files, _ := fs.Sub(dist, "dist")
	if _, err := fs.Stat(files, "index.html"); err != nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Write([]byte(placeholder))
		})
	}
	fileServer := http.FileServerFS(files)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" || name == "index.html" {
			name = "index.html"
		} else if _, err := fs.Stat(files, name); err != nil {
			name = "index.html"
		}
		if name == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
			r = r.Clone(r.Context())
			r.URL.Path = "/"
		} else if strings.HasPrefix(name, "assets/") {
			// Vite puts content hashes into asset file names.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fileServer.ServeHTTP(w, r)
	})
}
