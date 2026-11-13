// Command pidash serves the TV dashboard: embedded frontend, widget data over
// Server-Sent Events, and the phone remote API.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/DarioRomano/PiDash/internal/apps"
	"github.com/DarioRomano/PiDash/internal/cec"
	"github.com/DarioRomano/PiDash/internal/config"
	"github.com/DarioRomano/PiDash/internal/input"
	"github.com/DarioRomano/PiDash/internal/kiosk"
	"github.com/DarioRomano/PiDash/internal/photos"
	"github.com/DarioRomano/PiDash/internal/providers"
	"github.com/DarioRomano/PiDash/internal/providers/system"
	"github.com/DarioRomano/PiDash/internal/providers/weather"
	"github.com/DarioRomano/PiDash/internal/server"
	"github.com/DarioRomano/PiDash/internal/settings"
	"github.com/DarioRomano/PiDash/internal/tv"
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
		cecCmd    = flag.String("cec", "", "talk to the TV over HDMI-CEC and exit: setup, on, off or status")
		checkCfg  = flag.Bool("check-config", false, "check the settings file given with -config and exit")
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

	if *cecCmd != "" {
		os.Exit(runCEC(*cecCmd))
	}

	cfg, err := config.Load(*cfgPath)
	if *checkCfg {
		if err != nil {
			fmt.Println("Problem in the settings:", err)
			os.Exit(2)
		}
		fmt.Println("The settings file is fine.")
		return
	}
	// Things the person at the TV should be told about.
	var notices []string
	if err != nil {
		if *execApp != "" {
			log.Error("config", "err", err)
			os.Exit(2)
		}
		// A typo in the settings must not leave the TV dark with no hint why:
		// run with the built-in settings and say what is wrong on screen.
		log.Error("config: running with default settings until this is fixed", "err", err)
		notices = append(notices, "There is a mistake in the settings file, so PiDash is running with its default settings until it is fixed: "+err.Error())
		cfg = config.Default()
	}

	// What was saved on the settings page is laid over config.toml.
	baseListen := cfg.Server.Listen
	store, err := settings.Open(settings.Path(*cfgPath), cfg)
	if err != nil {
		log.Error("settings", "err", err)
		notices = append(notices, "The saved settings could not be read: "+err.Error())
	}
	if effective, err := store.Effective(); err != nil {
		log.Error("saved settings do not fit config.toml; ignoring them", "err", err)
		notices = append(notices, "The settings saved on the settings page no longer fit config.toml and were ignored: "+err.Error())
	} else {
		cfg = effective
	}
	if *execApp != "" {
		// Does not return on success.
		if err := runApp(cfg, *execApp, log); err != nil {
			log.Error("exec-app", "app", *execApp, "err", err)
			os.Exit(1)
		}
	}
	if *listen != "" {
		cfg.Server.Listen, baseListen = *listen, *listen
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
		switch w := cfg.Weather; {
		case w.Enabled():
			statePath := ""
			if dir, err := apps.DataDir(); err == nil {
				statePath = filepath.Join(dir, "weather-location.json")
			}
			ps = append(ps, weather.New(weather.Options{
				Location: w.Location, Latitude: w.Latitude, Longitude: w.Longitude, Name: w.Name,
				Units: w.Units, Interval: w.Interval.Duration, StatePath: statePath,
			}))
		case *mock:
			ps = append(ps, weather.Mock{})
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

	photoService, err := newPhotoService(cfg.Photos, *mock, log)
	if err != nil {
		log.Error("photos", "err", err)
		os.Exit(1)
	}

	// TV power schedule over HDMI-CEC.
	cecControl := cec.New()
	tvOpts := tv.Options{TV: cecControl, Log: log, AppRunning: func() bool { return appManager.Status().Running != "" }}
	for _, c := range []struct {
		value string
		dst   **tv.Clock
	}{{cfg.TV.Off, &tvOpts.Off}, {cfg.TV.On, &tvOpts.On}} {
		if c.value == "" {
			continue
		}
		clock, err := tv.ParseClock(c.value)
		if err != nil {
			log.Error("config", "err", err)
			os.Exit(2)
		}
		*c.dst = &clock
	}
	tvService := tv.New(tvOpts)

	// The phone as mouse and keyboard, and the TV remote's long-press exit.
	phoneInput := input.NewService(cfg.Remote.MouseKeyboard, log)
	defer phoneInput.Close()
	// Keys that leave an app when held: the built-in ones (the remote's
	// Back/Exit, a keyboard's Esc) plus any added in the settings.
	exitKeys := input.DefaultExitKeys()
	for _, name := range cfg.Input.AppExitKeys {
		code, err := input.ParseKey(name)
		if err != nil {
			log.Error("config: input.app_exit_keys", "err", err)
			notices = append(notices, "Settings: input.app_exit_keys: "+err.Error())
			continue
		}
		exitKeys = append(exitKeys, code)
	}
	// What the TV remote's buttons do on the dashboard.
	remoteActions := input.DefaultRemoteActions()
	for name, action := range cfg.Input.RemoteKeys {
		code, err := input.ParseKey(name)
		if err != nil {
			log.Error("config: input.remote_keys", "err", err)
			notices = append(notices, "Settings: input.remote_keys: "+err.Error())
			continue
		}
		if action = strings.ToLower(strings.TrimSpace(action)); action == "" || action == "none" {
			delete(remoteActions, code)
		} else {
			remoteActions[code] = action
		}
	}

	srv := server.New(cfg, log, static, server.Deps{
		Kiosk: kiosk.New(cfg.Kiosk.Unit, *mock, log), Apps: appManager, Photos: photoService,
		Input: phoneInput, TV: tvService, Version: version, NewRunner: newRunner,
		Notices: notices, URLFile: server.URLFile(), Settings: store, BaseListen: baseListen,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !*mock {
		remote := &input.Remote{
			Actions: remoteActions, ExitKeys: exitKeys, Hold: cfg.Input.AppExitHold.Duration, Log: log,
			AppRunning: func() bool { return appManager.Status().Running != "" },
			OnAction:   srv.RemoteAction,
			OnAppExit: func() {
				log.Info("Back held on the remote: closing the app")
				if err := appManager.Stop(context.Background()); err != nil {
					log.Warn("closing the app", "err", err)
				}
			},
			Tap: phoneInput.Tap,
		}
		go remote.Run(ctx)
		// Answer the TV's questions, so it keeps sending the remote's keys
		// here after it was switched to another input and back.
		go cecControl.Follow(ctx, log)
	}

	log.Info("pidash starting", "version", version, "config", *cfgPath)
	err = srv.Run(ctx)
	if errors.Is(err, server.ErrRestart) {
		// Start again in place to apply saved settings. Under systemd the
		// service simply keeps running, with the same process id.
		phoneInput.Close()
		stop()
		exe, exeErr := os.Executable()
		if exeErr == nil {
			log.Info("restarting to apply the saved settings")
			exeErr = syscall.Exec(exe, os.Args, os.Environ())
		}
		log.Error("could not restart in place", "err", exeErr)
		os.Exit(1) // systemd starts it again
	}
	if err != nil {
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

// newPhotoService builds the background slideshow from the config. It returns
// nil when no photo source is configured.
func newPhotoService(cfg config.Photos, mock bool, log *slog.Logger) (*photos.Service, error) {
	var src photos.Source
	name := "photos"
	switch cfg.Source {
	case config.PhotoSourceSynology:
		src = photos.NewSynology(photos.SynologyOptions{
			URL: cfg.Synology.URL, Username: cfg.Synology.Username, Password: cfg.Synology.Password,
			InsecureTLS: cfg.Synology.InsecureTLS,
		})
	case config.PhotoSourceFolder:
		src = photos.NewLocalDir(cfg.Folder.Path)
	default:
		if !mock {
			return nil, nil
		}
		// Development: generated sample pictures, kept apart from real data.
		src, name = photos.Mock{}, "photos-mock"
	}

	dataDir, err := apps.DataDir()
	if err != nil {
		return nil, err
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	return photos.New(photos.Options{
		Source:         src,
		Interval:       cfg.Interval.Duration,
		Refresh:        cfg.Refresh.Duration,
		PreferOriginal: cfg.Synology.Quality == config.PhotoQualityOriginal,
		Motion:         cfg.Motion,
		PairPortraits:  cfg.PairPortraits,
		OnThisDay:      cfg.OnThisDay,
		StatePath:      filepath.Join(dataDir, name+"-selection.json"),
		CacheDir:       filepath.Join(cacheRoot, "pidash", name),
		CacheBytes:     int64(cfg.CacheMB) << 20,
		Log:            log,
	}), nil
}

// runCEC handles `pidash -cec <command>` and returns the exit status. The
// kiosk unit uses "setup" so the TV switches to the Pi and forwards its
// remote's keys; the other commands are handy for trying CEC by hand.
func runCEC(command string) int {
	c := cec.New()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var err error
	switch command {
	case "setup", "on":
		err = c.Setup(ctx)
	case "off":
		err = c.Standby(ctx)
	case "status":
		var p cec.Power
		if p, err = c.PowerStatus(ctx); err == nil {
			fmt.Println("TV power:", p)
		}
	default:
		fmt.Fprintln(os.Stderr, "usage: pidash -cec setup|on|off|status")
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pidash:", err)
		return 1
	}
	return 0
}
