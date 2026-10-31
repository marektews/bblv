package auth

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/marektews/bblv/internal/config"
)

func TestFailedLoginsHoldBackAllClients(t *testing.T) {
	a := newAuth(t)
	if err := a.SetPassword(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil), "", "secret"); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	a.clock = func() time.Time { return now }
	login := func(password string) (bool, error) {
		return a.Login(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil), password)
	}

	waits := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	for _, want := range waits {
		if ok, err := login("wrong"); ok || err != nil {
			t.Fatalf("wrong password: %v, %v", ok, err)
		}
		// Even the right password waits, whoever sends it.
		var wait *WaitError
		if _, err := login("secret"); !errors.As(err, &wait) || wait.Wait != want {
			t.Fatalf("after a failure: %v, want a wait of %v", err, want)
		}
		now = now.Add(want)
	}
	if ok, err := login("secret"); !ok || err != nil {
		t.Fatalf("after the wait: %v, %v", ok, err)
	}
	if ok, err := login("wrong"); ok || err != nil { // a success starts over
		t.Fatalf("wrong password after a success: %v, %v", ok, err)
	}
	var wait *WaitError
	if _, err := login("wrong"); !errors.As(err, &wait) || wait.Wait != time.Second {
		t.Fatalf("wait after a success and a failure: %v", err)
	}

	th := &throttle{}
	for range 20 {
		th.end(false, now)
	}
	if w := th.begin(now); w == nil || w.Wait != maxWait {
		t.Fatalf("wait after many failures = %v, want %v", w, maxWait)
	}
}

func TestLogoutRevokesTheSession(t *testing.T) {
	dir := t.TempDir()
	store, err := config.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	a := New(store)
	anon := httptest.NewRequest("GET", "/", nil)
	if err = a.SetPassword(httptest.NewRecorder(), anon, "", "secret"); err != nil {
		t.Fatal(err)
	}
	logins := make([]*httptest.ResponseRecorder, 2)
	for i := range logins {
		logins[i] = httptest.NewRecorder()
		if ok, loginErr := a.Login(logins[i], anon, "secret"); !ok || loginErr != nil {
			t.Fatalf("login: %v, %v", ok, loginErr)
		}
		time.Sleep(1100 * time.Millisecond) // another expiry second, so another token
	}
	stolen, other := withCookies(logins[0]), withCookies(logins[1])

	if err = a.Logout(httptest.NewRecorder(), stolen); err != nil {
		t.Fatal(err)
	}
	if a.Valid(stolen) {
		t.Fatal("a copy of the cookie still works after the logout")
	}
	if !a.Valid(other) {
		t.Fatal("the logout ended another session")
	}

	// A restart keeps the revocation; a new password clears the list.
	reopened, err := config.OpenStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if New(reopened).Valid(stolen) {
		t.Fatal("revocation lost on restart")
	}
	if err = a.SetPassword(httptest.NewRecorder(), other, "secret", "new"); err != nil {
		t.Fatal(err)
	}
	if n := len(a.store.Get().RevokedSessions); n != 0 {
		t.Fatalf("%d revoked sessions kept after the secret changed", n)
	}
}
