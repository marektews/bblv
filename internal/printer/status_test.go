package printer

import (
	"encoding/json"
	"reflect"
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

func TestStatusSDCardAndLiveView(t *testing.T) {
	for _, tc := range []struct {
		name, msg   string
		sd          bool
		total, free int
		liveView    string
	}{
		{"A1", `{"print":{"sdcard":true,"ipcam":{"ipcam_dev":"1"}}}`, true, 0, 0, ""},
		{"H2D without a card", `{"print":{"sdcard":false,"ipcam":{"rtsp_url":"disable","tl_external_total_kb":0}}}`, false, 0, 0, "disabled"},
		{"P2S", `{"print":{"sdcard":true,"ipcam":{"rtsp_url":"rtsps://192.168.4.44/streaming/live/1",
		  "tl_external_total_kb":124984320,"tl_external_free_kb":37268992}}}`, true, 124984320, 37268992, "enabled"},
	} {
		var state map[string]any
		if err := json.Unmarshal([]byte(tc.msg), &state); err != nil {
			t.Fatal(err)
		}
		s := statusFrom(state)
		if s.SDCard != tc.sd || s.SDTotalKB != tc.total || s.SDFreeKB != tc.free || s.LANLiveView != tc.liveView {
			t.Errorf("%s: sd=%v %d/%d liveView=%q", tc.name, s.SDCard, s.SDFreeKB, s.SDTotalKB, s.LANLiveView)
		}
	}
}

func TestStatusAMSEnvironmentSpoolsAndMapping(t *testing.T) {
	var state map[string]any
	msg := `{"print":{"mapping":[3,65535,32768,1279],
	  "ams":{"ams":[{"id":"0","humidity":"2","humidity_raw":"29","temp":"33.2","dry_time":130,
	    "dry_setting":{"dry_duration":-1,"dry_temperature":65},"tray":[
	    {"id":"0","tray_type":"PETG","tray_id_name":"G02-G0","remain":96,"tray_weight":"1000","total_len":330000,
	     "nozzle_temp_min":"230","nozzle_temp_max":"260"},
	    {"id":"1","tray_type":"PETG-CF","tray_id_name":"","remain":-1,"remain_g":0,"tray_weight":"0"},
	    {"id":"2","tray_type":"PLA","tray_id_name":"A00-K0","remain":50,"remain_g":420}]}]}}}`
	if err := json.Unmarshal([]byte(msg), &state); err != nil {
		t.Fatal(err)
	}
	s := statusFrom(state)
	// ams_id<<8 | slot: AMS 0 slot 3, unused, AMS HT 128 slot 0, AMS 4 slot 255 (invalid).
	if want := []int{3, -1, 512, -1}; !reflect.DeepEqual(s.AMSMapping, want) {
		t.Errorf("mapping = %v, want %v", s.AMSMapping, want)
	}
	u := s.AMS[0]
	if u.Humidity != 29 || u.Temp != 33.2 || u.DryMin != 130 || u.DryTemp != 65 {
		t.Errorf("unit = %+v", u)
	}
	tr := u.Trays
	if !tr[0].Bambu || tr[0].RemainG != 960 || tr[0].RemainM != 317 || tr[0].TempMin != 230 || tr[0].TempMax != 260 {
		t.Errorf("Bambu spool = %+v", tr[0])
	}
	if tr[1].Bambu || tr[1].RemainG != -1 || tr[1].RemainM != -1 {
		t.Errorf("third-party spool = %+v", tr[1])
	}
	if tr[2].RemainG != 420 { // as reported, not from a weight
		t.Errorf("remain_g = %d", tr[2].RemainG)
	}
	var noSensor map[string]any
	json.Unmarshal([]byte(`{"print":{"ams":{"ams":[{"id":"0","tray":[]}]}}}`), &noSensor)
	if h := statusFrom(noSensor).AMS[0].Humidity; h != -1 {
		t.Errorf("humidity without a sensor = %d", h)
	}
}
