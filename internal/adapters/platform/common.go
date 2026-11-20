// Package platform implements ports.Platform for macOS, Windows and Linux.
// Each OS has its own file, chosen by Go build constraints.
package platform

import (
	"context"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Timeout bounds every helper command the app runs.
const Timeout = 15 * time.Second

// output runs a command with a timeout and returns its standard output.
func output(name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), Timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	hide(cmd)
	b, err := cmd.Output()
	return string(b), err
}

// start launches a program detached from this app and doesn't wait for it.
func start(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	detach(cmd) // not hidden: these are windows the user wants to see
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

var dataDirRe = regexp.MustCompile(`--user-data-dir=(.+?)(?:\s+--|$)`)

// dataDirFromCommand extracts --user-data-dir from a process command line.
func dataDirFromCommand(cmdline string) string {
	m := dataDirRe.FindStringSubmatch(strings.TrimSpace(cmdline))
	if m == nil {
		return ""
	}
	return strings.Trim(strings.TrimSpace(m[1]), `"`)
}

// posixQuote quotes a value for a POSIX shell.
func posixQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// posixCLICommand is the shell command for a CLI profile on macOS and Linux.
func posixCLICommand(configDir string) string {
	if configDir == "" {
		return "claude"
	}
	return "CLAUDE_CONFIG_DIR=" + posixQuote(configDir) + " claude"
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// symlinkDir and symlinkFile are the POSIX link implementations.
func symlinkDir(target, link string) error { return os.Symlink(target, link) }

func symlinkFile(target, link string) (bool, error) {
	if err := os.Symlink(target, link); err != nil {
		return false, err
	}
	return true, nil
}
