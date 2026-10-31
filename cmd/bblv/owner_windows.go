package main

import "errors"

// giveDataDir is not supported: a Windows service runs as LocalSystem,
// which needs no password, and its data directory is restricted to the
// system, the administrators and the directory's owner on start.
func giveDataDir(string, string) error {
	return errors.New("-user is supported on Linux only")
}
