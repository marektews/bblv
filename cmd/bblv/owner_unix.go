//go:build !windows

package main

import (
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
)

// giveDataDir creates the data directory if needed and makes the account
// the service runs as its owner, with what it holds, as only the owner may
// use it.
func giveDataDir(dir, account string) error {
	u, err := user.Lookup(account)
	if err != nil {
		return err
	}
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return err
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return filepath.WalkDir(dir, func(path string, _ fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		return os.Lchown(path, uid, gid)
	})
}
