package discovery

import (
	"strconv"
	"testing"
	"time"
)

func TestDevicesAreForgottenAndBounded(t *testing.T) {
	l := &Listener{devices: map[string]Device{}}
	now := time.Now()
	l.add(Device{Serial: "OLD", Name: "old", LastSeen: now.Add(-forgetAfter)})
	for i := range maxDevices + 10 { // announcements with made-up serials
		l.add(Device{Serial: strconv.Itoa(i), LastSeen: now})
	}
	if n := len(l.devices); n != maxDevices {
		t.Fatalf("%d devices kept, want at most %d", n, maxDevices)
	}
	if _, ok := l.devices["OLD"]; ok {
		t.Error("a printer not seen for half an hour was kept instead of a new one")
	}
	l.add(Device{Serial: "0", Name: "renamed", LastSeen: now}) // a known one still updates
	if l.devices["0"].Name != "renamed" {
		t.Error("a known printer was not updated when the list was full")
	}
}
