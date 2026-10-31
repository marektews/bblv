package printer

import (
	"encoding/json"
	"testing"
)

func TestChamberLight(t *testing.T) {
	for _, tc := range []struct{ msg, want string }{
		{`{"print":{"lights_report":[{"node":"chamber_light","mode":"on"}]}}`, "on"}, // as the A1 reports it
		{`{"print":{"lights_report":[{"node":"work_light","mode":"flashing"},{"node":"chamber_light","mode":"off"}]}}`, "off"},
		{`{"print":{"gcode_state":"IDLE"}}`, ""}, // no light reported
	} {
		var state map[string]any
		if err := json.Unmarshal([]byte(tc.msg), &state); err != nil {
			t.Fatal(err)
		}
		if got := statusFrom(state).ChamberLight; got != tc.want {
			t.Errorf("%s: chamber light %q, want %q", tc.msg, got, tc.want)
		}
	}
	if got := lightCommand(true); got != `{"system":{"sequence_id":"0","command":"ledctrl","led_node":"chamber_light","led_mode":"on","led_on_time":500,"led_off_time":500,"loop_times":0,"interval_time":0}}` {
		t.Errorf("command %s", got)
	}
}
