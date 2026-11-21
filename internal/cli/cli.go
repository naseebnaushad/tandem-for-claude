// Package cli is the command-line front-end. It only parses arguments,
// calls the Manager and prints results.
package cli

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/naseebnaushad/tango-for-claude/internal/app"
	"github.com/naseebnaushad/tango-for-claude/internal/domain"
)

// Exit codes, as in the Python tool.
const (
	ExitOK    = 0
	ExitFail  = 1
	ExitUsage = 2
)

const usage = `Tango for Claude: use several Claude seats and keep every Code-tab chat.

Usage:
  tango                        open the app window
  tango status                 show chat lists and what sync would do
  tango sync [--apply] [--quiet]
                                copy chat list entries between seats (dry run by default)
  tango profile list
  tango profile add <name> [--kind desktop|cli]
  tango profile rename <id> <new name>
  tango profile open <id>
  tango profile signin <id>    first sign-in of a desktop profile
  tango profile remove <id> [--delete-data] [--yes]
  tango backups list
  tango backups restore <id> [--yes]
  tango autostart on|off|status
  tango retention [days]       show or set how long Claude Code keeps chats
  tango version
`

// Run executes one command and returns the exit code.
func Run(m *app.Manager, args []string, version string, stdout io.Writer, stdin io.Reader) int {
	p := printer{stdout}
	if len(args) == 0 {
		p.say(usage)
		return ExitUsage
	}
	var err error
	switch args[0] {
	case "status":
		err = status(m, p)
	case "sync":
		return syncCmd(m, p, args[1:])
	case "profile":
		err = profile(m, p, args[1:], stdin)
	case "backups":
		err = backups(m, p, args[1:], stdin)
	case "autostart":
		err = autostart(m, p, args[1:])
	case "retention":
		err = retention(m, p, args[1:])
	case "version", "--version":
		p.say("Tango for Claude " + version)
	case "help", "-h", "--help":
		p.say(usage)
	default:
		p.say(usage)
		return ExitUsage
	}
	if err != nil {
		if u, ok := err.(usageErr); ok {
			p.say(string(u))
			return ExitUsage
		}
		p.say("Error: " + app.UserMessage(err))
		return ExitFail
	}
	return ExitOK
}

type usageErr string

func (u usageErr) Error() string { return string(u) }

type printer struct{ w io.Writer }

func (p printer) say(format string, a ...any) { fmt.Fprintf(p.w, format+"\n", a...) }

func describe(a domain.Action) string {
	title := domain.CleanText(a.Title, 60)
	if title == "" {
		title = a.ChatID[:8]
	}
	switch a.Kind {
	case domain.ActAdd:
		return fmt.Sprintf("add      %s  ->  %s  (from %s)", title, a.Target.Label(), a.Source.Label())
	case domain.ActUpdate:
		return fmt.Sprintf("update   %s  ->  %s  (newer in %s)", title, a.Target.Label(), a.Source.Label())
	case domain.ActTombstone:
		return fmt.Sprintf("deleted  %s  ->  %s", a.ChatID[:8], a.Target.Label())
	}
	where := ""
	if a.Target != nil {
		where = "  (" + a.Target.Label() + ")"
	}
	return fmt.Sprintf("%-8s %s%s: %s", a.Kind, title, where, a.Reason)
}

