package recording

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSanitizerRemovesIdentifiers(t *testing.T) {
	in := `{"info":{"module":[{"name":"ota","sn":"03919D000000000","sw_ver":"01.08.01.00"}]},
	  "print":{"ams":{"ams":[{"chip_id":"abc123","tray":[{"tag_uid":"0123456789ABCDEF","tray_uuid":"FF","tray_type":"PLA"}]}]},
	  "net":{"info":[{"ip":3232235624,"mask":16777215}]},"msg":"device/03919D000000000/report","mc_percent":55,"k":0.019999999552965164}}`
	out := string(NewSanitizer("03919D000000000").Report([]byte(in)))
	for _, leak := range []string{"03919D000000000", "abc123", "0123456789ABCDEF", "3232235624"} {
		if strings.Contains(out, leak) {
			t.Errorf("%q left in %s", leak, out)
		}
	}
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatal(err)
	}
	p := v["print"].(map[string]any)
	if p["mc_percent"].(float64) != 55 || p["msg"] != "device/SERIAL/report" || !strings.Contains(out, `"k":0.019999999552965164`) {
		t.Errorf("unrelated values changed: %s", out)
	}
	if !strings.Contains(out, `"tray_type":"PLA"`) || !strings.Contains(out, `"sw_ver":"01.08.01.00"`) {
		t.Errorf("useful values removed: %s", out)
	}
}

func TestSerials(t *testing.T) {
	got := Serials([]byte(`{"info":{"module":[{"sn":"AAA111222"},{"sn":""},{"name":"x"}]}}`))
	if len(got) != 1 || got[0] != "AAA111222" {
		t.Fatalf("serials = %v", got)
	}
}
