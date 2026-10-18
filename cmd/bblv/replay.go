package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/marektews/bblv/internal/printer"
	"github.com/marektews/bblv/internal/recording"
)

func runReplay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	speed := fs.Float64("speed", 10, "playback speed multiplier, 0 = as fast as possible")
	fs.Usage = func() {
		fs.Output().Write([]byte("Usage: bblv replay [-speed N] <file.jsonl>\n"))
		fs.PrintDefaults()
	}
	fs.Parse(args)
	if fs.NArg() != 1 {
		fs.Usage()
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	state := printer.NewState()
	var status statusLogger
	err := recording.Replay(ctx, fs.Arg(0), *speed, func(e recording.Entry) {
		if e.Kind == "meta" {
			slog.Info("recording", "model", e.Model, "started", e.Time)
			return
		}
		if err := state.Apply(e.Payload); err != nil {
			slog.Warn("invalid report", "time", e.Time, "err", err)
			return
		}
		status.log(state.Status())
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}