func printResult(p printer, r *app.SyncResult, applied bool) {
	if r.AppRunning {
		p.say("Claude is running: only new files are added.")
	} else {
		p.say("Claude is closed.")
	}
	p.say("Chat lists:")
	for _, s := range r.Snapshots {
		p.say("  %-30s org %s  %4d chats  %4d deleted", s.Store.Label(), s.Store.Org[:8], len(s.Entries), len(s.Tombstones))
	}
	for _, pr := range r.Problems {
		p.say("  ! %s", domain.CleanText(pr, 200))
	}
	if r.Paused {
		p.say("Sync is paused: some chat list files have an unexpected format (Claude may have been updated). Nothing was written.")
	}
	if len(r.Actions) == 0 {
		p.say("Everything is in sync.")
		return
	}
	if applied {
		p.say("Applied:")
	} else {
		p.say("Plan (dry run; add --apply to write):")
	}
	done := map[string]bool{}
	for _, a := range r.Applied {
		done[string(a.Kind)+a.ChatID+a.Target.Label()] = true
	}
	for _, a := range r.Actions {
		if applied && a.Writes() && !done[string(a.Kind)+a.ChatID+a.Target.Label()] {
			continue
		}
		p.say("  %s", describe(a))
	}
	if len(r.Unchanged) > 0 {
		p.say("  %d left as they were (changed by Claude meanwhile, or Claude started).", len(r.Unchanged))
	}
	for what, why := range r.Failed {
		p.say("  ! failed: %s (%s)", what, why)
	}
	if r.BackupID != "" {
		p.say("Backup: %s", r.BackupID)
	}
}

func status(m *app.Manager, p printer) error {
	r, err := m.Sync(false)
	if err != nil {
		return err
	}
	printResult(p, r, false)
	views, err := m.Profiles.List()
	if err != nil {
		return err
	}
	var names []string
	for _, v := range views {
		names = append(names, v.Name)
	}
	p.say("Profiles: %s", strings.Join(names, ", "))
	s := m.Settings()
	p.say("Start at login: %v", s.StartAtLogin)
	return nil
}

func syncCmd(m *app.Manager, p printer, args []string) int {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	fs.SetOutput(p.w)
	apply := fs.Bool("apply", false, "write the changes")
	quiet := fs.Bool("quiet", false, "print only when something changed or failed")
	if fs.Parse(args) != nil {
		return ExitUsage
	}
	r, err := m.Sync(*apply)
	if err != nil {
		p.say("Error: " + app.UserMessage(err))
		return ExitFail
	}
	if *quiet {
		if len(r.Applied) > 0 || len(r.Failed) > 0 || r.Paused {
			p.say("applied %d, failed %d, paused %v", len(r.Applied), len(r.Failed), r.Paused)
		}
	} else {
		printResult(p, r, *apply)
	}
	if len(r.Failed) > 0 {
		return ExitFail
	}
	return ExitOK
}

func confirm(p printer, stdin io.Reader, q string, yes bool) bool {
	if yes {
		return true
	}
	if f, ok := stdin.(*os.File); !ok || !isTerminal(f) {
		p.say("Not confirmed (no terminal). Add --yes to confirm.")
		return false
	}
	fmt.Fprintf(p.w, "%s [y/N] ", q)
	line, _ := bufio.NewReader(stdin).ReadString('\n')
	a := strings.ToLower(strings.TrimSpace(line))
	return a == "y" || a == "yes"
}

func isTerminal(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// splitFlags separates --flags from positional words, so flags may come last.
func splitFlags(args []string) (pos []string, flags map[string]string) {
	flags = map[string]string{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			pos = append(pos, a)
			continue
		}
		k, v, ok := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		if !ok && k == "kind" && i+1 < len(args) {
			v, i = args[i+1], i+1
		}
		flags[k] = v
	}
	return pos, flags
}

