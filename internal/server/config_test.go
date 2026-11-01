package server

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
)

func send(t *testing.T, c *http.Client, method, url string, body any) *http.Response {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, url, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestPrinterConfigLifecycle(t *testing.T) {
	ts, cfg := setupWithConfig(t)
	c := ts.Client()

	resp := send(t, c, "POST", ts.URL+"/api/config/printers", map[string]string{"name": "Bambus A1", "model": "A1", "host": "127.0.0.1", "serial": "03919d000000000"})
	var created printerView
	json.NewDecoder(resp.Body).Decode(&created)
	if resp.StatusCode != 200 || created.ID != "bambus-a1" || created.Serial != "03919D000000000" || created.HasAccessCode {
		t.Fatalf("create: %d %+v", resp.StatusCode, created)
	}
	if dup := send(t, c, "POST", ts.URL+"/api/config/printers", map[string]string{"host": "127.0.0.2", "serial": "03919D000000000"}); dup.StatusCode != http.StatusConflict {
		t.Fatalf("duplicate serial: %d", dup.StatusCode)
	}
	var info []struct{ ID string }
	getJSON(t, ts.URL+"/api/printers", &info)
	if len(info) != 2 || info[1].ID != "bambus-a1" {
		t.Fatalf("monitored printers = %+v", info)
	}

	// Import the access code from an uploaded BambuStudio.conf.
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", "BambuStudio.conf")
	io.WriteString(fw, `{"access_code": {"03919D000000000": "87654321"}}`+"\n# MD5 checksum x\n")
	mw.Close()
	resp, err := c.Post(ts.URL+"/api/config/import-codes", mw.FormDataContentType(), &body)
	if err != nil {
		t.Fatal(err)
	}
	var imported struct{ Updated, Missing []string }
	json.NewDecoder(resp.Body).Decode(&imported)
	resp.Body.Close()
	if len(imported.Updated) != 1 || imported.Updated[0] != "bambus-a1" {
		t.Fatalf("import: %d %+v", resp.StatusCode, imported)
	}
	if got := cfg.Get().Printers[0].AccessCode; got != "87654321" {
		t.Fatalf("access code = %q", got)
	}

	// Editing without a code keeps it; the code is never sent back.
	resp = send(t, c, "PUT", ts.URL+"/api/config/printers/bambus-a1", map[string]string{"name": "Biuro", "host": "127.0.0.3", "serial": "03919D000000000"})
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || strings.Contains(string(raw), "87654321") || !strings.Contains(string(raw), `"hasAccessCode":true`) {
		t.Fatalf("update: %d %s", resp.StatusCode, raw)
	}
	resp = send(t, c, "GET", ts.URL+"/api/config", nil)
	raw, _ = io.ReadAll(resp.Body)
	if strings.Contains(string(raw), "87654321") || !strings.Contains(string(raw), `"host":"127.0.0.3"`) {
		t.Fatalf("config leaks the code or lacks the update: %s", raw)
	}

	// Another serial number, not pinned to a CA yet, does not get the code:
	// any device could claim it.
	resp = send(t, c, "PUT", ts.URL+"/api/config/printers/bambus-a1", map[string]string{"host": "127.0.0.4", "serial": "03919D999999999"})
	if resp.StatusCode != http.StatusBadRequest || cfg.Get().Printers[0].Host != "127.0.0.3" {
		t.Fatalf("serial changed without the code: %d %+v", resp.StatusCode, cfg.Get().Printers[0])
	}
	resp = send(t, c, "PUT", ts.URL+"/api/config/printers/bambus-a1", map[string]string{"host": "127.0.0.4", "serial": "03919D999999999", "accessCode": "11112222"})
	if p := cfg.Get().Printers[0]; resp.StatusCode != 200 || p.Serial != "03919D999999999" || p.AccessCode != "11112222" {
		t.Fatalf("serial change with the code: %d %+v", resp.StatusCode, p)
	}

	if del := send(t, c, "DELETE", ts.URL+"/api/config/printers/bambus-a1", nil); del.StatusCode != http.StatusNoContent {
		t.Fatalf("delete: %d", del.StatusCode)
	}
	if len(cfg.Get().Printers) != 0 {
		t.Fatal("printer still configured")
	}
	getJSON(t, ts.URL+"/api/printers", &info)
	if len(info) != 1 {
		t.Fatalf("printer still monitored: %+v", info)
	}
}

func TestPasswordProtectsAPI(t *testing.T) {
	ts, _ := setupWithConfig(t)
	jar, _ := cookiejar.New(nil)
	user := &http.Client{Jar: jar}
	other := ts.Client()

	if resp := send(t, user, "PUT", ts.URL+"/api/config/password", map[string]string{"password": "tajne"}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("set password: %d", resp.StatusCode)
	}
	if resp := send(t, other, "GET", ts.URL+"/api/printers", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("anonymous request: %d", resp.StatusCode)
	}
	if resp := send(t, user, "GET", ts.URL+"/api/printers", nil); resp.StatusCode != 200 {
		t.Fatalf("the user who set the password should stay logged in: %d", resp.StatusCode)
	}
	var st struct{ Required, Authenticated bool }
	json.NewDecoder(send(t, other, "GET", ts.URL+"/api/auth", nil).Body).Decode(&st)
	if !st.Required || st.Authenticated {
		t.Fatalf("auth status = %+v", st)
	}
	if resp := send(t, other, "POST", ts.URL+"/api/login", map[string]string{"password": "tajne"}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("login: %d", resp.StatusCode)
	}
	if resp := send(t, user, "PUT", ts.URL+"/api/config/password", map[string]string{"current": "zle", "password": ""}); resp.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong current password: %d", resp.StatusCode)
	}
}

