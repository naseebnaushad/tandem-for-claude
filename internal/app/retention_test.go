package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// RET-001
func TestRetentionKeepsOtherSettings(t *testing.T) {
	p, _ := newProfiles(t)
	m := &Manager{Profiles: p}
	path := filepath.Join(p.Paths.ClaudeDir, "settings.json")
	must(t, os.WriteFile(path, []byte(`{"model":"x","hooks":{"a":[1,2]},"big":1794479906673}`), 0o600))
	if d, _ := m.Retention(); d != 30 {
		t.Fatal(d)
	}
	must(t, m.SetRetention(365))
	if d, _ := m.Retention(); d != 365 {
		t.Fatal(d)
	}
	raw, _ := os.ReadFile(path)
	var s map[string]any
	must(t, json.Unmarshal(raw, &s))
	if s["model"] != "x" || s["hooks"] == nil {
		t.Fatal(s)
	}
	if !json.Valid(raw) || !containsNumber(raw, "1794479906673") {
		t.Fatal("number precision lost")
	}
	if _, err := os.Stat(path + ".before-tandem"); err != nil {
		t.Fatal("no backup of the previous settings")
	}
}

// RET-002
func TestRetentionRejectsBadInput(t *testing.T) {
	p, _ := newProfiles(t)
	m := &Manager{Profiles: p}
	for _, d := range []int{0, 7, 29, 3651} {
		if err := m.SetRetention(d); err == nil {
			t.Fatal(d)
		}
	}
	path := filepath.Join(p.Paths.ClaudeDir, "settings.json")
	must(t, os.WriteFile(path, []byte(`{not json`), 0o600))
	if err := m.SetRetention(90); err == nil {
		t.Fatal("edited an invalid file")
	}
	if b, _ := os.ReadFile(path); string(b) != "{not json" {
		t.Fatal("invalid file changed")
	}
}
