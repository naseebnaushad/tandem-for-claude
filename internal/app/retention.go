package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Claude Code deletes transcripts older than cleanupPeriodDays (30 by default),
// which would lose chats no matter how they are synced. The user can raise it
// here; it is only ever changed on their request (CR-0.1.0-02 OQ8).
const (
	retentionKey     = "cleanupPeriodDays"
	DefaultRetention = 30
	MinRetention     = 30
	MaxRetention     = 3650
	maxSettingsBytes = 5 << 20
)

func (m *Manager) claudeSettingsPath() string {
	return filepath.Join(m.Profiles.Paths.ClaudeDir, "settings.json")
}

func readSettingsJSON(path string) (map[string]any, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, maxSettingsBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxSettingsBytes {
		return nil, ErrUser{"Claude Code's settings file is too large to edit safely."}
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]any{}, nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var m map[string]any
	if err := d.Decode(&m); err != nil || m == nil {
		return nil, ErrUser{"Claude Code's settings file isn't valid JSON, so it was left alone."}
	}
	return m, nil
}

// Retention returns Claude Code's chat retention in days (30 when not set).
func (m *Manager) Retention() (int, error) {
	s, err := readSettingsJSON(m.claudeSettingsPath())
	if err != nil {
		return 0, err
	}
	if n, ok := s[retentionKey].(json.Number); ok {
		if v, err := n.Int64(); err == nil {
			return int(v), nil
		}
	}
	return DefaultRetention, nil
}

// SetRetention sets cleanupPeriodDays in Claude Code's settings.json. Every
// other setting is kept; the previous file is saved next to it first.
func (m *Manager) SetRetention(days int) error {
	if days < MinRetention || days > MaxRetention {
		return ErrUser{"Choose between 30 and 3650 days."}
	}
	path := m.claudeSettingsPath()
	s, err := readSettingsJSON(path)
	if err != nil {
		return err
	}
	if prev, err := os.ReadFile(path); err == nil {
		if err := os.WriteFile(path+".before-tango", prev, 0o600); err != nil {
			return err
		}
	}
	s[retentionKey] = days
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		return err
	}
	tmp := path + ".tango-tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
