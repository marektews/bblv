package webui

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestFilesOfOtherBuildsAreMissing(t *testing.T) {
	h := handler(fstest.MapFS{
		"index.html":      {Data: []byte("<!doctype html>")},
		"assets/app-1.js": {Data: []byte("app")},
	})
	for path, want := range map[string]int{
		"/":                http.StatusOK,
		"/printer/a1":      http.StatusOK, // index.html for the client-side router
		"/assets/app-1.js": http.StatusOK,
		"/assets/app-0.js": http.StatusNotFound, // the build before an update
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		if rec.Code != want {
			t.Errorf("GET %s: %d, want %d", path, rec.Code, want)
		}
	}
}
