// Package bambustudio reads printer access codes saved by Bambu Studio.
package bambustudio

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// ConfigPath returns where Bambu Studio keeps BambuStudio.conf for the
// current user. A service running under a system account has its own profile
// and will not find the file of a desktop user.
func ConfigPath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "BambuStudio", "BambuStudio.conf")
	case "darwin":
		home, _ := os.UserHomeDir()
		return filepath.Join(home, "Library", "Application Support", "BambuStudio", "BambuStudio.conf")
	default:
		dir, err := os.UserConfigDir()
		if err != nil {
			return ""
		}
		return filepath.Join(dir, "BambuStudio", "BambuStudio.conf")
	}
}

// AccessCodes parses BambuStudio.conf and returns the access codes it holds,
// keyed by printer serial number. The file is JSON followed by comment lines
// starting with "#".
func AccessCodes(data []byte) (map[string]string, error) {
	var js bytes.Buffer
	for _, line := range bytes.Split(data, []byte("\n")) {
		if !bytes.HasPrefix(bytes.TrimSpace(line), []byte("#")) {
			js.Write(line)
			js.WriteByte('\n')
		}
	}
	var conf struct {
		AccessCode map[string]string `json:"access_code"`
	}
	if err := json.Unmarshal(js.Bytes(), &conf); err != nil {
		return nil, errors.New("not a BambuStudio.conf file")
	}
	codes := map[string]string{}
	for serial, code := range conf.AccessCode {
		if code != "" {
			codes[serial] = code
		}
	}
	return codes, nil
}
