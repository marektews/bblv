package main

import (
	"context"
	"crypto/tls"
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
	"github.com/marektews/bblv/internal/notify"
	"github.com/marektews/bblv/internal/printjob"
	"github.com/marektews/bblv/internal/relocate"
	"github.com/marektews/bblv/internal/server"
	"github.com/marektews/bblv/internal/tlscert"
	"github.com/marektews/bblv/internal/trust"
)

type serveOptions struct {
	dataDir     string
	listen      string
	https       string // "" serves plain HTTP on listen only
	allowHosts  string // comma-separated
	user        string // with service install: the account the service runs as
	replay      string
	replayModel string
	speed       float64
}

func parseServeFlags(args []string) serveOptions {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	var o serveOptions
	fs.StringVar(&o.dataDir, "data", config.DefaultDataDir(), "data directory")
	fs.StringVar(&o.listen, "listen", ":8080", "HTTP listen address; with -https it redirects there")
	fs.StringVar(&o.https, "https", ":8443", "HTTPS listen address, with a self-signed certificate kept in the data directory; -https= serves plain HTTP on -listen only")
	fs.StringVar(&o.user, "user", "", "with bblv service install on Linux: the account the service runs as instead of root; it becomes the owner of the data directory")
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
	// The CAs of the printers, learned on their first connection; the
	// printers are pinned to them.
	cas, err := trust.Load(filepath.Join(o.dataDir, printerCAFile))
	if err != nil {
		slog.Error("printer CAs not loaded", "err", err)
	}
	mon.TrustPrinters(cas)
	// Notifications reach the open pages with the events and, when set up,
	// an ntfy topic, which the ntfy app shows on a phone.
	ntfy := notify.NewNtfy(func() config.Ntfy { return cfgStore.Get().Ntfy })
	mon.OnNotice(ntfy.Send)
	if err = mon.KeepFilamentNames(filepath.Join(o.dataDir, "filaments.json")); err != nil {
		slog.Warn("filament names not loaded", "err", err)
	}
	if err = mon.KeepDismissedAlerts(filepath.Join(o.dataDir, "dismissed-alerts.json")); err != nil {
		slog.Warn("dismissed alerts not loaded", "err", err)
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
		if len(cfgStore.Get().Printers) == 0 {
			slog.Warn("no printers configured, add them in the settings", "config", filepath.Join(o.dataDir, config.FileName))
		}
		// Every saved change reaches the monitor in the order of the saves,
		// also when settings and the relocator change printers at once.
		// Printers removed or given another serial number are unpinned.
		cfgStore.Watch(func(c config.Config) {
			mon.SyncPrinters(c.Printers)
			serials := make([]string, len(c.Printers))
			for i, p := range c.Printers {
				serials[i] = p.Serial
			}
			cas.Retain(serials)
		})
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	monitorDone := make(chan struct{})
	go func() {
		mon.Run(ctx)
		close(monitorDone)
	}()
	go ntfy.Run(ctx)
	disc := discovery.Listen(ctx)
	if o.replay == "" {
		// Follows printers that got another address from DHCP.
		go relocate.New(cfgStore, mon, disc, cas).Run(ctx)
	}

	newServer := func(addr string, h http.Handler) *http.Server {
		return &http.Server{
			Addr:              addr,
			Handler:           h,
			ReadHeaderTimeout: 10 * time.Second,
			IdleTimeout:       2 * time.Minute,
			// Without pings, an HTTP/2 connection of a device that left the
			// network stays open until TCP gives up.
			HTTP2: &http.HTTP2Config{SendPingTimeout: 30 * time.Second, PingTimeout: 15 * time.Second},
			// Cancels long-lived event streams on shutdown.
			BaseContext: func(net.Listener) context.Context { return ctx },
		}
	}
	scheme, addr := "https", o.https
	if o.https == "" {
		scheme, addr = "http", o.listen
	}
	app := server.New(server.Deps{
		Monitor: mon, Config: cfgStore, Auth: auth.New(cfgStore), Discovery: disc, AllowedHosts: splitList(o.allowHosts), Version: version,
		LANAddress: func() string { return lanAddress(scheme, addr, tlscert.PrimaryIPv4()) },
	})
	var servers []*http.Server
	if o.https == "" {
		servers = append(servers, newServer(o.listen, app))
		slog.Info("listening", "addr", o.listen, "data", o.dataDir, "version", version)
	} else {
		// Keeps the password and the session cookie off the network in
		// clear text; the plain address redirects.
		certs, certErr := tlscert.NewKeeper(o.dataDir, splitList(o.allowHosts))
		if certErr != nil {
			return certErr
		}
		// A server running for over a year still gets a valid certificate.
		go certs.Run(ctx)
		secure := newServer(o.https, app)
		secure.TLSConfig = &tls.Config{GetCertificate: certs.GetCertificate, MinVersion: tls.VersionTLS12}
		servers = append(servers, secure, newServer(o.listen, server.RedirectToHTTPS(o.https)))
		slog.Info("listening", "https", o.https, "redirect", o.listen, "data", o.dataDir, "version", version)
	}

	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		<-ctx.Done()
		shutdown, stop := context.WithTimeout(context.Background(), shutdownTimeout)
		defer stop()
		for _, s := range servers {
			s.Shutdown(shutdown)
		}
	}()
	errs := make(chan error, len(servers))
	for _, s := range servers {
		go func() {
			if s.TLSConfig != nil {
				errs <- s.ListenAndServeTLS("", "")
			} else {
				errs <- s.ListenAndServe()
			}
		}()
	}
	// A server that cannot start, e.g. on a port in use, stops the others.
	var failed error
	for range servers {
		if serveErr := <-errs; !errors.Is(serveErr, http.ErrServerClosed) && failed == nil {
			failed = serveErr
			cancel()
		}
	}
	// Lets the requests in progress finish and the printers disconnect,
	// which also stops the camera streams and their ffmpeg processes.
	<-serverDone
	select {
	case <-monitorDone:
		if failed == nil {
			slog.Info("stopped")
		}
	case <-time.After(shutdownTimeout):
		slog.Warn("printers did not disconnect in time")
	}
	return failed
}

// lanAddress returns the address other devices in the network reach a
// server listening on addr at, given the computer's address in the network;
// "" for a server listening only on a loopback address or the computer
// without an address.
func lanAddress(scheme, addr, primary string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return ""
	}
	ip := net.ParseIP(host)
	switch {
	case host == "" || ip != nil && ip.IsUnspecified():
		host = primary
	case ip != nil && ip.IsLoopback(), strings.EqualFold(host, "localhost"):
		return ""
	}
	if host == "" {
		return ""
	}
	return scheme + "://" + net.JoinHostPort(host, port)
}

// shutdownTimeout is how long stopping waits for the requests in progress
// and, after them, for the printers.
const shutdownTimeout = 5 * time.Second
