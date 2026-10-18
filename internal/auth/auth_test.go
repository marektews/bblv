package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marektews/bblv/internal/config"
)

func newAuth(t *testing.T) *Auth {
	t.Helper()
	store, err := config.OpenStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return New(store)
}

// withCookies returns a request carrying the cookies set on rec.
func withCookies(rec *httptest.ResponseRecorder) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge >= 0 {
			r.AddCookie(c)
		}
	}
	return r
}

func TestPasswordLifecycle(t *testing.T) {
	a := newAuth(t)
	anon := httptest.NewRequest("GET", "/", nil)
	if a.Required() || !a.Valid(anon) {
		t.Fatal("without a password everything is open")
	}

	rec := httptest.NewRecorder()
	if err := a.SetPassword(rec, anon, "", "secret"); err != nil {
		t.Fatal(err)
	}
	if !a.Required() || a.Valid(anon) {
		t.Fatal("password not enforced")
	}
	if !a.Valid(withCookies(rec)) {
		t.Fatal("setting the password should keep the caller logged in")
	}

	rec = httptest.NewRecorder()
	if ok, _ := a.Login(rec, anon, "wrong"); ok || len(rec.Result().Cookies()) != 0 {
		t.Fatal("wrong password accepted")
	}
	rec = httptest.NewRecorder()
	if ok, err := a.Login(rec, anon, "secret"); !ok || err != nil {
		t.Fatalf("login failed: %v", err)
	}
	session := withCookies(rec)
	if !a.Valid(session) {
		t.Fatal("session cookie rejected")
	}

	if err := a.SetPassword(httptest.NewRecorder(), session, "wrong", "other"); err != ErrWrongPassword {
		t.Fatalf("err = %v", err)
	}
	if err := a.SetPassword(httptest.NewRecorder(), session, "secret", "other"); err != nil {
		t.Fatal(err)
	}
	if a.Valid(session) {
		t.Fatal("old sessions must end when the password changes")
	}

	if err := a.SetPassword(httptest.NewRecorder(), anon, "other", ""); err != nil || a.Required() {
		t.Fatalf("removing the password: %v", err)
	}
}

func TestTokenExpiryAndTampering(t *testing.T) {
	now := time.Now()
	tok := token("s", now.Add(time.Hour))
	if !verify("s", tok, now) {
		t.Fatal("valid token rejected")
	}
	if verify("s", tok, now.Add(2*time.Hour)) {
		t.Fatal("expired token accepted")
	}
	if verify("other", tok, now) || verify("s", "9999999999."+tok[len(tok)-10:], now) || verify("", tok, now) {
		t.Fatal("forged token accepted")
	}
}
