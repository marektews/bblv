package printer

import (
	"encoding/json"
	"testing"
)

func TestMergeKeepsFieldsAndReplacesArrays(t *testing.T) {
	state := map[string]any{}
	for _, msg := range []string{
		`{"print":{"gcode_state":"RUNNING","layer_num":1,"hms":[{"attr":1,"code":2}],"ams":{"tray_now":"0"}}}`,
		`{"print":{"layer_num":2,"hms":[]}}`,
	} {
		var m map[string]any
		if err := json.Unmarshal([]byte(msg), &m); err != nil {
			t.Fatal(err)
		}
		merge(state, m)
	}
	s := statusFrom(state)
	if s.State != "RUNNING" || s.Layer != 2 || len(s.HMS) != 0 {
		t.Fatalf("unexpected status %+v", s)
	}
	if got := obj(obj(state, "print"), "ams")["tray_now"]; got != "0" {
		t.Fatalf("nested field lost, got %v", got)
	}
}

func TestStatusParsesStringNumbersAndHMS(t *testing.T) {
	var state map[string]any
	msg := `{"print":{"spd_lvl":"2","mc_percent":42,"hms":[{"attr":50331904,"code":65543}]},
	         "info":{"module":[{"name":"mc","sw_ver":"x"},{"name":"ota","sw_ver":"01.04.00.00"}]}}`
	if err := json.Unmarshal([]byte(msg), &state); err != nil {
		t.Fatal(err)
	}
	s := statusFrom(state)
	if s.SpeedLevel != 2 || s.Percent != 42 || s.Firmware != "01.04.00.00" {
		t.Fatalf("unexpected status %+v", s)
	}
	if len(s.HMS) != 1 || s.HMS[0].String() != "0300_0100_0001_0007" {
		t.Fatalf("unexpected hms %v", s.HMS)
	}
}
