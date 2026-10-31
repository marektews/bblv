package server

import (
	"net/http"
	"strings"
	"testing"
)

func TestLightRequests(t *testing.T) {
	ts := setup(t)
	for _, tc := range []struct {
		path, contentType string
		want              int
	}{
		{"/api/printers/replay/light", "application/json", http.StatusConflict}, // a replay has no light
		{"/api/printers/gone/light", "application/json", http.StatusNotFound},
		{"/api/printers/replay/light", "text/plain", http.StatusUnsupportedMediaType}, // as a form of another site would send
	} {
		req, _ := http.NewRequest("POST", ts.URL+tc.path, strings.NewReader(`{"on":true}`))
		req.Header.Set("Content-Type", tc.contentType)
		resp, err := ts.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != tc.want {
			t.Errorf("%s (%s): %d, want %d", tc.path, tc.contentType, resp.StatusCode, tc.want)
		}
	}
}
