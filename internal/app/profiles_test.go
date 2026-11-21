package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/naseebnaushad/tango-for-claude/internal/adapters/fsstore"
	"github.com/naseebnaushad/tango-for-claude/internal/domain"
	"github.com/naseebnaushad/tango-for-claude/internal/ports"
)

// fakePlatform records launches and lets tests decide what is running.
type fakePlatform struct {
	running   []ports.RunningApp
	launched  []string
	quit      []int
	noLinks   bool
	focused   []int
	autostart bool
}

func (f *fakePlatform) DefaultClaudeData() string { return "" }
func (f *fakePlatform) ClaudeDesktopPath() string { return "/Applications/Claude.app" }
func (f *fakePlatform) RunningDesktop() ([]ports.RunningApp, bool) {
	return f.running, true
}
func (f *fakePlatform) LaunchDesktop(d string) error {
	f.launched = append(f.launched, d)
	f.running = append(f.running, ports.RunningApp{PID: 100 + len(f.launched), DataDir: d})
	return nil
}
func (f *fakePlatform) FocusDesktop(pid int) error {
	f.focused = append(f.focused, pid)
	return nil
}
func (f *fakePlatform) QuitDesktop(pid int) error {
	f.quit = append(f.quit, pid)
	var keep []ports.RunningApp
	for _, a := range f.running {
		if a.PID != pid {
			keep = append(keep, a)
		}
	}
	f.running = keep
	return nil
}
func (f *fakePlatform) OpenCLI(string) error       { return nil }
func (f *fakePlatform) CLICommand(d string) string { return "claude " + d }
func (f *fakePlatform) SetAutostart(e bool, _ string) error {
	f.autostart = e
	return nil
}
func (f *fakePlatform) AutostartEnabled() bool    { return f.autostart }
func (f *fakePlatform) OpenFolder(string) error   { return nil }
func (f *fakePlatform) LinkDir(t, l string) error { return os.Symlink(t, l) }
func (f *fakePlatform) LinkFile(t, l string) (bool, error) {
	if f.noLinks {
		return false, nil
	}
	return true, os.Symlink(t, l)
}

func newProfiles(t *testing.T) (*Profiles, *fakePlatform) {
	root := t.TempDir()
	claude := filepath.Join(root, "dotclaude")
	must(t, os.MkdirAll(filepath.Join(claude, "projects"), 0o700))
	must(t, os.MkdirAll(filepath.Join(claude, "skills"), 0o700))
	must(t, os.WriteFile(filepath.Join(claude, "CLAUDE.md"), []byte("rules"), 0o600))
	must(t, os.WriteFile(filepath.Join(claude, "settings.json"), []byte("{}"), 0o600))
	cj := filepath.Join(root, "claude.json")
	must(t, os.WriteFile(cj, []byte(`{"oauthAccount":{"x":1},"userID":"u","mcpServers":{"m":{}},"n":1794479906673}`), 0o600))
	plat := &fakePlatform{}
	p := &Profiles{
		Paths:    Paths{Home: filepath.Join(root, "home"), ClaudeData: filepath.Join(root, "AppSupport"), ClaudeDir: claude, ClaudeJSON: cj},
		Repo:     fsstore.ProfileFile{Path: filepath.Join(root, "home", "profiles.json")},
		Platform: plat,
	}
	return p, plat
}

// PROF-001
func TestCreateRenameDeleteDesktop(t *testing.T) {
	p, _ := newProfiles(t)
	pr, err := p.Create("  Work ", domain.KindDesktop)
	must(t, err)
	if pr.Name != "Work" || !domain.ProfileIDRe.MatchString(pr.ID) {
		t.Fatal(pr)
	}
	if _, err := p.Create("work", domain.KindCLI); err == nil {
		t.Fatal("duplicate name accepted")
	}
	must(t, p.Rename(pr.ID, "Seat 2"))
	list, _ := p.List()
	if len(list) != 3 || list[2].Name != "Seat 2" || list[0].ID != domain.DefaultDesktopID {
		t.Fatal(list)
	}
	must(t, p.Delete(pr.ID, false))
	if _, err := os.Stat(p.Paths.DesktopDir(pr.ID)); err != nil {
		t.Fatal("data deleted without being asked")
	}
}

// PROF-002
func TestBuiltinsCanBeRenamedNotDeleted(t *testing.T) {
	p, _ := newProfiles(t)
	must(t, p.Rename(domain.DefaultDesktopID, "Main seat"))
	all, _ := p.All()
	if all[0].Name != "Main seat" {
		t.Fatal(all[0])
	}
	if err := p.Delete(domain.DefaultDesktopID, true); err == nil {
		t.Fatal("built-in deleted")
	}
}

// PROF-003
func TestCLIProfileLinksSharedAndStripsAccount(t *testing.T) {
	p, _ := newProfiles(t)
	pr, err := p.Create("Terminal 2", domain.KindCLI)
	must(t, err)
	dir := p.Paths.CLIDir(pr.ID)
	for _, item := range []string{"projects", "skills", "CLAUDE.md", "settings.json"} {
		st, err := os.Lstat(filepath.Join(dir, item))
		if err != nil || st.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s not linked", item)
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, ".claude.json"))
	must(t, err)
	var m map[string]any
	must(t, json.Unmarshal(raw, &m))
	if _, ok := m["oauthAccount"]; ok {
		t.Fatal("account copied")
	}
	if _, ok := m["mcpServers"]; !ok {
		t.Fatal("MCP servers not carried over")
	}
	if string(raw) == "" || !json.Valid(raw) || !containsNumber(raw, "1794479906673") {
		t.Fatal("number precision lost")
	}
}

