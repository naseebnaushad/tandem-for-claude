//go:build linux

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// PROF-011: start at login set by 0.1.0-beta.1 ("Tandem for Claude") moves to
// Tango's autostart entry and points at the new program.
func TestLinuxMigratesOldAutostart(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	l := Linux{HomeDir: t.TempDir()}
	if l.MigrateOldAutostart("/opt/tango") {
		t.Fatal("migrated with nothing to migrate")
	}
	old := filepath.Join(l.configHome(), "autostart", "tandem-for-claude.desktop")
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("[Desktop Entry]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !l.MigrateOldAutostart("/opt/tango") {
		t.Fatal("old entry not migrated")
	}
	if exists(old) {
		t.Fatal("old entry left behind")
	}
	b, err := os.ReadFile(l.autostartPath())
	if err != nil || !strings.Contains(string(b), `"/opt/tango" --background`) || !strings.Contains(string(b), "Name=Tango for Claude") {
		t.Fatal("new entry missing or wrong")
	}
}
