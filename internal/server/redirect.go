package server

import (
	"net"
	"net/http"
	"strings"
)

// RedirectToHTTPS sends every request to the same address under HTTPS on
// the port of httpsAddr, so that bookmarks of the plain HTTP address keep
// working. The redirect is temporary: a permanent one, kept by browsers,
// would break the plain address if HTTPS were turned off again.
func RedirectToHTTPS(httpsAddr string) http.Handler {
	_, port, err := net.SplitHostPort(httpsAddr)
	if err != nil {
		port = "443"
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := r.Host
		if h, _, splitErr := net.SplitHostPort(host); splitErr == nil {
			host = h
		}
		host = strings.Trim(host, "[]")
		if host == "" {
			http.Error(w, "use https", http.StatusBadRequest)
			return
		}
		target := net.JoinHostPort(host, port)
		if port == "443" {
			target = strings.TrimSuffix(target, ":443")
		}
		http.Redirect(w, r, "https://"+target+r.URL.RequestURI(), http.StatusTemporaryRedirect)
	})
}
