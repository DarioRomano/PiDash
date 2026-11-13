// Command pidash serves the TV dashboard: embedded frontend, widget data over
// Server-Sent Events, and the phone remote API.
package main

import (
	"context"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/DarioRomano/PiDash/internal/config"
	"github.com/DarioRomano/PiDash/internal/kiosk"
	"github.com/DarioRomano/PiDash/internal/providers"
	"github.com/DarioRomano/PiDash/internal/providers/system"
	"github.com/DarioRomano/PiDash/internal/server"
	"github.com/DarioRomano/PiDash/web"
)

var version = "dev"

func main() {
	var (
		cfgPath   = flag.String("config", "", "path to config.toml (defaults are used when empty)")
		listen    = flag.String("listen", "", "override server.listen, e.g. :8080")
		staticDir = flag.String("static", "", "serve the frontend from this directory instead of the embedded build")
		mock      = flag.Bool("mock", false, "use canned data for providers that need the network")
		verbose   = flag.Bool("v", false, "debug logging")
		showVer   = flag.Bool("version", false, "print version and exit")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("pidash", version)
		return
	}

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Error("config", "err", err)
		os.Exit(2)
	}
	if *listen != "" {
		cfg.Server.Listen = *listen
	}

	var static fs.FS
	if *staticDir != "" {
		static = os.DirFS(*staticDir)
	} else {
		static, err = fs.Sub(web.Dist, "dist")
		if err != nil {
			log.Error("embedded frontend", "err", err)
			os.Exit(1)
		}
	}

	newRunner := func(publish func(providers.Entry)) *providers.Runner {
		var ps []providers.Provider
		if cfg.System.Enabled {
			ps = append(ps, system.New(cfg.System.Interval.Duration))
		}
		return providers.NewRunner(log, publish, ps...)
	}

	srv := server.New(cfg, log, static, kiosk.New(cfg.Kiosk.Unit, *mock, log), newRunner)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("pidash starting", "version", version, "config", *cfgPath)
	if err := srv.Run(ctx); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}
