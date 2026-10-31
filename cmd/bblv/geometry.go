package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/marektews/bblv/internal/cache"
	"github.com/marektews/bblv/internal/config"
	"github.com/marektews/bblv/internal/ftps"
	"github.com/marektews/bblv/internal/gcode"
	"github.com/marektews/bblv/internal/printer"
	"github.com/marektews/bblv/internal/printjob"
)

func runGeometry(args []string) error {
	fs := flag.NewFlagSet("geometry", flag.ExitOnError)
	dataDir := fs.String("data", config.DefaultDataDir(), "data directory with config.json")
	id := fs.String("printer", "", "printer id from config.json (optional when only one is configured)")
	subtask := fs.String("subtask", "", "print name on the SD card (default: the current print)")
	file := fs.String("file", "", "parse a local .gcode or .3mf file instead of using the printer")
	plate := fs.Int("plate", 0, "plate number for -file with a .3mf (0 = the only one)")
	out := fs.String("out", "", "with -file: write the encoded geometry to this file")
	fs.Parse(args)

	if *file != "" {
		start := time.Now()
		m, png, err := printjob.ParseFile(*file, *plate)
		if err != nil {
			return err
		}
		logModel(m, time.Since(start))
		slog.Info("thumbnail", "bytes", len(png))
		if *out == "" {
			return nil
		}
		f, err := os.Create(*out)
		if err != nil {
			return err
		}
		if err = m.Encode(f); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	}

	cfg, err := config.Load(*dataDir)
	if err != nil {
		return err
	}
	p, err := cfg.Printer(*id)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	name, gcodeFile := *subtask, ""
	if name == "" {
		var s printer.Status
		s, err = currentPrint(ctx, *p)
		if err != nil {
			return err
		}
		name, gcodeFile = s.Subtask, s.GcodeFile
	}
	conn, err := ftps.Dial(ctx, p.Host, p.AccessCode, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	files, err := printjob.Find(conn, name, gcodeFile)
	if err != nil {
		return err
	}
	slog.Info("print files", "plate", files.Plate, "3mf", files.ThreeMF, "gcode", files.Gcode, "ams_mapping", files.AMSMapping)

	store, err := cache.Open(filepath.Join(*dataDir, "cache"), cache.DefaultLimit)
	if err != nil {
		return err
	}
	start := time.Now()
	res, err := printjob.Build(conn, files, store)
	if err != nil {
		return err
	}
	if res.Model != nil {
		logModel(res.Model, time.Since(start))
	} else {
		slog.Info("geometry served from cache")
	}
	if info, statErr := os.Stat(res.Geometry); statErr == nil {
		slog.Info("cached", "geometry", res.Geometry, "bytes", info.Size(), "thumbnail", res.Thumbnail)
	}
	return nil
}

// currentPrint connects to the printer and waits for a report naming the current print.
func currentPrint(ctx context.Context, p config.Printer) (printer.Status, error) {
	found := make(chan printer.Status, 1)
	var c *printer.Client
	c = printer.New(p, nil, func(printer.Report) {
		if s := c.Status(); s.Subtask != "" {
			select {
			case found <- s:
			default:
			}
		}
	})
	if err := c.Connect(ctx); err != nil {
		return printer.Status{}, err
	}
	defer c.Close()
	select {
	case s := <-found:
		return s, nil
	case <-time.After(20 * time.Second):
		return printer.Status{}, errors.New("printer did not report a print name, use -subtask")
	case <-ctx.Done():
		return printer.Status{}, ctx.Err()
	}
}

func logModel(m *gcode.Model, took time.Duration) {
	slog.Info("geometry",
		"layers", len(m.Layers), "segments", m.Segments, "points", len(m.Points),
		"features", m.Features, "filaments", m.Filaments, "bounds", m.Bounds, "took", took.Round(time.Millisecond))
}