func TestCrossOriginChangesRejected(t *testing.T) {
	ts, _ := setupWithConfig(t)
	req, _ := http.NewRequest("POST", ts.URL+"/api/config/printers", strings.NewReader(`{"host":"1.2.3.4","serial":"X"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://evil.example")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status %d", resp.StatusCode)
	}
	req, _ = http.NewRequest("POST", ts.URL+"/api/config/printers", strings.NewReader(`{"host":"1.2.3.4","serial":"X"}`))
	req.Header.Set("Content-Type", "text/plain")
	resp, err = ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Fatalf("form-like post: %d", resp.StatusCode)
	}
}

func TestNtfySettings(t *testing.T) {
	ts, cfg := setupWithConfig(t)
	c := ts.Client()
	titles := make(chan string, 2)
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		titles <- r.Header.Get("Authorization") + " " + body["title"].(string)
	}))
	defer ntfy.Close()

	var got struct {
		Ntfy ntfyView `json:"ntfy"`
	}
	json.NewDecoder(send(t, c, "GET", ts.URL+"/api/config", nil).Body).Decode(&got)
	if got.Ntfy.URL != "https://ntfy.sh" || got.Ntfy.Topic != "" || len(got.Ntfy.Events) != 4 {
		t.Fatalf("defaults: %+v", got.Ntfy)
	}
	if r := send(t, c, "POST", ts.URL+"/api/config/ntfy/test", nil); r.StatusCode != http.StatusConflict {
		t.Fatalf("test while off: %d", r.StatusCode)
	}
	if r := send(t, c, "PUT", ts.URL+"/api/config/ntfy", map[string]any{"topic": "a/b", "events": []string{"finish"}, "lang": "pl"}); r.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad topic: %d", r.StatusCode)
	}

	in := map[string]any{"url": ntfy.URL + "/", "topic": "bblv-x", "token": "tk_1", "events": []string{"finish", "nonsense"}, "lang": "pl", "link": "https://pc:8443/"}
	resp := send(t, c, "PUT", ts.URL+"/api/config/ntfy", in)
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || strings.Contains(string(raw), "tk_1") || !strings.Contains(string(raw), `"hasToken":true`) {
		t.Fatalf("save: %d %s", resp.StatusCode, raw)
	}
	if n := cfg.Get().Ntfy; n.Token != "tk_1" || n.URL != ntfy.URL || n.Link != "https://pc:8443" || len(n.Events) != 1 {
		t.Fatalf("saved %+v", n)
	}
	if r := send(t, c, "POST", ts.URL+"/api/config/ntfy/test", nil); r.StatusCode != http.StatusNoContent {
		t.Fatalf("test: %d", r.StatusCode)
	}
	if title := <-titles; title != "Bearer tk_1 bblv: powiadomienie testowe" {
		t.Fatalf("test sent %q", title)
	}

	// The token stays with its server: another one needs it again.
	delete(in, "token")
	if r := send(t, c, "PUT", ts.URL+"/api/config/ntfy", in); r.StatusCode != 200 || cfg.Get().Ntfy.Token != "tk_1" {
		t.Fatalf("save without token: %d", r.StatusCode)
	}
	in["url"] = "https://ntfy.example.com"
	if r := send(t, c, "PUT", ts.URL+"/api/config/ntfy", in); r.StatusCode != http.StatusBadRequest || cfg.Get().Ntfy.URL != ntfy.URL {
		t.Fatalf("other server kept the token: %d", r.StatusCode)
	}
	in["clearToken"] = true
	if r := send(t, c, "PUT", ts.URL+"/api/config/ntfy", in); r.StatusCode != 200 || cfg.Get().Ntfy.Token != "" {
		t.Fatalf("clear token: %d %+v", r.StatusCode, cfg.Get().Ntfy)
	}
}