func containsNumber(raw []byte, n string) bool {
	return len(raw) > 0 && string(raw) != "" && (stringIndex(string(raw), n) >= 0)
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// PROF-004
func TestDeleteCLIProfileNeverTouchesShared(t *testing.T) {
	p, _ := newProfiles(t)
	pr, err := p.Create("T", domain.KindCLI)
	must(t, err)
	must(t, p.Delete(pr.ID, true))
	if _, err := os.Stat(p.Paths.CLIDir(pr.ID)); !os.IsNotExist(err) {
		t.Fatal("profile folder left")
	}
	for _, item := range []string{"projects", "skills", "CLAUDE.md", "settings.json"} {
		if _, err := os.Stat(filepath.Join(p.Paths.ClaudeDir, item)); err != nil {
			t.Fatalf("shared %s deleted", item)
		}
	}
}

// PROF-005
func TestCopyFallbackRecopiesWhenChanged(t *testing.T) {
	p, plat := newProfiles(t)
	plat.noLinks = true
	pr, err := p.Create("Win", domain.KindCLI)
	must(t, err)
	dst := filepath.Join(p.Paths.CLIDir(pr.ID), "CLAUDE.md")
	if b, _ := os.ReadFile(dst); string(b) != "rules" {
		t.Fatal("not copied")
	}
	src := filepath.Join(p.Paths.ClaudeDir, "CLAUDE.md")
	must(t, os.WriteFile(src, []byte("new rules"), 0o600))
	future := mustStat(t, dst).ModTime().Add(2e9)
	must(t, os.Chtimes(src, future, future))
	must(t, p.RefreshCLI(p.Paths.CLIDir(pr.ID)))
	if b, _ := os.ReadFile(dst); string(b) != "new rules" {
		t.Fatal("not re-copied")
	}
}

// PROF-010: a profile made by 0.1.0-beta.1 (then "Tandem for Claude") still
// has its copied files refreshed, and the record moves to the new name.
func TestOldCopiedMarkerStillRead(t *testing.T) {
	p, plat := newProfiles(t)
	plat.noLinks = true
	pr, err := p.Create("Win", domain.KindCLI)
	must(t, err)
	dir := p.Paths.CLIDir(pr.ID)
	must(t, os.Rename(filepath.Join(dir, copiedMarker), filepath.Join(dir, oldCopiedMarker)))
	plat.noLinks = false // links work now, so only the old record says "copy"
	src := filepath.Join(p.Paths.ClaudeDir, "CLAUDE.md")
	dst := filepath.Join(dir, "CLAUDE.md")
	must(t, os.WriteFile(src, []byte("new rules"), 0o600))
	future := mustStat(t, dst).ModTime().Add(2e9)
	must(t, os.Chtimes(src, future, future))
	must(t, p.RefreshCLI(dir))
	if b, _ := os.ReadFile(dst); string(b) != "new rules" {
		t.Fatal("old record ignored: not re-copied")
	}
	if _, err := os.Stat(filepath.Join(dir, copiedMarker)); err != nil {
		t.Fatal("record not carried over to the new name")
	}
}

func mustStat(t *testing.T, p string) os.FileInfo {
	st, err := os.Stat(p)
	must(t, err)
	return st
}

// PROF-006
func TestSignInClosesOthersAndReopens(t *testing.T) {
	p, plat := newProfiles(t)
	pr, err := p.Create("Seat 2", domain.KindDesktop)
	must(t, err)
	plat.running = []ports.RunningApp{{PID: 7, DataDir: ""}}
	others, _ := p.SignInOthers(pr.ID)
	if len(others) != 1 {
		t.Fatal(others)
	}
	must(t, p.SignInStart(pr.ID))
	if len(plat.quit) != 1 || plat.quit[0] != 7 || plat.launched[0] != p.Paths.DesktopDir(pr.ID) {
		t.Fatal(plat.quit, plat.launched)
	}
	must(t, p.SignInDone())
	if plat.launched[len(plat.launched)-1] != "" {
		t.Fatal("default window not reopened")
	}
}

// PROF-007
func TestOpenFocusesWhenAlreadyRunning(t *testing.T) {
	p, plat := newProfiles(t)
	plat.running = []ports.RunningApp{{PID: 7, DataDir: ""}}
	must(t, p.Open(domain.DefaultDesktopID))
	if len(plat.launched) != 0 || len(plat.focused) != 1 || plat.focused[0] != 7 {
		t.Fatal(plat.launched, plat.focused)
	}
}

// PROF-008
func TestMigrateAdoptsExistingFolders(t *testing.T) {
	p, _ := newProfiles(t)
	must(t, os.MkdirAll(filepath.Join(p.Paths.Home, "profiles", "seat2"), 0o700))
	must(t, os.MkdirAll(filepath.Join(p.Paths.Home, "cli", "oq2-test"), 0o700))
	must(t, p.Migrate())
	all, _ := p.All()
	if len(all) != 4 || all[2].ID != "seat2" || all[3].Kind != domain.KindCLI {
		t.Fatal(all)
	}
	dirs, _ := p.DesktopDataDirs()
	if dirs["profile-seat2"] == "" || dirs["default"] == "" {
		t.Fatal(dirs)
	}
}

// PROF-009
func TestBadNamesRejected(t *testing.T) {
	p, _ := newProfiles(t)
	for _, n := range []string{"", "  ", "\x1b"} {
		if _, err := p.Create(n, domain.KindDesktop); err == nil {
			t.Fatalf("%q accepted", n)
		}
	}
	if _, err := p.Create("ok", domain.ProfileKind("evil")); err == nil {
		t.Fatal("bad kind accepted")
	}
}
