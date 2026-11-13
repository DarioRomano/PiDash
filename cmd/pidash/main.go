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
	"path/filepath"
	"syscall"

	"github.com/DarioRomano/PiDash/internal/apps"
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
		execApp   = flag.String("exec-app", "", "internal: replace this process with the configured app of this id")
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
	if *execApp != "" {
		// Does not return on success.
		if err := runApp(cfg, *execApp, log); err != nil {
			log.Error("exec-app", "app", *execApp, "err", err)
			os.Exit(1)
		}
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

	var appBackend apps.Backend = &apps.Mock{Log: log}
	if !*mock {
		exe, err := os.Executable()
		if err != nil {
			log.Error("locate own binary", "err", err)
			os.Exit(1)
		}
		absCfg := *cfgPath
		if absCfg != "" {
			if absCfg, err = filepath.Abs(absCfg); err != nil {
				log.Error("config path", "err", err)
				os.Exit(1)
			}
		}
		appBackend = &apps.Systemd{Exe: exe, ConfigPath: absCfg, Log: log}
	}
	appManager := apps.NewManager(cfg.Apps, appBackend, log)
	appManager.ResolveIcons(apps.DefaultIconFinder())

	srv := server.New(cfg, log, static, kiosk.New(cfg.Kiosk.Unit, *mock, log), appManager, newRunner)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Info("pidash starting", "version", version, "config", *cfgPath)
	if err := srv.Run(ctx); err != nil {
		log.Error("server", "err", err)
		os.Exit(1)
	}
}

// runApp replaces this process with the command of the configured app. It is
// what the systemd units for both launch modes execute, so an app's command
// lives only in config.toml.
func runApp(cfg config.Config, id string, log *slog.Logger) error {
	for _, a := range cfg.Apps {
		if a.ID != id {
			continue
		}
		env := os.Environ()
		if a.Mode == config.AppModeConsole {
			env = apps.ConsoleEnv(env)
		} else {
			env = apps.DesktopEnv(env)
		}
		if a.URL != "" {
			// A website: open it fullscreen in the app's own browser profile.
			dataDir, err := apps.DataDir()
			if err != nil {
				return err
			}
			launch, err := apps.PrepareWebApp(a, dataDir, env, log)
			if err != nil {
				return err
			}
			return syscall.Exec(launch.Argv[0], launch.Argv, launch.Env)
		}
		return syscall.Exec("/bin/sh", []string{"sh", "-c", a.Command}, env)
	}
	return fmt.Errorf("no app with id %q in the config", id)
}
