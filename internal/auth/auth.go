// Package auth implements the optional shared password. Without a password
// everything is open; with one, requests need a signed session cookie.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"maps"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/marektews/bblv/internal/config"
)

const (
	cookieName = "bblv_session"
	sessionTTL = 30 * 24 * time.Hour
)

// ErrWrongPassword is returned when the current password does not match.
var ErrWrongPassword = errors.New("wrong password")

// Auth checks and changes the password kept in the configuration.
type Auth struct {
	store    *config.Store
	throttle throttle
	clock    func() time.Time // of the throttle; replaced in tests
}

// New returns an Auth backed by the configuration store.
func New(store *config.Store) *Auth {
	return &Auth{store: store, clock: time.Now}
}

// Required reports whether a password is set.
func (a *Auth) Required() bool {
	return a.store.Get().PasswordHash != ""
}

// Valid reports whether the request may use the application.
func (a *Auth) Valid(r *http.Request) bool {
	c := a.store.Get()
	if c.PasswordHash == "" {
		return true
	}
	cookie, err := r.Cookie(cookieName)
	if err != nil || !verify(c.SessionSecret, cookie.Value, time.Now()) {
		return false
	}
	_, revoked := c.RevokedSessions[sessionID(cookie.Value)]
	return !revoked
}

// Login checks the password and on success starts a session. After failed
// attempts, further ones are refused with a *WaitError for a while.
func (a *Auth) Login(w http.ResponseWriter, r *http.Request, password string) (bool, error) {
	c := a.store.Get()
	if c.PasswordHash == "" {
		return true, nil
	}
	if wait := a.throttle.begin(a.clock()); wait != nil {
		return false, wait
	}
	ok := bcrypt.CompareHashAndPassword([]byte(c.PasswordHash), []byte(password)) == nil
	a.throttle.end(ok, a.clock())
	if !ok {
		return false, nil
	}
	secret := c.SessionSecret
	if secret == "" {
		// The password was set by editing config.json.
		secret = newSecret()
		if err := a.store.Update(func(cfg *config.Config) error {
			cfg.SessionSecret = secret
			return nil
		}); err != nil {
			return false, err
		}
	}
	setCookie(w, r, secret)
	return true, nil
}

// Logout ends the session of the caller, also for copies of its cookie: the
// session is revoked until it would have expired.
func (a *Auth) Logout(w http.ResponseWriter, r *http.Request) error {
	clearCookie(w)
	cookie, err := r.Cookie(cookieName)
	c := a.store.Get()
	now := time.Now()
	if err != nil || c.PasswordHash == "" || !verify(c.SessionSecret, cookie.Value, now) {
		return nil
	}
	expires, _ := expiry(cookie.Value)
	return a.store.Update(func(cfg *config.Config) error {
		// Expired sessions are refused anyway.
		maps.DeleteFunc(cfg.RevokedSessions, func(_ string, exp int64) bool { return exp < now.Unix() })
		if cfg.RevokedSessions == nil {
			cfg.RevokedSessions = map[string]int64{}
		}
		cfg.RevokedSessions[sessionID(cookie.Value)] = expires
		return nil
	})
}

func clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

// sessionID identifies a session in the list of revoked ones without
// keeping its cookie, which would be valid again with the old secret.
func sessionID(cookie string) string {
	sum := sha256.Sum256([]byte(cookie))
	return hex.EncodeToString(sum[:16])
}

// SetPassword replaces the password after checking the current one; an empty
// password removes it. Other sessions end, the caller gets a new one. Wrong
// current passwords hold further checks back like failed logins, with a
// *WaitError.
func (a *Auth) SetPassword(w http.ResponseWriter, r *http.Request, current, password string) error {
	// Checked outside the configuration lock, which every request takes to
	// check its session.
	old := a.store.Get().PasswordHash
	if old != "" {
		if wait := a.throttle.begin(a.clock()); wait != nil {
			return wait
		}
		ok := bcrypt.CompareHashAndPassword([]byte(old), []byte(current)) == nil
		a.throttle.end(ok, a.clock())
		if !ok {
			return ErrWrongPassword
		}
	}
	var hash []byte
	if password != "" {
		var err error
		if hash, err = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost); err != nil {
			return err
		}
	}
	secret := ""
	err := a.store.Update(func(c *config.Config) error {
		if c.PasswordHash != old { // changed meanwhile
			return ErrWrongPassword
		}
		if hash != nil {
			secret = newSecret()
		}
		// A new secret ends every session, the revoked ones included.
		c.PasswordHash, c.SessionSecret, c.RevokedSessions = string(hash), secret, nil
		return nil
	})
	if err != nil {
		return err
	}
	if secret != "" {
		setCookie(w, r, secret)
	} else {
		clearCookie(w)
	}
	return nil
}

func setCookie(w http.ResponseWriter, r *http.Request, secret string) {
	expires := time.Now().Add(sessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token(secret, expires, newSessionID()),
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

// token is "<expiry unix time>.<id>.<HMAC of both>". The random id tells
// sessions started in the same second apart, so that logging out of one
// ends no other. Tokens without an id, "<expiry>.<HMAC of it>", were made
// before; they stay valid until they expire.
func token(secret string, expires time.Time, id string) string {
	signed := strconv.FormatInt(expires.Unix(), 10)
	if id != "" {
		signed += "." + id
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(signed))
	return signed + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verify(secret, value string, now time.Time) bool {
	n, ok := expiry(value)
	if secret == "" || !ok || now.Unix() > n {
		return false
	}
	signed := value[:strings.LastIndexByte(value, '.')]
	_, id, _ := strings.Cut(signed, ".")
	return hmac.Equal([]byte(token(secret, time.Unix(n, 0), id)), []byte(value))
}

// newSessionID returns a random id for a session token.
func newSessionID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// expiry returns the expiry unix time of a token.
func expiry(value string) (int64, bool) {
	exp, _, ok := strings.Cut(value, ".")
	n, err := strconv.ParseInt(exp, 10, 64)
	return n, ok && err == nil
}

func newSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
