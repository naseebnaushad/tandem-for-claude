// Package ports declares the interfaces the use cases depend on. Adapters
// implement them; tests use temp-folder or in-memory versions.
package ports

import "github.com/naseebnaushad/tandem-for-claude/internal/domain"

// SessionStore reads and writes Claude desktop chat lists.
type SessionStore interface {
	// Snapshots returns every store found plus human-readable problems
	// (files skipped because their format was unexpected).
	Snapshots() ([]*domain.Snapshot, []string, error)
	// ReadEntry returns the raw entry (numbers as json.Number).
	ReadEntry(s domain.StoreID, chatID string) (map[string]any, error)
	// WriteEntry writes atomically. Without overwrite it never replaces an
	// existing file and returns false when one exists.
	WriteEntry(s domain.StoreID, chatID string, data map[string]any, overwrite bool) (bool, error)
	// WriteTombstone writes a deleted marker unless one exists.
	WriteTombstone(s domain.StoreID, chatID, value string) (bool, error)
	// SessionDirs maps each data folder label to its claude-code-sessions folder.
	SessionDirs() map[string]string
}

// TranscriptIndex lists the conversation transcripts on disk.
type TranscriptIndex interface {
	// IDs returns transcript ids, or nil when the folder can't be read.
	IDs() map[string]bool
}

// RunningApp is one running Claude desktop main process.
type RunningApp struct {
	PID     int
	DataDir string // "" for the default data folder
}

// Platform is everything that differs between macOS, Windows and Linux.
type Platform interface {
	// DefaultClaudeData is Claude desktop's own data folder.
	DefaultClaudeData() string
	// ClaudeDesktopPath returns the Claude desktop app or executable, or "".
	ClaudeDesktopPath() string
	// RunningDesktop lists running Claude desktop main processes; ok is false when unsure.
	RunningDesktop() (apps []RunningApp, ok bool)
	// LaunchDesktop starts a new Claude desktop window ("" = default data folder).
	LaunchDesktop(dataDir string) error
	// FocusDesktop brings a running Claude desktop process to the front.
	FocusDesktop(pid int) error
	// QuitDesktop asks one Claude desktop process to quit gracefully.
	QuitDesktop(pid int) error
	// OpenCLI opens a terminal running claude ("" = default config folder).
	OpenCLI(configDir string) error
	// CLICommand is the shell command OpenCLI runs, for copying by hand.
	CLICommand(configDir string) string
	// SetAutostart turns "start at login" on or off for the given executable.
	SetAutostart(enable bool, exe string) error
	// AutostartEnabled reports whether "start at login" is on.
	AutostartEnabled() bool
	// OpenFolder shows a folder in the file manager.
	OpenFolder(path string) error
	// LinkDir links a folder (symlink, or a junction on Windows).
	LinkDir(target, link string) error
	// LinkFile links a file; ok is false when links aren't allowed and the caller should copy.
	LinkFile(target, link string) (ok bool, err error)
}

// ProfileRepo stores the list of profiles.
type ProfileRepo interface {
	Load() ([]domain.Profile, error)
	Save([]domain.Profile) error
}

// ProcessProbe looks at running Claude desktop processes.
type ProcessProbe interface {
	// Running returns the data folders of running Claude desktop main
	// processes ("" for the default folder). ok is false when unsure.
	Running() (dataDirs []string, ok bool)
}

// SyncState stores each copy's activity time at the last sync.
type SyncState interface {
	Load() map[string]int64
	Save(map[string]int64) error
}

// Backups archives and restores chat lists.
type Backups interface {
	Create(dirs map[string]string, note string) (string, error)
	List() []string
	Restore(id string, dirs map[string]string) ([]string, error)
}
