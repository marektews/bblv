package printer

import (
	"fmt"
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
	if mods, ok := obj(state, "info")["module"].([]any); ok {
		for _, it := range mods {
			if m, ok := it.(map[string]any); ok && str(m, "name") == "ota" {
				s.Firmware = str(m, "sw_ver")
			}
		}
	}
	return s
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
