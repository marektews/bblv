//go:build !windows

package config

import "os"

// ProtectDir lets only the owner of the data directory, which runs the
// server, use it: config.json holds the access codes, the password hash and
// the session secret, and printer-ca.pem decides which printers are trusted.
// The files in it are created readable by the owner only.
func ProtectDir(dir string) error {
	return os.Chmod(dir, 0o700)
}
