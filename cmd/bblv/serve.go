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
	"strings"
	"syscall"
	"time"

	"github.com/kardianos/service"

	"github.com/marektews/bblv/internal/auth"
	"github.com/marektews/bblv/internal/cache"
	"github.com/marektews/bblv/internal/config"
	"github.com/marektews/bblv/internal/discovery"
	"github.com/marektews/bblv/internal/monitor"
	"github.com/marektews/bblv/internal/printjob"
	"github.com/marektews/bblv/internal/relocate"
	"github.com/marektews/bblv/internal/server"
)

type serveOptions struct {
	dataDir     string
	listen      string
	allowHosts  string // comma-separated
	replay      string
	replayModel string
	speed       float64
}

func parseServeFlags(args []string) serveOptions {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	var o serveOptions
	fs.StringVar(&o.dataDir, "data", config.DefaultDataDir(), "data directory")
	fs.StringVar(&o.listen, "listen", ":8080", "HTTP listen address")
	fs.StringVar(&o.allowHosts, "allow-host", "", "comma-separated host names, besides addresses and local names, under which the web interface may be opened, e.g. that of a reverse proxy")
	fs.StringVar(&o.replay, "replay", "", "serve a virtual printer replaying this recording instead of the configured printers")
	fs.StringVar(&o.replayModel, "replay-model", "", "with -replay: .gcode or .3mf file used as the geometry")
	fs.Float64Var(&o.speed, "speed", 1, "with -replay: playback speed multiplier")
	fs.Parse(args)
	return o
}

func runServe(args []string) error {
	o := parseServeFlags(args)
	if !service.Interactive() {
		return runAsService(o)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serve(ctx, o)
}

// splitList splits a comma-separated flag value, dropping empty items.
func splitList(s string) []string {
	var out []string
	for _, item := range strings.Split(s, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// serve runs the web server until ctx is cancelled.
func serve(ctx context.Context, o serveOptions) error {
	store, err := cache.Open(filepath.Join(o.dataDir, "cache"), cache.DefaultLimit)
	if err != nil {
		return err
	}
	cfgStore, err := config.OpenStore(o.dataDir)
	if err != nil {
		return err
	}
	if err = config.ProtectDir(o.dataDir); err != nil {
		slog.Warn("other users of this computer may read the data directory", "dir", o.dataDir, "err", err)
	}
	ffmpeg, _ := exec.LookPath("ffmpeg")
	mon := monitor.New(store, ffmpeg)
	if err = mon.KeepFilamentNames(filepath.Join(o.dataDir, "filaments.json")); err != nil {
		slog.Warn("filament names not loaded", "err", err)
	}
	if o.replay != "" {
		if err = mon.AddReplay(o.replay, o.speed); err != nil {
			return err
		}
		if o.replayModel != "" {
			var res *printjob.Result
			if res, err = printjob.BuildFile(o.replayModel, 0, store); err != nil {
				return err
			}
			mon.Upload("replay", res)
		}
	} else {
		cfg := cfgStore.Get()
		if len(cfg.Printers) == 0 {
			slog.Warn("no printers configured, add them in the settings", "config", filepath.Join(o.dataDir, config.FileName))
		}
		for _, p := range cfg.Printers {
			mon.AddPrinter(p)
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go mon.Run(ctx)
	disc := discovery.Listen(ctx)
	if o.replay == "" {
		// Follows printers that got another address from DHCP.
		go relocate.New(cfgStore, mon, disc, filepath.Join(o.dataDir, "printer-ca.pem")).Run(ctx)
	}

	srv := &http.Server{
		Addr:              o.listen,
		Handler:           server.New(server.Deps{Monitor: mon, Config: cfgStore, Auth: auth.New(cfgStore), Discovery: disc, AllowedHosts: splitList(o.allowHosts)}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// Cancels long-lived event streams on shutdown.
		BaseContext: func(net.Listener) context.Context { return ctx },
	}
	go func() {
		<-ctx.Done()
		shutdown, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		srv.Shutdown(shutdown)
	}()
	slog.Info("listening", "addr", o.listen, "data", o.dataDir, "version", version)
	if err = srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
