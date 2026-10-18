package hms

import (
	"strings"
	"testing"
)

func TestTextFromEmbeddedTables(t *testing.T) {
	// The heatbed heater fault exists in every table version.
	code := "0300_0100_0001_0001"
	en := Text("en", "03919D000000000", KindHMS, code)
	pl := Text("pl", "03919D000000000", KindHMS, code)
	if en == "" || pl == "" || en == pl {
		t.Fatalf("en %q, pl %q", en, pl)
	}
	if got := Text("de", "03919D000000000", KindHMS, code); got != en {
		t.Errorf("unknown language should fall back to English, got %q", got)
	}
	if Text("en", "", KindHMS, "FFFF_FFFF_FFFF_FFFF") != "" {
		t.Error("unknown code described")
	}
}

func TestHelpURLAndSeverity(t *testing.T) {
	u := HelpURL("pl", "03919D000000000", KindError, "0300_400C")
	for _, part := range []string{"e=0300400C", "d=039", "s=device_error", "lang=pl"} {
		if !strings.Contains(u, part) {
			t.Errorf("%s lacks %s", u, part)
		}
	}
	if Severity(0x00020007) != "serious" || Severity(0x00040001) != "info" || Severity(0) != "" {
		t.Error("wrong severity")
	}
}
