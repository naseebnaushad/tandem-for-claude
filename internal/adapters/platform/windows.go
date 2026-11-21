//go:build windows

package platform

// Windows support follows Claude desktop's documented install layout but has
// not been tried on a real Windows machine yet (CR-0.1.0-02 OQ1).

import (
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/naseebnaushad/tango-for-claude/internal/ports"
)

const runKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
const runValue = "TangoForClaude"

// oldRunValue is the Run entry of 0.1.0-beta.1, when the app was called
// "Tandem for Claude".
const oldRunValue = "TandemForClaude"

// Win implements ports.Platform on Windows.
type Win struct{}

// New returns the platform for this OS.
func New() ports.Platform { return Win{} }

func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

func detach(*exec.Cmd) {}

// DefaultClaudeData is %APPDATA%\Claude.
func (Win) DefaultClaudeData() string { return filepath.Join(os.Getenv("APPDATA"), "Claude") }

// ClaudeDesktopPath looks in the usual install folders.
func (Win) ClaudeDesktopPath() string {
	local := os.Getenv("LOCALAPPDATA")
	candidates := []string{
		filepath.Join(local, "AnthropicClaude", "claude.exe"),
		filepath.Join(local, "Programs", "Claude", "Claude.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Claude", "Claude.exe"),
	}
	for _, c := range candidates {
		if exists(c) {
			return c
		}
	}
	// Squirrel installs keep versions in app-x.y.z folders; take the newest.
	dirs, _ := filepath.Glob(filepath.Join(local, "AnthropicClaude", "app-*"))
	sort.Strings(dirs)
	for i := len(dirs) - 1; i >= 0; i-- {
		if p := filepath.Join(dirs[i], "claude.exe"); exists(p) {
			return p
		}
	}
	return ""
}

// RunningDesktop asks Windows for claude.exe command lines. Electron helper
// processes carry --type=, the main process doesn't.
func (Win) RunningDesktop() ([]ports.RunningApp, bool) {
	out, err := output("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		`Get-CimInstance Win32_Process -Filter "Name='claude.exe'" | ForEach-Object { "$($_.ProcessId)`+"`t"+`$($_.CommandLine)" }`)
	if err != nil {
		return nil, false
	}
	var apps []ports.RunningApp
	for _, line := range strings.Split(out, "\n") {
		pidStr, cmdline, ok := strings.Cut(strings.TrimSpace(line), "\t")
		if !ok || strings.Contains(cmdline, "--type=") {
			continue
		}
		pid, err := strconv.Atoi(pidStr)
		if err != nil {
			continue
		}
		apps = append(apps, ports.RunningApp{PID: pid, DataDir: dataDirFromCommand(cmdline)})
	}
	return apps, true
}

// LaunchDesktop starts Claude with its own data folder.
func (w Win) LaunchDesktop(dataDir string) error {
	exe := w.ClaudeDesktopPath()
	if exe == "" {
		return os.ErrNotExist
	}
	if dataDir == "" {
		return start(exe)
	}
	return start(exe, "--user-data-dir="+dataDir)
}

// FocusDesktop activates the process's window.
func (Win) FocusDesktop(pid int) error {
	_, err := output("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"(New-Object -ComObject WScript.Shell).AppActivate("+strconv.Itoa(pid)+") | Out-Null")
	return err
}

// QuitDesktop asks the process to close (no /F, so it can save its state).
func (Win) QuitDesktop(pid int) error {
	_, err := output("taskkill.exe", "/PID", strconv.Itoa(pid))
	return err
}

// CLICommand returns the cmd.exe command for a CLI profile.
func (Win) CLICommand(configDir string) string {
	if configDir == "" {
		return "claude"
	}
	return `set "CLAUDE_CONFIG_DIR=` + configDir + `" && claude`
}

// OpenCLI opens a new console window running claude for the profile.
func (w Win) OpenCLI(configDir string) error {
	cmd := exec.Command("cmd.exe")
	// Raw command line: start a new window that keeps running after claude exits.
	cmd.SysProcAttr = &syscall.SysProcAttr{CmdLine: `cmd.exe /c start "Claude" cmd.exe /k "` + w.CLICommand(configDir) + `"`}
	return cmd.Start()
}

// SetAutostart adds or removes the per-user Run entry.
func (Win) SetAutostart(enable bool, exe string) error {
	if !enable {
		_, _ = output("reg.exe", "delete", runKey, "/v", runValue, "/f")
		return nil
	}
	_, err := output("reg.exe", "add", runKey, "/v", runValue, "/t", "REG_SZ", "/d", `"`+exe+`" --background`, "/f")
	return err
}

// AutostartEnabled reports whether the Run entry exists.
func (Win) AutostartEnabled() bool {
	_, err := output("reg.exe", "query", runKey, "/v", runValue)
	return err == nil
}

// MigrateOldAutostart replaces the Run entry of the app's old name with one
// for this app, keeping the user's choice. True if it found one.
func (w Win) MigrateOldAutostart(exe string) bool {
	if _, err := output("reg.exe", "query", runKey, "/v", oldRunValue); err != nil {
		return false
	}
	_, _ = output("reg.exe", "delete", runKey, "/v", oldRunValue, "/f")
	return w.SetAutostart(true, exe) == nil
}

// OpenFolder shows a folder in Explorer.
func (Win) OpenFolder(path string) error { return start("explorer.exe", path) }

// LinkDir creates a directory junction, which needs no admin rights.
func (Win) LinkDir(target, link string) error {
	_, err := output("cmd.exe", "/c", "mklink", "/J", link, target)
	return err
}

// LinkFile tries a symlink; without Developer Mode or admin rights it returns
// ok=false so the caller copies the file instead (CR-02 OQ3).
func (Win) LinkFile(target, link string) (bool, error) {
	if err := os.Symlink(target, link); err != nil {
		return false, nil
	}
	return true, nil
}
