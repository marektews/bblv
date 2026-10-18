package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/kardianos/service"
)

const serviceUsage = `Usage: bblv service <command> [serve flags]

Commands:
  install     register bblv as a system service started at boot; takes the
              serve flags, e.g. bblv service install -data D:\bblv -listen :8080
  uninstall   remove the service
  start, stop, restart
  status      show whether the service is running

Installing needs an administrator prompt on Windows and root on Linux.
`

func serviceConfig(o serveOptions) *service.Config {
	return &service.Config{
		Name:        "bblv",
		DisplayName: "Bambu Live View",
		Description: "Live view of Bambu Lab 3D printers in the browser.",
		Arguments:   []string{"serve", "-data", o.dataDir, "-listen", o.listen},
		Option:      service.KeyValue{"OnFailure": "restart"},
	}
}

func runServiceCommand(args []string) error {
	if len(args) == 0 {
		fmt.Fprint(os.Stderr, serviceUsage)
		os.Exit(2)
	}
	action := args[0]
	o := parseServeFlags(args[1:])
	if o.replay != "" {
		return errors.New("-replay cannot be used for a service")
	}
	// A service does not start in the current directory.
	abs, err := filepath.Abs(o.dataDir)
	if err != nil {
		return err
	}
	o.dataDir = abs

	s, err := service.New(&program{opts: o}, serviceConfig(o))
	if err != nil {
		return err
	}
	switch action {
	case "status":
		st, err := s.Status()
		if errors.Is(err, service.ErrNotInstalled) {
			fmt.Println("not installed")
			return nil
		}
		if err != nil {
			return err
		}
		fmt.Println(map[service.Status]string{service.StatusRunning: "running", service.StatusStopped: "stopped"}[st])
		return nil
	case "install", "uninstall", "start", "stop", "restart":
		if err := service.Control(s, action); err != nil {
			return fmt.Errorf("%w (administrator or root rights are needed)", err)
		}
		if action == "install" {
			fmt.Printf("installed; data in %s, web UI on %s. Start it with: bblv service start\n", o.dataDir, o.listen)
		}
		return nil
	default:
		fmt.Fprint(os.Stderr, serviceUsage)
		os.Exit(2)
	}
	return nil
}

// runAsService runs the server under the Windows service manager or systemd.
func runAsService(o serveOptions) error {
	if runtime.GOOS == "windows" {
		// Windows services have no console; systemd keeps stderr in the journal.
		if err := os.MkdirAll(o.dataDir, 0o700); err != nil {
			return err
		}
		f, err := os.OpenFile(filepath.Join(o.dataDir, "bblv.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return err
		}
		defer f.Close()
		slog.SetDefault(slog.New(slog.NewTextHandler(f, nil)))
	}
	s, err := service.New(&program{opts: o}, serviceConfig(o))
	if err != nil {
		return err
	}
	return s.Run()
}

// program adapts serve to the service manager.
type program struct {
	opts   serveOptions
	cancel context.CancelFunc
	done   chan struct{}
}

func (p *program) Start(service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	p.done = make(chan struct{})
	go func() {
		defer close(p.done)
		if err := serve(ctx, p.opts); err != nil && ctx.Err() == nil {
			// E.g. the port is taken; exiting lets the service manager restart us.
			slog.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()
	return nil
}

func (p *program) Stop(service.Service) error {
	p.cancel()
	select {
	case <-p.done:
	case <-time.After(10 * time.Second):
	}
	return nil
}
