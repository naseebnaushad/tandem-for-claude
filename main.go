// Tandem for Claude: use several Claude seats side by side and keep every
// Code-tab chat on all of them. With arguments it runs as a command-line
// tool; without, it opens the app window and tray icon.
package main

import (
	"embed"
	"fmt"
	"os"

	"github.com/naseebnaushad/tandem-for-claude/internal/cli"
	"github.com/naseebnaushad/tandem-for-claude/internal/wiring"
	"github.com/naseebnaushad/tandem-for-claude/ui"
)

//go:embed all:frontend/dist
var assets embed.FS

// version is set at build time with -ldflags "-X main.version=...".
var version = "0.1.0-alpha"

func main() {
	m, err := wiring.Build()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Tandem for Claude couldn't start: its folder isn't usable.")
		os.Exit(cli.ExitFail)
	}
	args := os.Args[1:]
	if len(args) == 0 || (len(args) == 1 && args[0] == "--background") {
		background := len(args) == 1
		if err := ui.Run(m, assets, version, background); err != nil {
			fmt.Fprintln(os.Stderr, "Tandem for Claude stopped because of an error.")
			os.Exit(cli.ExitFail)
		}
		return
	}
	attachConsole()
	os.Exit(cli.Run(m, args, version, os.Stdout, os.Stdin))
}
