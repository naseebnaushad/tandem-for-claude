//go:build darwin

package platform

import (
	"bytes"
	"encoding/xml"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/naseebnaushad/tango-for-claude/internal/ports"
)

const agentLabel = "local.tango-for-claude"

// oldAgentLabel is the LaunchAgent of 0.1.0-beta.1, when the app was called
// "Tandem for Claude".
const oldAgentLabel = "local.tandem-for-claude"

// legacyAgentLabel is the LaunchAgent the earlier Python tool installed.
const legacyAgentLabel = "local.claude-profile.sync"

// Mac implements ports.Platform on macOS.
type Mac struct{ HomeDir string }

// New returns the platform for this OS.
func New() ports.Platform {
	h, _ := os.UserHomeDir()
	return Mac{HomeDir: h}
}

func hide(*exec.Cmd)   {}
func detach(*exec.Cmd) {}

// DefaultClaudeData is ~/Library/Application Support/Claude.
func (m Mac) DefaultClaudeData() string {
	return filepath.Join(m.HomeDir, "Library", "Application Support", "Claude")
}

// ClaudeDesktopPath finds Claude.app.
func (m Mac) ClaudeDesktopPath() string {
	for _, p := range []string{"/Applications/Claude.app", filepath.Join(m.HomeDir, "Applications", "Claude.app")} {
		if exists(p) {
			return p
		}
	}
	return ""
}

var macMain = regexp.MustCompile(`^\s*(\d+)\s+(\S.*\.app/Contents/MacOS/Claude)(\s.*)?$`)

// RunningDesktop reads `ps` for Claude main processes (helpers are skipped).
func (m Mac) RunningDesktop() ([]ports.RunningApp, bool) {
	out, err := output("/bin/ps", "-axo", "pid=,command=")
	if err != nil {
		return nil, false
	}
	var apps []ports.RunningApp
	for _, line := range strings.Split(out, "\n") {
		g := macMain.FindStringSubmatch(line)
		if g == nil {
			continue
		}
		pid, _ := strconv.Atoi(g[1])
		apps = append(apps, ports.RunningApp{PID: pid, DataDir: dataDirFromCommand(g[3])})
	}
	return apps, true
}

// LaunchDesktop opens a new Claude window with `open -n`, which starts a new
// copy even when one is already running.
func (m Mac) LaunchDesktop(dataDir string) error {
	app := m.ClaudeDesktopPath()
	if app == "" {
		return os.ErrNotExist
	}
	args := []string{"-n", "-a", app}
	if dataDir != "" {
		args = append(args, "--args", "--user-data-dir="+dataDir)
	}
	_, err := output("/usr/bin/open", args...)
	return err
}

// FocusDesktop brings that exact process to the front (macOS may ask once
// to allow Tango for Claude to control System Events).
func (m Mac) FocusDesktop(pid int) error {
	_, err := output("/usr/bin/osascript", "-e",
		`tell application "System Events" to set frontmost of (first process whose unix id is `+strconv.Itoa(pid)+`) to true`)
	return err
}

// QuitDesktop sends SIGTERM, which Electron handles as a normal quit.
func (m Mac) QuitDesktop(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

// CLICommand returns the shell command for a CLI profile.
func (m Mac) CLICommand(configDir string) string { return posixCLICommand(configDir) }

// OpenCLI opens Terminal running the profile's command.
func (m Mac) OpenCLI(configDir string) error {
	cmd := m.CLICommand(configDir)
	esc := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(cmd)
	_, err := output("/usr/bin/osascript",
		"-e", `tell application "Terminal" to do script "`+esc+`"`,
		"-e", `tell application "Terminal" to activate`)
	return err
}

func (m Mac) agentPath(label string) string {
	return filepath.Join(m.HomeDir, "Library", "LaunchAgents", label+".plist")
}

// SetAutostart installs or removes a LaunchAgent that starts the app hidden.
func (m Mac) SetAutostart(enable bool, exe string) error {
	path := m.agentPath(agentLabel)
	if !enable {
		_, _ = output("/bin/launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid()), path)
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	var exeXML bytes.Buffer
	if err := xml.EscapeText(&exeXML, []byte(exe)); err != nil {
		return err
	}
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>` + agentLabel + `</string>
  <key>ProgramArguments</key><array><string>` + exeXML.String() + `</string><string>--background</string></array>
  <key>RunAtLoad</key><true/>
  <key>ProcessType</key><string>Interactive</string>
</dict>
</plist>
`)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// AutostartEnabled reports whether the LaunchAgent exists.
func (m Mac) AutostartEnabled() bool { return exists(m.agentPath(agentLabel)) }

// RemoveLegacyAgent removes the Python tool's LaunchAgent; true if it existed.
func (m Mac) RemoveLegacyAgent() bool {
	path := m.agentPath(legacyAgentLabel)
	if !exists(path) {
		return false
	}
	_, _ = output("/bin/launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid()), path)
	return os.Remove(path) == nil
}

// MigrateOldAutostart replaces the start-at-login job of the app's old name
// with one for this app, keeping the user's choice. True if it found one.
func (m Mac) MigrateOldAutostart(exe string) bool {
	path := m.agentPath(oldAgentLabel)
	if !exists(path) {
		return false
	}
	_, _ = output("/bin/launchctl", "bootout", "gui/"+strconv.Itoa(os.Getuid()), path)
	_ = os.Remove(path)
	return m.SetAutostart(true, exe) == nil
}

// OpenFolder shows a folder in Finder.
func (m Mac) OpenFolder(path string) error { return start("/usr/bin/open", path) }

// LinkDir creates a symlink.
func (m Mac) LinkDir(target, link string) error { return symlinkDir(target, link) }

// LinkFile creates a symlink.
func (m Mac) LinkFile(target, link string) (bool, error) { return symlinkFile(target, link) }
