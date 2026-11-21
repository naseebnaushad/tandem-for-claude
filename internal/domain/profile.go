package domain

import (
	"regexp"
	"strings"
)

// ProfileKind says what a profile opens.
type ProfileKind string

// Profile kinds.
const (
	KindDesktop ProfileKind = "desktop" // a Claude desktop window with its own data folder
	KindCLI     ProfileKind = "cli"     // Claude Code in a terminal with its own config folder
)

// Built-in profile ids: the normal Claude desktop window and plain `claude`.
const (
	DefaultDesktopID = "default-desktop"
	DefaultCLIID     = "default-cli"
)

// ProfileIDRe matches profile ids, which are also folder names.
var ProfileIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// Profile is one named seat.
type Profile struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Kind    ProfileKind `json:"kind"`
	Builtin bool        `json:"builtin,omitempty"`
}

// ValidKind reports whether k is a known profile kind.
func ValidKind(k ProfileKind) bool { return k == KindDesktop || k == KindCLI }

// NameTaken reports whether another profile already uses name (ignoring case).
func NameTaken(profiles []Profile, name, exceptID string) bool {
	for _, p := range profiles {
		if p.ID != exceptID && strings.EqualFold(p.Name, name) {
			return true
		}
	}
	return false
}

// DefaultProfiles returns the two built-in profiles.
func DefaultProfiles() []Profile {
	return []Profile{
		{ID: DefaultDesktopID, Name: "Default (desktop)", Kind: KindDesktop, Builtin: true},
		{ID: DefaultCLIID, Name: "Default (terminal)", Kind: KindCLI, Builtin: true},
	}
}

// SharedCLIItems are the Claude Code items a CLI profile links back to the
// shared ~/.claude, so only the sign-in and .claude.json differ (CR-02 D12).
var SharedCLIItems = []string{
	"projects", "CLAUDE.md", "settings.json", "skills", "plugins", "agents", "commands",
	"rules", "output-styles", "workflows", "agent-memory", "todos", "file-history",
}

// AccountFieldsInClaudeJSON are removed when .claude.json is copied into a
// new CLI profile, so the copy carries settings and MCP servers but no account.
var AccountFieldsInClaudeJSON = []string{"oauthAccount", "userID", "primaryApiKey", "customApiKeyResponses"}
