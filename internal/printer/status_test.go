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

func TestStatusFansAndAMS(t *testing.T) {
	var state map[string]any
	msg := `{"print":{"heatbreak_fan_speed":"10","cooling_fan_speed":"15",
	  "ams":{"tray_now":"3","ams":[{"id":"0","tray":[
	    {"id":"0","tray_type":"PLA","tray_sub_brands":"PLA Basic","tray_color":"000000FF","tray_id_name":"A00-K0","remain":80},
	    {"id":"1","tray_type":"PETG","tray_color":"F72323FF","tray_id_name":"","remain":100},
	    {"id":"2"}]}]},
	  "vt_tray":{"id":"254","tray_type":"TPU","tray_color":"161616FF","remain":0}}}`
	if err := json.Unmarshal([]byte(msg), &state); err != nil {
		t.Fatal(err)
	}
	s := statusFrom(state)
	if s.Fans.Heatbreak != 67 || s.Fans.Part != 100 || s.Fans.Aux != 0 {
		t.Errorf("fans = %+v", s.Fans)
	}
	if s.ActiveTray != 3 || len(s.AMS) != 1 || len(s.AMS[0].Trays) != 3 {
		t.Fatalf("ams = %+v active %d", s.AMS, s.ActiveTray)
	}
	tr := s.AMS[0].Trays
	if tr[0].Color != "#000000" || tr[0].Remain != 80 || tr[0].Name != "PLA Basic" {
		t.Errorf("tray 0 = %+v", tr[0])
	}
	if tr[1].Remain != -1 || tr[2].Type != "" {
		t.Errorf("trays = %+v", tr)
	}
	if s.External == nil || s.External.Type != "TPU" || s.External.Remain != -1 {
		t.Errorf("external = %+v", s.External)
	}
}
