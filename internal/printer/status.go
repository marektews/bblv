package printer

import (
	"fmt"
	"math"
	"strconv"
)

// Status is a typed view of the fields of the merged printer state used by the UI.
type Status struct {
	State        string  `json:"state"` // gcode_state: IDLE, PREPARE, RUNNING, PAUSE, FINISH, FAILED
	Stage        int     `json:"stage"` // stg_cur
	Percent      int     `json:"percent"`
	RemainingMin int     `json:"remainingMin"`
	Layer        int     `json:"layer"`
	TotalLayers  int     `json:"totalLayers"`
	NozzleTemp   float64 `json:"nozzleTemp"`
	NozzleTarget float64 `json:"nozzleTarget"`
	BedTemp      float64 `json:"bedTemp"`
	BedTarget    float64 `json:"bedTarget"`
	GcodeFile    string  `json:"gcodeFile"`
	Subtask      string  `json:"subtask"`
	TaskID       string  `json:"taskId"`
	SpeedLevel   int     `json:"speedLevel"`
	WifiSignal   string  `json:"wifiSignal"`
	PrintError   int     `json:"printError"`
	HMS          []HMS   `json:"hms"`
	Firmware     string  `json:"firmware"`
	Fans         Fans    `json:"fans"`
	AMS          []AMS   `json:"ams"`
	ActiveTray   int     `json:"activeTray"` // unit*4+slot, 254 = external spool, -1 = none
	External     *Tray   `json:"external"`
	SDCard       bool    `json:"sdCard"`
	SDTotalKB    int     `json:"sdTotalKb"` // 0 when not reported
	SDFreeKB     int     `json:"sdFreeKb"`
	// LANLiveView is "enabled" or "disabled" for printers with an RTSP
	// camera, which report ipcam.rtsp_url, and "" for the others.
	LANLiveView string `json:"lanLiveView"`
	RTSPURL     string `json:"-"` // ipcam.rtsp_url: "disable" or the stream address
}

// Fans holds fan speeds in percent.
type Fans struct {
	Part      int `json:"part"`
	Aux       int `json:"aux"`
	Chamber   int `json:"chamber"`
	Heatbreak int `json:"heatbreak"`
}

// AMS is one AMS unit.
type AMS struct {
	ID    int    `json:"id"`
	Trays []Tray `json:"trays"`
}

// Tray is one filament slot; Type is empty for an empty slot.
type Tray struct {
	ID     int    `json:"id"`
	Type   string `json:"type"`
	Name   string `json:"name"`
	Color  string `json:"color"`  // #RRGGBB
	Remain int    `json:"remain"` // percent, -1 when unknown
}

// HMS is a single Health Management System entry reported by the printer.
type HMS struct {
	Attr uint32
	Code uint32
}

// MarshalJSON encodes the entry as its code string.
func (h HMS) MarshalJSON() ([]byte, error) {
	return []byte(`"` + h.String() + `"`), nil
}

// String formats the entry the way Bambu documents it, e.g. 0300_0100_0001_0007.
func (h HMS) String() string {
	return fmt.Sprintf("%04X_%04X_%04X_%04X", h.Attr>>16, h.Attr&0xFFFF, h.Code>>16, h.Code&0xFFFF)
}

