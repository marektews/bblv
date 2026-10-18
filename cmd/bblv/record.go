package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/marektews/bblv/internal/config"
	"github.com/marektews/bblv/internal/printer"
	"github.com/marektews/bblv/internal/recording"
)

func runRecord(args []string) error {
	fs := flag.NewFlagSet("record", flag.ExitOnError)
	dataDir := fs.String("data", config.DefaultDataDir(), "data directory with config.json")
	id := fs.String("printer", "", "printer id from config.json (optional when only one is configured)")
	out := fs.String("out", "", "output file (default <data>/recordings/<id>-<time>.jsonl)")
	raw := fs.Bool("raw", false, "keep serial numbers, spool ids and network addresses (do not share such files)")
	fs.Parse(args)

	cfg, err := config.Load(*dataDir)
	if err != nil {
		return err
	}
	if len(cfg.Printers) == 0 {
		return fmt.Errorf("no printers configured in %s", filepath.Join(*dataDir, config.FileName))
	}
	p, err := cfg.Printer(*id)
	if err != nil {
		return err
	}
	if *out == "" {
		*out = filepath.Join(*dataDir, "recordings", fmt.Sprintf("%s-%s.jsonl", p.ID, time.Now().Format("20060102-150405")))
	}

	var san *recording.Sanitizer
	if !*raw {
		san = recording.NewSanitizer(p.Serial)
	}
	rec, err := recording.Create(*out, p.Model, san)
	if err != nil {
		return err
	}
	defer rec.Close()

	var client *printer.Client
	var status statusLogger
	client = printer.New(*p, func(r printer.Report) {
		if err := rec.Write(r.Time, r.Payload); err != nil {
			slog.Error("write recording", "err", err)
		}
		status.log(client.Status())
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := client.Connect(ctx); err != nil {
		return err
	}
	defer client.Close()

	slog.Info("recording, press Ctrl+C to stop", "file", *out)
	<-ctx.Done()
	return nil
}
