// Package wiring is the composition root: it reads configuration from the
// environment and connects the adapters to the use cases.
package wiring

import (
	"os"
	"path/filepath"

	"github.com/naseebnaushad/tandem-for-claude/internal/adapters/fsstore"
	"github.com/naseebnaushad/tandem-for-claude/internal/adapters/platform"
	"github.com/naseebnaushad/tandem-for-claude/internal/app"
	"github.com/naseebnaushad/tandem-for-claude/internal/ports"
)

// Environment variables that override the default folders (see .env.example).
const (
	EnvHome      = "CLAUDE_PROFILE_HOME"
	EnvAppData   = "CLAUDE_PROFILE_APP_DATA"
	EnvClaudeDir = "CLAUDE_PROFILE_CLAUDE_DIR"
	EnvProjects  = "CLAUDE_PROFILE_PROJECTS"
)

// Build returns a ready Manager for this computer.
func Build() (*app.Manager, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	plat := platform.New()
	paths := app.Paths{
		Home:       envOr(EnvHome, filepath.Join(home, ".claude-profile")),
		ClaudeData: envOr(EnvAppData, plat.DefaultClaudeData()),
		ClaudeDir:  envOr(EnvClaudeDir, filepath.Join(home, ".claude")),
		ClaudeJSON: filepath.Join(home, ".claude.json"),
	}
	if err := os.MkdirAll(paths.Home, 0o700); err != nil {
		return nil, err
	}
	exe, _ := os.Executable()
	profiles := &app.Profiles{Paths: paths, Repo: fsstore.ProfileFile{Path: filepath.Join(paths.Home, "profiles.json")}, Platform: plat}
	if err := profiles.Migrate(); err != nil {
		return nil, err
	}
	return &app.Manager{
		Profiles:    profiles,
		Platform:    plat,
		NewStore:    func(dirs map[string]string) ports.SessionStore { return fsstore.NewStore(dirs) },
		Transcripts: fsstore.Transcripts{Dir: envOr(EnvProjects, filepath.Join(paths.ClaudeDir, "projects"))},
		Backups:     fsstore.Backups{Dir: filepath.Join(paths.Home, "backups")},
		State:       fsstore.State{Path: filepath.Join(paths.Home, "state.json")},
		SettingsF:   fsstore.JSONFile{Path: filepath.Join(paths.Home, "settings.json")},
		Exe:         exe,
	}, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// LegacyAgentRemover is implemented by platforms that can remove the old
// Python tool's background job (macOS).
type LegacyAgentRemover interface{ RemoveLegacyAgent() bool }
