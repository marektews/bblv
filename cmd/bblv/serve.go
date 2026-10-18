package main

import (
	"context"
	"errors"
	"flag"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/marektews/bblv/internal/auth"
	"github.com/marektews/bblv/internal/cache"
	"github.com/marektews/bblv/internal/config"
	"github.com/marektews/bblv/internal/discovery"
	"github.com/marektews/bblv/internal/monitor"
	"github.com/marektews/bblv/internal/printjob"
	"github.com/marektews/bblv/internal/server"
)

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dataDir := fs.String("data", config.DefaultDataDir(), "data directory")
	listen := fs.String("listen", ":8080", "HTTP listen address")
	replay := fs.String("replay", "", "serve a virtual printer replaying this recording instead of the configured printers")
	replayModel := fs.String("replay-model", "", "with -replay: .gcode or .3mf file used as the geometry")
	speed := fs.Float64("speed", 1, "with -replay: playback speed multiplier")
	fs.Parse(args)

	store, err := cache.Open(filepath.Join(*dataDir, "cache"), cache.DefaultLimit)
	if err != nil {
		return err
	}
	cfgStore, err := config.OpenStore(*dataDir)
	if err != nil {
		return err
	}
	ffmpeg, _ := exec.LookPath("ffmpeg")
	mon := monitor.New(store, ffmpeg)
	if *replay != "" {
		if err := mon.AddReplay(*replay, *speed); err != nil {
			return err
		}
		if *replayModel != "" {
			res, err := printjob.BuildFile(*replayModel, 0, store)
			if err != nil {
				return err
			}
			mon.Upload("replay", res)
		}
	} else {
		cfg := cfgStore.Get()
		if len(cfg.Printers) == 0 {
			slog.Warn("no printers configured", "config", filepath.Join(*dataDir, config.FileName))
		}
		for _, p := range cfg.Printers {
			mon.AddPrinter(p)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go mon.Run(ctx)
	disc := discovery.Listen(ctx)

	srv := &http.Server{
		Addr:              *listen,
		Handler:           server.New(server.Deps{Monitor: mon, Config: cfgStore, Auth: auth.New(cfgStore), Discovery: disc}),
		ReadHeaderTimeout: 10 * time.Second,
		// Cancels long-lived event streams on shutdown.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(shutdown)
	}()
	slog.Info("listening", "addr", *listen)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