func statusFrom(state map[string]any) Status {
	p := obj(state, "print")
	s := Status{
		State:        str(p, "gcode_state"),
		Stage:        int(num(p, "stg_cur")),
		Percent:      int(num(p, "mc_percent")),
		RemainingMin: int(num(p, "mc_remaining_time")),
		Layer:        int(num(p, "layer_num")),
		TotalLayers:  int(num(p, "total_layer_num")),
		NozzleTemp:   num(p, "nozzle_temper"),
		NozzleTarget: num(p, "nozzle_target_temper"),
		BedTemp:      num(p, "bed_temper"),
		BedTarget:    num(p, "bed_target_temper"),
		GcodeFile:    str(p, "gcode_file"),
		Subtask:      str(p, "subtask_name"),
		TaskID:       str(p, "task_id"),
		SpeedLevel:   int(num(p, "spd_lvl")),
		WifiSignal:   str(p, "wifi_signal"),
		PrintError:   int(num(p, "print_error")),
		HMS:          []HMS{},
	}
	if list, ok := p["hms"].([]any); ok {
		for _, it := range list {
			if h, ok := it.(map[string]any); ok {
				s.HMS = append(s.HMS, HMS{Attr: uint32(num(h, "attr")), Code: uint32(num(h, "code"))})
			}
		}
	}
	// Fan speeds are reported as levels 0-15.
	fan := func(key string) int { return int(math.Round(num(p, key) / 15 * 100)) }
	s.Fans = Fans{Part: fan("cooling_fan_speed"), Aux: fan("big_fan1_speed"), Chamber: fan("big_fan2_speed"), Heatbreak: fan("heatbreak_fan_speed")}

	ams := obj(p, "ams")
	s.ActiveTray = -1
	if now, err := strconv.Atoi(str(ams, "tray_now")); err == nil && now != 255 {
		s.ActiveTray = now
	}
	s.AMS = []AMS{}
	if units, ok := ams["ams"].([]any); ok {
		for _, it := range units {
			u, ok := it.(map[string]any)
			if !ok {
				continue
			}
			unit := AMS{ID: int(num(u, "id")), Trays: []Tray{}}
			if trays, ok := u["tray"].([]any); ok {
				for _, t := range trays {
					if t, ok := t.(map[string]any); ok {
						unit.Trays = append(unit.Trays, trayFrom(t))
					}
				}
			}
			s.AMS = append(s.AMS, unit)
		}
	}
	if vt := obj(p, "vt_tray"); vt != nil {
		t := trayFrom(vt)
		s.External = &t
	}

	// The SD card holds the timelapses, so its size is reported with the camera.
	s.SDCard, _ = p["sdcard"].(bool)
	ipcam := obj(p, "ipcam")
	s.SDTotalKB, s.SDFreeKB = int(num(ipcam, "tl_external_total_kb")), int(num(ipcam, "tl_external_free_kb"))
	switch s.RTSPURL = str(ipcam, "rtsp_url"); s.RTSPURL {
	case "":
	case "disable":
		s.LANLiveView = "disabled"
	default:
		s.LANLiveView = "enabled"
	}

	if mods, ok := obj(state, "info")["module"].([]any); ok {
		for _, it := range mods {
			if m, ok := it.(map[string]any); ok && str(m, "name") == "ota" {
				s.Firmware = str(m, "sw_ver")
			}
		}
	}
	return s
}

func trayFrom(t map[string]any) Tray {
	tray := Tray{ID: int(num(t, "id")), Type: str(t, "tray_type"), Name: str(t, "tray_sub_brands"), Remain: -1}
	if c := str(t, "tray_color"); len(c) >= 6 {
		tray.Color = "#" + c[:6]
	}
	// Only spools identified by RFID (tray_id_name set) report a meaningful remain.
	if r, ok := t["remain"].(float64); ok && r >= 0 && str(t, "tray_id_name") != "" {
		tray.Remain = int(r)
	}
	return tray
}

// merge deep-merges src into dst. Objects are merged key by key; any other
// value (arrays included) replaces the previous one.
func merge(dst, src map[string]any) {
	for k, v := range src {
		if sm, ok := v.(map[string]any); ok {
			if dm, ok := dst[k].(map[string]any); ok {
				merge(dm, sm)
				continue
			}
		}
		dst[k] = v
	}
}

func obj(m map[string]any, key string) map[string]any {
	o, _ := m[key].(map[string]any)
	return o
}

func str(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

// num reads a number that the printer may send either as a JSON number or as a string.
func num(m map[string]any, key string) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	}
	return 0
}
