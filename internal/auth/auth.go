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
	store *config.Store
}

// New returns an Auth backed by the configuration store.
func New(store *config.Store) *Auth {
	return &Auth{store: store}
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
	return err == nil && verify(c.SessionSecret, cookie.Value, time.Now())
}

// Login checks the password and on success starts a session.
func (a *Auth) Login(w http.ResponseWriter, r *http.Request, password string) (bool, error) {
	c := a.store.Get()
	if c.PasswordHash == "" {
		return true, nil
	}
	if bcrypt.CompareHashAndPassword([]byte(c.PasswordHash), []byte(password)) != nil {
		return false, nil
	}
	secret := c.SessionSecret
	if secret == "" {
		// The password was set by editing config.json.
		secret = newSecret()
		if err := a.store.Update(func(c *config.Config) error {
			c.SessionSecret = secret
			return nil
		}); err != nil {
			return false, err
		}
	}
	setCookie(w, r, secret)
	return true, nil
}

// Logout ends the session of the caller.
func (a *Auth) Logout(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteStrictMode})
}

// SetPassword replaces the password after checking the current one; an empty
// password removes it. Other sessions end, the caller gets a new one.
func (a *Auth) SetPassword(w http.ResponseWriter, r *http.Request, current, password string) error {
	var hash []byte
	if password != "" {
		var err error
		if hash, err = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost); err != nil {
			return err
		}
	}
	secret := ""
	err := a.store.Update(func(c *config.Config) error {
		if c.PasswordHash != "" && bcrypt.CompareHashAndPassword([]byte(c.PasswordHash), []byte(current)) != nil {
			return ErrWrongPassword
		}
		if hash != nil {
			secret = newSecret()
		}
		c.PasswordHash, c.SessionSecret = string(hash), secret
		return nil
	})
	if err != nil {
		return err
	}
	if secret != "" {
		setCookie(w, r, secret)
	} else {
		a.Logout(w)
	}
	return nil
}

func setCookie(w http.ResponseWriter, r *http.Request, secret string) {
	expires := time.Now().Add(sessionTTL)
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    token(secret, expires),
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
	})
}

// token is "<expiry unix time>.<HMAC of it>".
func token(secret string, expires time.Time) string {
	exp := strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(exp))
	return exp + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func verify(secret, value string, now time.Time) bool {
	exp, _, ok := strings.Cut(value, ".")
	n, err := strconv.ParseInt(exp, 10, 64)
	if secret == "" || !ok || err != nil || now.Unix() > n {
		return false
	}
	return hmac.Equal([]byte(token(secret, time.Unix(n, 0))), []byte(value))
}

func newSecret() string {
	b := make([]byte, 32)
	rand.Read(b)
	return hex.EncodeToString(b)
}
