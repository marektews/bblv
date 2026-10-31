package server

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/marektews/bblv/internal/monitor"
	"github.com/marektews/bblv/internal/printer"
)

// light switches the chamber light of a printer: {"on": true}. The new
// mode reaches the pages with the printer's next report.
func (s *server) light(w http.ResponseWriter, r *http.Request) {
	var in struct {
		On bool `json:"on"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	id := r.PathValue("id")
	err := s.mon.SetLight(id, in.On)
	switch {
	case err == nil:
		slog.Info("light switched", "printer", id, "on", in.On)
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, monitor.ErrUnknownPrinter):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, monitor.ErrNotSupported), errors.Is(err, printer.ErrNotConnected):
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusBadGateway, err.Error())
	}
}
