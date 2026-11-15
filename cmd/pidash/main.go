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
	"github.com/DarioRomano/PiDash/internal/bluetooth"
	"github.com/DarioRomano/PiDash/internal/cec"
	"github.com/DarioRomano/PiDash/internal/config"
	"github.com/DarioRomano/PiDash/internal/games"
	"github.com/DarioRomano/PiDash/internal/hass"
	"github.com/DarioRomano/PiDash/internal/input"
	"github.com/DarioRomano/PiDash/internal/kiosk"
	"github.com/DarioRomano/PiDash/internal/logring"
	"github.com/DarioRomano/PiDash/internal/maps"
	"github.com/DarioRomano/PiDash/internal/photos"
	"github.com/DarioRomano/PiDash/internal/playing"
	"github.com/DarioRomano/PiDash/internal/power"
	"github.com/DarioRomano/PiDash/internal/providers"
	"github.com/DarioRomano/PiDash/internal/providers/calendar"
	"github.com/DarioRomano/PiDash/internal/providers/system"
	"github.com/DarioRomano/PiDash/internal/providers/weather"
	"github.com/DarioRomano/PiDash/internal/radar"
	"github.com/DarioRomano/PiDash/internal/receivers"
	"github.com/DarioRomano/PiDash/internal/screen"
	"github.com/DarioRomano/PiDash/internal/server"
	"github.com/DarioRomano/PiDash/internal/settings"
	"github.com/DarioRomano/PiDash/internal/sound"
	"github.com/DarioRomano/PiDash/internal/tv"
	"github.com/DarioRomano/PiDash/internal/update"
	"github.com/DarioRomano/PiDash/internal/videos"
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
		screenCmd = flag.String("screen", "", "set the picture sent to the TV and exit: dashboard (as the settings say) or restore")
		checkCfg  = flag.Bool("check-config", false, "check the settings file given with -config and exit")
		alone     = flag.Bool("standalone", false, "serve only the dashboard, for any browser: nothing of this machine (apps, TV, sound, Bluetooth) is touched; for a container")
		health    = flag.Bool("healthcheck", false, "ask the PiDash running on this machine whether it is well, and exit: 0 if so")
		speakerEv = flag.Bool("speaker-event", false, "internal: pass on what the Spotify speaker says is playing")
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
	// Warnings and errors are also kept for the status page on the TV.
	problems := logring.New(30)
	log := slog.New(problems.Handler(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	if *health {
		os.Exit(healthcheck(*cfgPath, *listen))
	}
	if *speakerEv {
		// Started by librespot, many times a song: quick, and quiet.
		if err := receivers.SendEvent(); err != nil {
			os.Exit(1)
		}
		return
	}
	if *cecCmd != "" {
		os.Exit(runCEC(*cecCmd, *cfgPath))
	}
	if *screenCmd != "" {
		os.Exit(runScreen(*screenCmd, *cfgPath, log))
	}

	cfg, err := config.Load(*cfgPath)
	if *alone && !*checkCfg && errors.Is(err, fs.ErrNotExist) {
		// A container that was given no settings file: the built-in settings,
		// and what is chosen on the settings page.
		log.Info("no settings file; using the built-in settings", "path", *cfgPath)
		cfg, err = config.Default(), nil
	}
	if *alone && err == nil {
		mountedPhotos(&cfg, os.Getenv("PIDASH_PHOTOS"))
	}
	if *checkCfg {
		if err != nil {
			fmt.Println("Problem in the settings:", err)
			os.Exit(2)
		}
		if len(cfg.Unknown) > 0 {
			fmt.Println("The settings file is fine, apart from settings this version does not know, which are ignored:", strings.Join(cfg.Unknown, ", "))
			return
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
	if len(cfg.Unknown) > 0 && *execApp == "" {
		// A misspelt setting, or one of another version: the rest still counts.
		log.Warn("config: settings this version does not know are ignored", "keys", cfg.Unknown)
		notices = append(notices, "The settings file has settings this version of PiDash does not know, which were ignored: "+strings.Join(cfg.Unknown, ", ")+".")
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
	if *alone {
		cfg.OnlyTheDashboard()
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
			sys := system.New(cfg.System.Interval.Duration)
			sys.CoolAbove = cfg.System.CoolAbove
			ps = append(ps, sys)
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
		if cfg.Home.Enabled() && len(cfg.Home.Entities) > 0 {
			ps = append(ps, &hass.Provider{Client: hass.New(cfg.Home.URL, cfg.Home.Token), Entities: cfg.Home.Entities})
		}
		if len(cfg.Calendar.URLs) > 0 {
			ps = append(ps, calendar.New(calendar.Options{URLs: cfg.Calendar.URLs, Days: cfg.Calendar.Days}))
		}
		return providers.NewRunner(log, publish, ps...)
	}

	// What is of this machine is left alone in development, and where only
	// the dashboard is served.
	bare := *mock || *alone
	var appBackend apps.Backend = &apps.Mock{Log: log}
	if !bare {
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
	if !bare {
		// Keeps the keyboard, and with it the remote, on the app that is open.
		appManager.Focus = &apps.Focuser{Env: func() []string { return apps.DesktopEnv(os.Environ()) }, Log: log}
	}
	if *mock {
		appManager.Focus = apps.PretendFocuser(log)
	}
	appManager.ResolveIcons(apps.DefaultIconFinder())
	// The games of the PC, as Moonlight knows them, become tiles too.
	library := gameLibrary(cfg, log)
	if library != nil {
		finder := apps.DefaultIconFinder()
		appManager.SetGames(library.Load(), finder)
		library.OnChange = func(tiles []config.App) { appManager.SetGames(tiles, finder) }
		if *mock {
			// Development: no PC is knocked at or woken.
			library.Wake.Up = func(context.Context, games.Host) bool { return true }
		}
	}

	photoSettings := cfg.Photos
	if cfg.Display.Performance == "saver" {
		// Playing video is among the hardest work the dashboard does.
		photoSettings.Videos = false
	}
	photoService, err := newPhotoService(photoSettings, *mock, log)
	if err != nil {
		log.Error("photos", "err", err)
		os.Exit(1)
	}

	// TV power schedule over HDMI-CEC.
	cecControl := cec.New()
	// Development: with PIDASH_PRETEND_TV set there is a TV that does as it is told.
	var pretendTV *tv.Pretend
	var theTV tv.TV = cecControl
	if *mock && os.Getenv("PIDASH_PRETEND_TV") != "" {
		pretendTV = &tv.Pretend{}
		theTV = pretendTV
	}
	tvOpts := tv.Options{TV: theTV, Log: log, AppRunning: func() bool { return appManager.Status().Running != "" },
		OnSleep: func() {
			if appManager.Status().Running != "" {
				_ = appManager.Stop(context.Background())
			}
		}}
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
	tvOpts.IdleOff = cfg.TV.IdleOff.Duration
	// Music behind the dashboard is something going on, though nobody
	// presses anything for it: from an app, or from a phone.
	var speakers *receivers.Service
	tvOpts.Busy = func() bool { return appManager.Status().Background != "" || (speakers != nil && speakers.Playing()) }
	tvService := tv.New(tvOpts)
	// What the TV says over HDMI tells whether anyone can see the dashboard.
	cecControl.OnTVStandby = tvService.NoteStandby
	cecControl.OnTVActive = tvService.NoteAwake

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

	// New releases: reported on the settings page, installed at night if wanted.
	updateNote := ""
	if *alone {
		updateNote = "PiDash runs in a container here: a new version comes with a new image. Apps, sound, Bluetooth and the TV's remote need PiDash installed on the computer itself."
	}
	updater := update.New(update.Options{
		Current: version, Auto: cfg.Update.Auto, Log: log, Fixed: updateNote,
		Busy: func() bool { return appManager.Status().Running != "" },
	})

	// Starts the browser on the TV again if the dashboard in it freezes.
	kioskControl := kiosk.New(cfg.Kiosk.Unit, *mock, log)
	// The settings may have changed what picture the TV gets while the
	// dashboard runs: saving them starts this program anew, and it is done here.
	if !bare && kioskControl.Available() {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			running := kioskControl.Active(ctx)
			cancel()
			if !running {
				return
			}
			if err := applyScreen(cfg.Kiosk.Resolution == "1080p", log); err != nil && !errors.Is(err, screen.ErrNoTool) {
				log.Warn("could not set the picture sent to the TV", "err", err)
			} else if err != nil && cfg.Kiosk.Resolution == "1080p" {
				log.Warn("the TV cannot be sent a smaller picture here", "err", err)
			}
		}()
	}
	var watchdog *kiosk.Watchdog
	if cfg.Kiosk.Watchdog && !bare && kioskControl.Available() {
		watchdog = &kiosk.Watchdog{Browser: kioskControl, Log: log,
			Paused: func() bool { return appManager.Status().Running != "" }}
	}

	// Bluetooth devices, managed from the settings page.
	var blue bluetooth.Manager
	switch {
	case *alone: // none of this machine's
	case *mock:
		blue = bluetooth.NewMock()
	default:
		real := bluetooth.New(log)
		defer real.Close()
		blue = real
	}

	// Where the sound goes, also chosen on the settings page.
	var audio sound.Manager
	switch {
	case *alone:
	case *mock:
		audio = sound.NewMock()
	default:
		audio = &sound.Pactl{}
	}

	// Restarting the Pi and switching it off, from the app row. In
	// development the computer is left alone.
	var powerControl power.Control
	switch {
	case *alone:
	case *mock:
		powerControl = &power.Mock{}
	default:
		powerControl = &power.System{Log: log}
	}

	// What music is playing, for the dashboard.
	var player server.Player
	var follow func(context.Context)
	var srv *server.Server
	// What the dashboard is told when a player on the Pi says something
	// new: that, unless a phone is playing on the Pi (see below).
	shown := func(t *playing.Track) *playing.Track { return t }
	switch {
	case *alone:
	case *mock:
		player = &playing.Mock{OnChange: func(t *playing.Track) { srv.PlayingChanged(shown(t)) }}
	default:
		real := &playing.Service{Log: log, OnChange: func(t *playing.Track) { srv.PlayingChanged(shown(t)) }}
		player, follow = real, real.Run
	}

	// The Pi as a speaker for phones: Spotify Connect and AirPlay. What a
	// phone plays is shown like any other music.
	if !*alone {
		exe, _ := os.Executable()
		dir := ""
		if data, err := apps.DataDir(); err == nil {
			dir = filepath.Join(data, "speakers")
		}
		on := dir != "" && exe != ""
		var onPi receivers.Player
		if player != nil {
			onPi = player
		}
		speakers = receivers.New(receivers.Options{
			Name: cfg.Sound.SpeakerName, Spotify: on && cfg.Sound.SpotifyConnect, AirPlay: on && cfg.Sound.AirPlay,
			Dir: dir, Exe: exe, Log: log,
		})
		both := receivers.Both{Speakers: speakers, Others: onPi}
		speakers.OnChange(func() { srv.PlayingChanged(both.Current()) })
		shown = func(*playing.Track) *playing.Track { return both.Current() }
		player = both
	}

	// The TV remote, keyboards and game controllers, read below the desktop.
	var remote *input.Remote
	if !bare {
		remote = &input.Remote{
			Actions: remoteActions, ExitKeys: exitKeys, Hold: cfg.Input.AppExitHold.Duration, ExitPresses: cfg.Input.AppExitPresses, Log: log,
			AppRunning: func() bool { return appManager.Status().Running != "" },
			OnChannel:  func(up bool) bool { return srv != nil && srv.ChannelKey(up) },
			OnAction: func(action string) {
				tvService.NoteAwake() // a button of its remote: the TV is on
				tvService.NoteActivity()
				srv.RemoteAction(action)
			},
			OnAppExit: func() {
				log.Info("Back held on the remote: leaving the app")
				if err := appManager.Leave(context.Background()); err != nil {
					log.Warn("closing the app", "err", err)
				}
			},
			Tap: phoneInput.Tap,
			OnAppKey: func() {
				tvService.NoteActivity()
				appManager.EnsureFocus()
			},
			// A controller's buttons do not come through the TV, so they say
			// nothing about whether it is on. While it is believed to be off
			// they only make PiDash ask it.
			// A held direction keeps moving through the app row and the lists
			// of the settings pages.
			OnRepeat: func(action string) {
				if !tvService.Status().Asleep {
					srv.RemoteRepeat(action)
				}
			},
			PadAsKeys: func() bool { return cfg.Input.PadInWebsites && appManager.RunningWebsite() },
			OnPadAction: func(action string) {
				tvService.NoteActivity()
				if tvService.Status().Asleep {
					go tvService.Recheck(context.Background())
					return
				}
				// The screen names the buttons of the controller that is in the hand.
				last, _ := remote.Last()
				srv.RemoteActionFrom(action, input.PadKind(last.Device))
			},
		}
		if cfg.TV.ControllerVolume {
			// Logo button and up or down on a controller: the TV's volume.
			// The steps are sent one after the other; while the TV is busy
			// with one, further presses are dropped rather than piled up.
			steps := make(chan int, 2)
			to := 0 // the TV
			if cfg.TV.VolumeTo == "audio" {
				to = 5 // a soundbar or receiver
			}
			go func() {
				warned := false
				for step := range steps {
					button := "up"
					if step < 0 {
						button = "down"
					}
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					if err := cecControl.Volume(ctx, button, to); err != nil && !warned {
						warned = true
						log.Warn("could not change the volume from the controller", "err", err)
					}
					cancel()
				}
			}()
			remote.OnVolume = func(step int) {
				select {
				case steps <- step:
				default:
				}
			}
		}
		if cfg.TV.WakeOnController {
			// Like a games console: switching the controller on switches the TV on.
			remote.OnPadWake = func(name string) {
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
					defer cancel()
					switch woke, err := tvService.WakeIfOff(ctx); {
					case err != nil:
						log.Warn("switching the TV on for a game controller", "controller", name, "err", err)
					case woke:
						log.Info("switched the TV on for a game controller", "controller", name)
						// Like a games console: what comes up is the games.
						if cfg.Games.OpenOnController {
							srv.OpenPage("games")
						}
					}
				}()
			}
		}
	}

	films := videoLibrary(cfg, *mock, func() string { return srv.Loopback() })
	srv = server.New(cfg, log, static, server.Deps{
		Art:    artService(cfg, *mock, version, log),
		Videos: films, PretendTV: pretendTV, Radar: rainRadar(cfg, *mock, version),
		Kiosk: kioskControl, Apps: appManager, Photos: photoService,
		Input: phoneInput, TV: tvService, Version: version, Standalone: *alone, Speakers: speakers, NewRunner: newRunner, Updater: updater, PhotoStatePath: photoStatePath(), TestHooks: *mock,
		Notices: notices, URLFile: server.URLFile(), Settings: store, BaseListen: baseListen,
		Remote: remote, Problems: problems, Bluetooth: blue, Sound: audio, Watchdog: watchdog, Playing: player, Games: library, Power: powerControl, Maps: mapTiles(cfg, version),
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if library != nil {
		go library.Run(ctx)
	}
	var speakersDone chan struct{}
	if speakers != nil {
		speakersDone = make(chan struct{})
		go func() {
			speakers.Run(ctx)
			close(speakersDone)
		}()
	}
	if !bare {
		go remote.Run(ctx)
		if watchdog != nil {
			go watchdog.Run(ctx)
		}
		go follow(ctx)
		// Answer the TV's questions, so it keeps sending the remote's keys
		// here after it was switched to another input and back.
		go cecControl.Follow(ctx, log)
		go updater.Run(ctx)
	}

	log.Info("pidash starting", "version", version, "config", *cfgPath)
	err = srv.Run(ctx)
	// The speakers for phones end with this program, also when it starts
	// itself anew: left running, they would be in their successors' way.
	stop()
	if speakersDone != nil {
		select {
		case <-speakersDone:
		case <-time.After(5 * time.Second):
		}
	}
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
// gameLibrary returns the games that get tiles of their own: those of the
// PC, if Moonlight is one of the apps, and those of RetroArch, if that is.
// Nil when neither is set up or wanted.
func gameLibrary(cfg config.Config, log *slog.Logger) *games.Library {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	library := &games.Library{Home: home, Log: log}
	for _, a := range cfg.Apps {
		if cfg.Games.Moonlight && library.Moonlight.Command == "" && games.IsMoonlight(a) {
			library.Moonlight = a
		}
		if cfg.Games.RetroArch && library.RetroArch.Command == "" && games.IsRetroArch(a) {
			library.RetroArch = a
		}
	}
	if library.Moonlight.Command == "" && library.RetroArch.Command == "" {
		return nil
	}
	if dataDir, err := apps.DataDir(); err == nil {
		library.PlayedPath = filepath.Join(dataDir, "games-played.json")
	}
	return library
}

// artService is the slideshow of paintings that can stand in for the
// photos, or nil when it is not offered.
func artService(cfg config.Config, mock bool, version string, log *slog.Logger) *photos.Service {
	if !cfg.Photos.Art {
		return nil
	}
	dataDir, err := apps.DataDir()
	if err != nil {
		return nil
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return nil
	}
	var src photos.Source = photos.NewArt("PiDash/" + version + " (TV dashboard; https://github.com/DarioRomano/PiDash)")
	name := "art"
	if mock {
		// Development: invented paintings, so that no museum is asked.
		src, name = photos.ArtMock{}, "art-mock"
	}
	motion := cfg.Photos.Motion
	if motion == config.PhotoMotionFill {
		motion = config.PhotoMotionWhole // nothing of a painting is cut off
	}
	return photos.New(photos.Options{
		Source: src, Interval: cfg.Photos.Interval.Duration, Refresh: 24 * time.Hour, Motion: motion, ShowWhole: true,
		StatePath: filepath.Join(dataDir, name+"-selection.json"), CacheDir: filepath.Join(cacheRoot, "pidash", name),
		CacheBytes: 300 << 20, Log: log,
	})
}

// rainRadar is where the pictures of the rain radar come from, or nil when
// it is not wanted or there is no weather to show it beside.
func rainRadar(cfg config.Config, mock bool, version string) *radar.Radar {
	if !cfg.Weather.Radar || (!cfg.Weather.Enabled() && !mock) {
		return nil
	}
	r := radar.New("PiDash/" + version + " (TV dashboard; https://github.com/DarioRomano/PiDash)")
	if cfg.Weather.RadarURL != "" {
		r.URL = cfg.Weather.RadarURL
	}
	return r
}

// videoLibrary returns the folders of videos and their player, or nil when
// none are set up. self says where PiDash is reached on the Pi itself.
func videoLibrary(cfg config.Config, mock bool, self func() string) *videos.Library {
	v := cfg.Videos
	if v.Source == config.VideoSourceNone {
		return nil
	}
	dataDir, err := apps.DataDir()
	if err != nil {
		return nil
	}
	lib := &videos.Library{Player: v.Player, Dir: filepath.Join(dataDir, "videos")}
	_ = os.MkdirAll(lib.Dir, 0o700)
	if mock {
		// Development: a player is pretended, so nothing needs installing.
		lib.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	}
	switch v.Source {
	case config.VideoSourceSynology:
		nas := cfg.Photos.Synology
		lib.Source = videos.NewSynology(videos.SynologyOptions{
			URL: nas.URL, Username: nas.Username, Password: nas.Password, InsecureTLS: nas.InsecureTLS,
			Folders: v.Folders, Self: self, SecretPath: filepath.Join(lib.Dir, "secret"),
		})
	default:
		lib.Source = &videos.Local{Roots: v.Folders}
	}
	return lib
}

func runApp(cfg config.Config, id string, log *slog.Logger) error {
	// A video: the player and what it is to play were noted for it.
	if id == videos.AppID {
		argv, err := videos.Pending()
		if err != nil {
			return err
		}
		return syscall.Exec(argv[0], argv, apps.DesktopEnv(os.Environ()))
	}
	// A game tile is not in the settings: it is read from Moonlight's list.
	if strings.HasPrefix(id, games.Prefix) {
		if library := gameLibrary(cfg, log); library != nil {
			library.Load()
			if game, ok := library.Find(id); ok {
				cfg.Apps = append(cfg.Apps, game)
			}
		}
	}
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

// mapTiles is the store of the map shown beside a photo, or nil when no map
// is wanted.
func mapTiles(cfg config.Config, version string) *maps.Tiles {
	if !cfg.Photos.Map {
		return nil
	}
	cacheRoot, err := os.UserCacheDir()
	if err != nil {
		return nil
	}
	// The map service asks every program to say what it is.
	return maps.New(cfg.Photos.MapTiles, filepath.Join(cacheRoot, "pidash", "map"), "PiDash/"+version+" (TV dashboard; https://github.com/DarioRomano/PiDash)")
}

// photoStatePath is where the slideshow of a real photo source keeps its state.
func photoStatePath() string {
	dir, err := apps.DataDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "photos-selection.json")
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
			InsecureTLS: cfg.Synology.InsecureTLS, Videos: cfg.Videos,
		})
	case config.PhotoSourceFolder:
		local := photos.NewLocalDir(cfg.Folder.Path)
		local.Videos = cfg.Videos
		src = local
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
		Videos:         cfg.Videos,
		SkipOdd:        cfg.SkipOdd,
		Places:         cfg.Map,
		StatePath:      filepath.Join(dataDir, name+"-selection.json"),
		CacheDir:       filepath.Join(cacheRoot, "pidash", name),
		CacheBytes:     int64(cfg.CacheMB) << 20,
		Log:            log,
	}), nil
}

// screenControl sets the picture the TV is sent. What a screen was set to is
// remembered until the desktop session ends, which is also when the desktop
// sets its screens anew by itself.
func screenControl() *screen.Control {
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	return &screen.Control{StatePath: filepath.Join(dir, "pidash-screen.json")}
}

// wantsSmallPicture reads the settings, as saved on the settings page and in
// config.toml, for whether the TV gets 1920 x 1080 while the dashboard runs.
func wantsSmallPicture(cfgPath string) bool {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return false
	}
	if store, err := settings.Open(settings.Path(cfgPath), cfg); err == nil {
		if effective, err := store.Effective(); err == nil {
			cfg = effective
		}
	}
	return cfg.Kiosk.Resolution == "1080p"
}

// applyScreen gives the TV the picture size the settings ask for while the
// dashboard runs (small), or its own back (!small).
func applyScreen(small bool, log *slog.Logger) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c := screenControl()
	var changed []string
	var err error
	if small {
		changed, err = c.Lower(ctx)
	} else {
		changed, err = c.Restore(ctx)
	}
	for _, what := range changed {
		log.Info("set the picture sent to the TV", "screen", what)
	}
	return err
}

