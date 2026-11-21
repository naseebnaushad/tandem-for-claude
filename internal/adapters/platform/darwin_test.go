//go:build darwin

package platform

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// PROF-011: start at login set by 0.1.0-beta.1 ("Tandem for Claude") moves to
// Tango's LaunchAgent and points at the new program.
func TestMacMigratesOldAutostart(t *testing.T) {
	m := Mac{HomeDir: t.TempDir()}
	if m.MigrateOldAutostart("/Applications/Tango for Claude.app/Contents/MacOS/tango") {
		t.Fatal("migrated with nothing to migrate")
	}
	old := m.agentPath(oldAgentLabel)
	if err := os.MkdirAll(filepath.Dir(old), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(old, []byte("<plist/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !m.MigrateOldAutostart("/Applications/Tango for Claude.app/Contents/MacOS/tango") {
		t.Fatal("old job not migrated")
	}
	if exists(old) {
		t.Fatal("old job left behind")
	}
	b, err := os.ReadFile(m.agentPath(agentLabel))
	if err != nil || !strings.Contains(string(b), "MacOS/tango</string>") || !m.AutostartEnabled() {
		t.Fatal("new job missing or wrong")
	}
}
