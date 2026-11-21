package app

import (
	"errors"
	"sync"
	"time"

	"github.com/naseebnaushad/tango-for-claude/internal/domain"
	"github.com/naseebnaushad/tango-for-claude/internal/ports"
)

// SettingsStore loads and saves the user's settings.
type SettingsStore interface {
	Load(v any)
	Save(v any) error
}

// StoreFactory builds a session store over the given data folders.
type StoreFactory func(dirs map[string]string) ports.SessionStore

// Manager is the single entry point for every front-end (window, tray, CLI).
type Manager struct {
	Profiles    *Profiles
	Platform    ports.Platform
	NewStore    StoreFactory
	Transcripts ports.TranscriptIndex
	Backups     ports.Backups
	State       ports.SyncState
	SettingsF   SettingsStore
	Exe         string // this program, for start at login

	mu       sync.Mutex // one sync at a time
	lastSync time.Time
	last     *SyncResult
}

// probe adapts the platform's process list to ports.ProcessProbe.
type probe struct{ p ports.Platform }

func (p probe) Running() ([]string, bool) {
	apps, ok := p.p.RunningDesktop()
	var dirs []string
	for _, a := range apps {
		dirs = append(dirs, a.DataDir)
	}
	return dirs, ok
}

func (m *Manager) syncer() (Syncer, error) {
	dirs, err := m.Profiles.DesktopDataDirs()
	if err != nil {
		return Syncer{}, err
	}
	return Syncer{Store: m.NewStore(dirs), Transcripts: m.Transcripts, Probe: probe{m.Platform},
		Backups: m.Backups, State: m.State}, nil
}

// Sync runs one sync (dry run unless apply) and refreshes CLI profile links.
func (m *Manager) Sync(apply bool) (*SyncResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.syncer()
	if err != nil {
		return nil, err
	}
	res, err := s.Sync(apply)
	if err != nil {
		return nil, err
	}
	if apply {
		m.lastSync, m.last = time.Now(), res
		m.refreshCLIProfiles()
	}
	return res, nil
}

func (m *Manager) refreshCLIProfiles() {
	all, err := m.Profiles.All()
	if err != nil {
		return
	}
	for _, p := range all {
		if p.Kind == domain.KindCLI && !p.Builtin {
			_ = m.Profiles.RefreshCLI(m.Profiles.Paths.CLIDir(p.ID))
		}
	}
}

// LastSync returns when the last applied sync ran and its result.
func (m *Manager) LastSync() (time.Time, *SyncResult) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.lastSync, m.last
}

// Settings returns the saved settings, with "start at login" read from the OS.
func (m *Manager) Settings() Settings {
	s := DefaultSettings()
	m.SettingsF.Load(&s)
	s.StartAtLogin = m.Platform.AutostartEnabled()
	return s.Normalise()
}

// SaveSettings stores settings and applies "start at login".
func (m *Manager) SaveSettings(s Settings) error {
	s = s.Normalise()
	if s.StartAtLogin != m.Platform.AutostartEnabled() {
		if err := m.Platform.SetAutostart(s.StartAtLogin, m.Exe); err != nil {
			return ErrUser{"Couldn't change start at login."}
		}
	}
	return m.SettingsF.Save(s)
}

// RestoreBackup puts a backup back. Every Claude window must be closed, and a
// fresh backup of the current state is taken first.
func (m *Manager) RestoreBackup(id string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if apps, ok := m.Platform.RunningDesktop(); !ok || len(apps) > 0 {
		return nil, ErrUser{"Quit every Claude window first, then try again."}
	}
	s, err := m.syncer()
	if err != nil {
		return nil, err
	}
	dirs := s.Store.SessionDirs()
	if _, err := m.Backups.Create(dirs, "before-restore"); err != nil {
		return nil, err
	}
	restored, err := m.Backups.Restore(id, dirs)
	if err != nil {
		return restored, ErrUser{"That backup couldn't be restored."}
	}
	return restored, nil
}

// UserMessage turns an error into text that is safe to show: ErrUser keeps
// its message, anything else becomes a generic one (details go to the log).
func UserMessage(err error) string {
	var u ErrUser
	if errors.As(err, &u) {
		return u.Msg
	}
	return "Something went wrong. Details are in the log file."
}
