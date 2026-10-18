package main

import (
	"fmt"
	"log/slog"

	"github.com/marektews/bblv/internal/printer"
)

// statusLogger logs the fields shown to the user whenever they change.
type statusLogger struct {
	last printer.Status
}

func (l *statusLogger) log(s printer.Status) {
	if s.Firmware != "" && s.Firmware != l.last.Firmware {
		slog.Info("firmware", "version", s.Firmware)
	}
	if s.GcodeFile != l.last.GcodeFile || s.Subtask != l.last.Subtask {
		slog.Info("print file", "gcode_file", s.GcodeFile, "subtask", s.Subtask)
	}
	if s.State != l.last.State || s.Stage != l.last.Stage || s.Layer != l.last.Layer || s.Percent != l.last.Percent || len(s.HMS) != len(l.last.HMS) {
		slog.Info("status",
			"state", s.State, "stage", s.Stage, "percent", s.Percent,
			"layer", fmt.Sprintf("%d/%d", s.Layer, s.TotalLayers),
			"nozzle", fmt.Sprintf("%.0f/%.0f", s.NozzleTemp, s.NozzleTarget),
			"bed", fmt.Sprintf("%.0f/%.0f", s.BedTemp, s.BedTarget),
			"remaining_min", s.RemainingMin, "hms", s.HMS)
	}
	l.last = s
}