func profile(m *app.Manager, p printer, args []string, stdin io.Reader) error {
	pos, flags := splitFlags(args)
	if len(pos) == 0 {
		return usageErr("Usage: tango profile list|add|rename|open|signin|remove")
	}
	switch pos[0] {
	case "list":
		views, err := m.Profiles.List()
		if err != nil {
			return err
		}
		for _, v := range views {
			state := ""
			if v.Running {
				state = "running"
			}
			p.say("%-18s %-8s %-28s %s", v.ID, v.Kind, domain.CleanText(v.Name, 28), state)
		}
		return nil
	case "add":
		if len(pos) < 2 {
			return usageErr("Usage: tango profile add <name> [--kind desktop|cli]")
		}
		kind := domain.ProfileKind(flags["kind"])
		if kind == "" {
			kind = domain.KindDesktop
		}
		pr, err := m.Profiles.Create(strings.Join(pos[1:], " "), kind)
		if err != nil {
			return err
		}
		p.say("Created %s profile %q (id %s).", pr.Kind, pr.Name, pr.ID)
		if pr.Kind == domain.KindDesktop {
			p.say("Next: tango profile signin %s", pr.ID)
		} else {
			p.say("Next: tango profile open %s, then log in with the seat for this profile.", pr.ID)
		}
		return nil
	case "rename":
		if len(pos) < 3 {
			return usageErr("Usage: tango profile rename <id> <new name>")
		}
		return m.Profiles.Rename(pos[1], strings.Join(pos[2:], " "))
	case "open":
		if len(pos) < 2 {
			return usageErr("Usage: tango profile open <id>")
		}
		return m.Profiles.Open(pos[1])
	case "signin":
		if len(pos) < 2 {
			return usageErr("Usage: tango profile signin <id>")
		}
		others, err := m.Profiles.SignInOthers(pos[1])
		if err != nil {
			return err
		}
		if len(others) > 0 && !confirm(p, stdin, "These Claude windows will close for the sign-in and reopen afterwards: "+strings.Join(others, ", ")+". Continue?", hasFlag(args, "--yes")) {
			return nil
		}
		if err := m.Profiles.SignInStart(pos[1]); err != nil {
			return err
		}
		p.say("Sign in with this profile's seat in the window that opened. Press Enter when done.")
		_, _ = bufio.NewReader(stdin).ReadString('\n')
		return m.Profiles.SignInDone()
	case "remove":
		if len(pos) < 2 {
			return usageErr("Usage: tango profile remove <id> [--delete-data] [--yes]")
		}
		del := hasFlag(args, "--delete-data")
		if del && !confirm(p, stdin, "Delete this profile's folder, including its sign-in? Shared chats stay.", hasFlag(args, "--yes")) {
			return nil
		}
		return m.Profiles.Delete(pos[1], del)
	}
	return usageErr("Usage: tango profile list|add|rename|open|signin|remove")
}

func hasFlag(args []string, f string) bool {
	for _, a := range args {
		if a == f {
			return true
		}
	}
	return false
}

func backups(m *app.Manager, p printer, args []string, stdin io.Reader) error {
	if len(args) == 0 {
		return usageErr("Usage: tango backups list|restore <id>")
	}
	switch args[0] {
	case "list":
		for _, b := range m.Backups.List() {
			p.say(b)
		}
		return nil
	case "restore":
		if len(args) < 2 {
			return usageErr("Usage: tango backups restore <id> [--yes]")
		}
		if !confirm(p, stdin, "Restore backup "+args[1]+"? Current chat lists are kept as claude-code-sessions.before-restore-<time>.", hasFlag(args, "--yes")) {
			return nil
		}
		restored, err := m.RestoreBackup(args[1])
		for _, r := range restored {
			p.say("Restored %s", r)
		}
		return err
	}
	return usageErr("Usage: tango backups list|restore <id>")
}

func retention(m *app.Manager, p printer, args []string) error {
	if len(args) == 0 {
		d, err := m.Retention()
		if err == nil {
			p.say("Claude Code keeps chats for %d days.", d)
		}
		return err
	}
	var days int
	if _, err := fmt.Sscanf(args[0], "%d", &days); err != nil {
		return usageErr("Usage: tango retention [days]  (30 to 3650)")
	}
	if err := m.SetRetention(days); err != nil {
		return err
	}
	p.say("Claude Code will keep chats for %d days.", days)
	return nil
}

func autostart(m *app.Manager, p printer, args []string) error {
	s := m.Settings()
	if len(args) == 0 || args[0] == "status" {
		p.say("Start at login: %v", s.StartAtLogin)
		return nil
	}
	switch args[0] {
	case "on":
		s.StartAtLogin = true
	case "off":
		s.StartAtLogin = false
	default:
		return usageErr("Usage: tango autostart on|off|status")
	}
	return m.SaveSettings(s)
}
