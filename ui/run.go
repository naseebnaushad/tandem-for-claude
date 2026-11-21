package ui

import (
	"embed"
	_ "embed"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/naseebnaushad/tango-for-claude/internal/app"
	"github.com/naseebnaushad/tango-for-claude/internal/domain"
	"github.com/naseebnaushad/tango-for-claude/internal/wiring"
)

const maxLogBytes = 1 << 20

// Tray icons, drawn by build/icon-src/draw.swift.
var (
	//go:embed icons/tray-template.png
	trayTemplate []byte
	//go:embed icons/tray.png
	trayColour []byte
)

// openLog returns a logger writing to <home>/app.log (0600). The previous log
// is kept as app.log.1 once it passes 1 MB.
func openLog(home string) *slog.Logger {
	path := filepath.Join(home, "app.log")
	if st, err := os.Stat(path); err == nil && st.Size() > maxLogBytes {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return slog.New(slog.NewJSONHandler(f, nil))
}

// Run shows the window (unless background) and the tray icon, and keeps
// syncing on the chosen interval until the user quits.
func Run(m *app.Manager, assets embed.FS, version string, background bool) error {
	log := openLog(m.Profiles.Paths.Home)
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		return err
	}
	svc := &Service{m: m, log: log}

	var window *application.WebviewWindow
	wapp := application.New(application.Options{
		Name:        "Tango for Claude",
		Description: "Use several Claude seats and keep every Code-tab chat",
		Services:    []application.Service{application.NewService(svc)},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(dist)},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "io.github.naseebnaushad.tangoforclaude",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if window != nil {
					window.Show()
					window.Focus()
				}
			},
		},
	})

	window = wapp.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "Tango for Claude",
		Width:            920,
		Height:           640,
		MinWidth:         640,
		MinHeight:        480,
		Hidden:           background,
		BackgroundColour: application.NewRGB(250, 249, 247),
		URL:              "/",
	})
	// Closing the window keeps the app in the tray, where sync keeps running.
	window.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		window.Hide()
		e.Cancel()
	})
	show := func() { window.Show(); window.Focus() }

	tray := wapp.SystemTray.New()
	if runtime.GOOS == "darwin" {
		tray.SetTemplateIcon(trayTemplate)
	} else {
		tray.SetIcon(trayColour)
	}
	tray.SetTooltip("Tango for Claude")
	rebuild := func() { tray.SetMenu(trayMenu(wapp, m, show, svc)) }
	svc.changed = func() { rebuild() }
	svc.synced = func() { wapp.Event.Emit("synced") }
	rebuild()
	tray.OnClick(func() { tray.OpenMenu() })

	if r, ok := m.Platform.(wiring.LegacyAgentRemover); ok && r.RemoveLegacyAgent() {
		log.Info("removed the earlier tool's background job; the app now syncs while it runs")
	}
	if r, ok := m.Platform.(wiring.OldAutostartMigrator); ok && m.Exe != "" && r.MigrateOldAutostart(m.Exe) {
		log.Info("moved start at login from the old app name to Tango for Claude")
	}

	go syncLoop(m, svc, log)
	log.Info("started", "version", version)
	return wapp.Run()
}

// syncLoop runs a sync every interval while sync is on, re-reading the
// settings each round so changes apply without a restart.
func syncLoop(m *app.Manager, svc *Service, log *slog.Logger) {
	time.Sleep(3 * time.Second) // let the window come up first
	for {
		st := m.Settings()
		if st.SyncEnabled {
			r, err := m.Sync(true)
			switch {
			case err != nil:
				log.Error("background sync", "error", err.Error())
			case len(r.Applied) > 0 || len(r.Failed) > 0 || r.Paused:
				log.Info("background sync", "applied", len(r.Applied), "failed", len(r.Failed), "paused", r.Paused)
			}
			if svc.synced != nil {
				svc.synced()
			}
		}
		time.Sleep(time.Duration(st.IntervalMinutes) * time.Minute)
	}
}

func trayMenu(wapp *application.App, m *app.Manager, show func(), svc *Service) *application.Menu {
	menu := wapp.NewMenu()
	views, err := m.Profiles.List()
	if err == nil {
		for _, v := range views {
			id := v.ID
			label := domain.CleanText(v.Name, 40)
			if v.Kind == domain.KindCLI {
				label += "  (terminal)"
			}
			menu.Add("Open " + label).OnClick(func(*application.Context) {
				if err := m.Profiles.Open(id); err != nil {
					svc.log.Info("tray open", "profile", id, "result", app.UserMessage(err))
					show()
					wapp.Event.Emit("notice", app.UserMessage(err))
				}
			})
		}
		menu.AddSeparator()
	}
	menu.Add("Sync now").OnClick(func(*application.Context) {
		go func() {
			if _, err := svc.SyncNow(); err != nil {
				wapp.Event.Emit("notice", err.Error())
			}
		}()
	})
	menu.Add("Open Tango for Claude").OnClick(func(*application.Context) { show() })
	menu.AddSeparator()
	menu.Add("Quit").OnClick(func(*application.Context) { wapp.Quit() })
	return menu
}