// runScreen handles `pidash -screen dashboard|restore`, which the unit of
// the dashboard's browser runs before it starts and after it has stopped.
func runScreen(command, cfgPath string, log *slog.Logger) int {
	var err error
	switch command {
	case "dashboard":
		err = applyScreen(wantsSmallPicture(cfgPath), log)
	case "restore":
		err = applyScreen(false, log)
	default:
		fmt.Fprintln(os.Stderr, "usage: pidash -screen dashboard|restore")
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pidash:", err)
		return 1
	}
	return 0
}

// runCEC handles `pidash -cec <command>` and returns the exit status. The
// kiosk unit uses "setup" so the TV switches to the Pi and forwards its
// remote's keys; the other commands are handy for trying CEC by hand.
func runCEC(command, cfgPath string) int {
	c := cec.New()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var err error
	switch command {
	case "start":
		// The dashboard starts: after switching the Pi on, but just as well
		// after a fault, an update, or someone working on the Pi from afar.
		// None of that is a reason to switch a TV on.
		if cfg, cfgErr := config.Load(cfgPath); cfgErr == nil && cfg.TV.WakeOnStart {
			err = c.Setup(ctx)
			break
		}
		var shown bool
		if shown, err = c.Announce(ctx); err == nil && !shown {
			fmt.Println("the TV is in standby and was left so")
		}
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
		fmt.Fprintln(os.Stderr, "usage: pidash -cec start|on|off|status")
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "pidash:", err)
		return 1
	}
	return 0
}
