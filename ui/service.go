// Package ui is the window and tray front-end. It only forwards user
// actions to the Manager and turns results into simple values for the page.
package ui

import (
	"errors"
	"log/slog"
	"time"

	"github.com/naseebnaushad/tango-for-claude/internal/app"
	"github.com/naseebnaushad/tango-for-claude/internal/domain"
)

// Service is bound to the page; each exported method is callable from it.
// Errors returned to the page carry only user-safe messages; details are logged.
type Service struct {
	m       *app.Manager
	log     *slog.Logger
	changed func() // refreshes the tray menu after profile changes
	synced  func() // tells the page and tray that a sync finished
}

func (s *Service) fail(op string, err error) error {
	if err == nil {
		return nil
	}
	var u app.ErrUser
	if !errors.As(err, &u) {
		s.log.Error(op, "error", err.Error())
	}
	return errors.New(app.UserMessage(err))
}

// Profiles lists every profile.
func (s *Service) Profiles() ([]app.ProfileView, error) {
	v, err := s.m.Profiles.List()
	return v, s.fail("list profiles", err)
}

// CreateProfile adds a profile of kind "desktop" or "cli".
func (s *Service) CreateProfile(name, kind string) (domain.Profile, error) {
	p, err := s.m.Profiles.Create(name, domain.ProfileKind(kind))
	if err == nil {
		s.changed()
	}
	return p, s.fail("create profile", err)
}

// RenameProfile changes a profile's display name.
func (s *Service) RenameProfile(id, name string) error {
	err := s.m.Profiles.Rename(id, name)
	if err == nil {
		s.changed()
	}
	return s.fail("rename profile", err)
}

// DeleteProfile removes a profile, and its folder when deleteData is true.
func (s *Service) DeleteProfile(id string, deleteData bool) error {
	err := s.m.Profiles.Delete(id, deleteData)
	if err == nil {
		s.changed()
	}
	return s.fail("delete profile", err)
}

// OpenProfile opens a profile's window or terminal.
func (s *Service) OpenProfile(id string) error {
	return s.fail("open profile", s.m.Profiles.Open(id))
}

// SignInOthers names the open windows the sign-in helper would close.
func (s *Service) SignInOthers(id string) ([]string, error) {
	n, err := s.m.Profiles.SignInOthers(id)
	return n, s.fail("sign-in check", err)
}

// SignInStart closes other windows and opens only this profile.
func (s *Service) SignInStart(id string) error {
	return s.fail("sign-in start", s.m.Profiles.SignInStart(id))
}

// SignInDone reopens the windows the sign-in helper closed.
func (s *Service) SignInDone() error {
	return s.fail("sign-in done", s.m.Profiles.SignInDone())
}

// OpenFolder shows a profile's folder.
func (s *Service) OpenFolder(id string) error {
	views, err := s.m.Profiles.List()
	if err != nil {
		return s.fail("open folder", err)
	}
	for _, v := range views {
		if v.ID == id {
			return s.fail("open folder", s.m.Platform.OpenFolder(v.Folder))
		}
	}
	return errors.New("That profile no longer exists.")
}

// SyncView is the sync panel's content.
type SyncView struct {
	Enabled         bool     `json:"enabled"`
	IntervalMinutes int      `json:"intervalMinutes"`
	LastSync        string   `json:"lastSync"` // RFC 3339, "" if never
	Applied         int      `json:"applied"`
	Failed          int      `json:"failed"`
	Paused          bool     `json:"paused"`
	ClaudeRunning   bool     `json:"claudeRunning"`
	Chats           int      `json:"chats"`
	Pending         int      `json:"pending"`
	Warnings        []string `json:"warnings"`
	Problems        []string `json:"problems"`
	Backup          string   `json:"backup"`
}

// SyncStatus reports the last sync plus a fresh dry run.
func (s *Service) SyncStatus() (SyncView, error) {
	st := s.m.Settings()
	v := SyncView{Enabled: st.SyncEnabled, IntervalMinutes: st.IntervalMinutes}
	when, last := s.m.LastSync()
	if !when.IsZero() {
		v.LastSync = when.Format(time.RFC3339)
		v.Applied, v.Failed, v.Backup = len(last.Applied), len(last.Failed), last.BackupID
	}
	plan, err := s.m.Sync(false)
	if err != nil {
		return v, s.fail("sync status", err)
	}
	fill(&v, plan)
	return v, nil
}

func fill(v *SyncView, r *app.SyncResult) {
	v.Paused, v.ClaudeRunning = r.Paused, r.AppRunning
	chats := map[string]bool{}
	for _, sn := range r.Snapshots {
		for id := range sn.Entries {
			chats[id] = true
		}
	}
	v.Chats = len(chats)
	v.Warnings, v.Problems, v.Pending = []string{}, []string{}, 0
	for _, a := range r.Actions {
		switch a.Kind {
		case domain.ActWarn:
			v.Warnings = append(v.Warnings, domain.CleanText(a.Title, 60)+": "+a.Reason)
		case domain.ActAdd, domain.ActUpdate, domain.ActTombstone:
			v.Pending++
		}
	}
	for _, p := range r.Problems {
		v.Problems = append(v.Problems, domain.CleanText(p, 200))
	}
}

// SyncNow runs a real sync.
func (s *Service) SyncNow() (SyncView, error) {
	r, err := s.m.Sync(true)
	if err != nil {
		return SyncView{}, s.fail("sync", err)
	}
	s.synced()
	st := s.m.Settings()
	v := SyncView{Enabled: st.SyncEnabled, IntervalMinutes: st.IntervalMinutes, LastSync: time.Now().Format(time.RFC3339),
		Applied: len(r.Applied), Failed: len(r.Failed), Backup: r.BackupID}
	fill(&v, r)
	v.Pending = 0
	return v, nil
}

// Settings returns the app settings.
func (s *Service) Settings() app.Settings { return s.m.Settings() }

// SaveSettings stores the app settings.
func (s *Service) SaveSettings(st app.Settings) error {
	return s.fail("save settings", s.m.SaveSettings(st))
}

// Retention returns how many days Claude Code keeps chats.
func (s *Service) Retention() (int, error) {
	d, err := s.m.Retention()
	return d, s.fail("read retention", err)
}

// SetRetention changes how many days Claude Code keeps chats.
func (s *Service) SetRetention(days int) error {
	return s.fail("set retention", s.m.SetRetention(days))
}

// Backups lists backup ids, newest first.
func (s *Service) Backups() []string {
	b := s.m.Backups.List()
	if b == nil {
		return []string{}
	}
	return b
}

// RestoreBackup restores one backup (all Claude windows must be closed).
func (s *Service) RestoreBackup(id string) ([]string, error) {
	r, err := s.m.RestoreBackup(id)
	return r, s.fail("restore backup", err)
}

// ClaudeFound reports whether Claude desktop is installed.
func (s *Service) ClaudeFound() bool { return s.m.Platform.ClaudeDesktopPath() != "" }
