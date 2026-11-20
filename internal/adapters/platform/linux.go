//go:build linux

package platform

// There is no official Claude desktop for Linux. This works with community
// builds that follow Electron's defaults; it has not been tried on a real
// Linux machine yet (CR-0.1.0-02 OQ1). CLI profiles work with Claude Code.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/naseebnaushad/tandem-for-claude/internal/ports"
)

var desktopNames = []string{"claude-desktop", "Claude", "claude-desktop-bin"}

// Linux implements ports.Platform on Linux.
type Linux struct{ HomeDir string }

// New returns the platform for this OS.
func New() ports.Platform {
	h, _ := os.UserHomeDir()
	return Linux{HomeDir: h}
}

func hide(*exec.Cmd) {}

func detach(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} }

func (l Linux) configHome() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return x
	}
	return filepath.Join(l.HomeDir, ".config")
}

// DefaultClaudeData is ~/.config/Claude.
func (l Linux) DefaultClaudeData() string { return filepath.Join(l.configHome(), "Claude") }

// ClaudeDesktopPath looks for a Claude desktop build on PATH or in /opt.
func (Linux) ClaudeDesktopPath() string {
	for _, n := range desktopNames {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
	}
	for _, p := range []string{"/opt/Claude/claude-desktop", "/opt/Claude/Claude", "/usr/lib/claude-desktop/claude-desktop"} {
		if exists(p) {
			return p
		}
	}
	return ""
}

// RunningDesktop reads /proc for Claude desktop main processes.
func (Linux) RunningDesktop() ([]ports.RunningApp, bool) {
	procs, err := os.ReadDir("/proc")
	if err != nil {
		return nil, false
	}
	var apps []ports.RunningApp
	for _, p := range procs {
		pid, err := strconv.Atoi(p.Name())
		if err != nil {
			continue
		}
		raw, err := os.ReadFile(filepath.Join("/proc", p.Name(), "cmdline"))
		if err != nil || len(raw) == 0 {
			continue
		}
		args := bytes.Split(bytes.TrimRight(raw, "\x00"), []byte{0})
		base := filepath.Base(string(args[0]))
		known := false
		for _, n := range desktopNames {
			if base == n {
				known = true
			}
		}
		if !known {
			continue
		}
		app := ports.RunningApp{PID: pid}
		helper := false
		for _, a := range args[1:] {
			s := string(a)
			if bytes.HasPrefix(a, []byte("--type=")) {
				helper = true
			}
			if v, ok := cut(s, "--user-data-dir="); ok {
				app.DataDir = v
			}
		}
		if !helper {
			apps = append(apps, app)
		}
	}
	return apps, true
}

func cut(s, prefix string) (string, bool) {
	if len(s) > len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return "", false
}

// LaunchDesktop starts Claude desktop with its own data folder.
func (l Linux) LaunchDesktop(dataDir string) error {
	exe := l.ClaudeDesktopPath()
	if exe == "" {
		return os.ErrNotExist
	}
	if dataDir == "" {
		return start(exe)
	}
	return start(exe, "--user-data-dir="+dataDir)
}

// FocusDesktop uses wmctrl when installed; otherwise it reports that it can't.
func (Linux) FocusDesktop(pid int) error {
	out, err := output("wmctrl", "-lp")
	if err != nil {
		return err
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) > 2 && f[2] == strconv.Itoa(pid) {
			_, err := output("wmctrl", "-ia", f[0])
			return err
		}
	}
	return os.ErrNotExist
}

// QuitDesktop sends SIGTERM.
func (Linux) QuitDesktop(pid int) error { return syscall.Kill(pid, syscall.SIGTERM) }

// CLICommand returns the shell command for a CLI profile.
func (Linux) CLICommand(configDir string) string { return posixCLICommand(configDir) }

// OpenCLI opens the first terminal found, running claude for the profile and
// then an interactive shell so the window stays open.
func (l Linux) OpenCLI(configDir string) error {
	script := l.CLICommand(configDir) + `; exec "${SHELL:-sh}"`
	terminals := [][]string{
		{"x-terminal-emulator", "-e", "sh", "-c", script},
		{"gnome-terminal", "--", "sh", "-c", script},
		{"konsole", "-e", "sh", "-c", script},
		{"xfce4-terminal", "-x", "sh", "-c", script},
		{"kitty", "sh", "-c", script},
		{"alacritty", "-e", "sh", "-c", script},
		{"xterm", "-e", "sh", "-c", script},
	}
	for _, t := range terminals {
		if _, err := exec.LookPath(t[0]); err == nil {
			return start(t[0], t[1:]...)
		}
	}
	return os.ErrNotExist
}

func (l Linux) autostartPath() string {
	return filepath.Join(l.configHome(), "autostart", "tandem-for-claude.desktop")
}

// SetAutostart writes or removes an XDG autostart entry.
func (l Linux) SetAutostart(enable bool, exe string) error {
	path := l.autostartPath()
	if !enable {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	entry := "[Desktop Entry]\nType=Application\nName=Tandem for Claude\nExec=" +
		strconv.Quote(exe) + " --background\nX-GNOME-Autostart-enabled=true\n"
	return os.WriteFile(path, []byte(entry), 0o644)
}

// AutostartEnabled reports whether the autostart entry exists.
func (l Linux) AutostartEnabled() bool { return exists(l.autostartPath()) }

// OpenFolder shows a folder with xdg-open.
func (Linux) OpenFolder(path string) error { return start("xdg-open", path) }

// LinkDir creates a symlink.
func (Linux) LinkDir(target, link string) error { return symlinkDir(target, link) }

// LinkFile creates a symlink.
func (Linux) LinkFile(target, link string) (bool, error) { return symlinkFile(target, link) }
