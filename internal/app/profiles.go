package app

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/naseebnaushad/tandem-for-claude/internal/domain"
	"github.com/naseebnaushad/tandem-for-claude/internal/ports"
)

// Paths are the folders the app works with.
type Paths struct {
	Home       string // ~/.claude-profile: profiles, backups, state, settings, log
	ClaudeData string // Claude desktop's default data folder
	ClaudeDir  string // Claude Code's shared folder, ~/.claude
	ClaudeJSON string // Claude Code's global state, ~/.claude.json
}

// DesktopDir is where desktop profiles keep their Claude data folder.
func (p Paths) DesktopDir(id string) string { return filepath.Join(p.Home, "profiles", id) }

// CLIDir is where CLI profiles keep their Claude Code config folder.
func (p Paths) CLIDir(id string) string { return filepath.Join(p.Home, "cli", id) }

// ErrUser is an error whose message is safe and useful to show the user.
type ErrUser struct{ Msg string }

func (e ErrUser) Error() string { return e.Msg }

// ProfileView is a profile plus what the UI needs to show it.
type ProfileView struct {
	domain.Profile
	Folder  string `json:"folder"`
	Running bool   `json:"running"` // desktop only; CLI sessions can't be detected
	Command string `json:"command"` // CLI only: the command to run by hand
}

// Profiles manages named profiles.
type Profiles struct {
	Paths    Paths
	Repo     ports.ProfileRepo
	Platform ports.Platform

	mu       sync.Mutex
	reopen   []string // data folders to reopen after a sign-in
	signInID string
}

// All returns the built-in profiles followed by the user's, in order.
func (p *Profiles) All() ([]domain.Profile, error) {
	saved, err := p.Repo.Load()
	if err != nil {
		return nil, err
	}
	out := domain.DefaultProfiles()
	for i, b := range out { // built-ins may be renamed
		for _, s := range saved {
			if s.ID == b.ID && s.Name != "" {
				out[i].Name = s.Name
			}
		}
	}
	for _, s := range saved {
		if !s.Builtin {
			out = append(out, s)
		}
	}
	return out, nil
}

// DataDir returns the Claude data folder of a desktop profile ("" = default).
func (p *Profiles) DataDir(pr domain.Profile) string {
	if pr.ID == domain.DefaultDesktopID {
		return ""
	}
	return p.Paths.DesktopDir(pr.ID)
}

// ConfigDir returns the Claude Code folder of a CLI profile ("" = default).
func (p *Profiles) ConfigDir(pr domain.Profile) string {
	if pr.ID == domain.DefaultCLIID {
		return ""
	}
	return p.Paths.CLIDir(pr.ID)
}

// List returns every profile with its running state.
func (p *Profiles) List() ([]ProfileView, error) {
	all, err := p.All()
	if err != nil {
		return nil, err
	}
	running := p.runningSet()
	var out []ProfileView
	for _, pr := range all {
		v := ProfileView{Profile: pr}
		if pr.Kind == domain.KindDesktop {
			v.Folder = p.DataDir(pr)
			if v.Folder == "" {
				v.Folder = p.Paths.ClaudeData
			}
			v.Running = running[cleanPath(p.DataDir(pr))]
		} else {
			v.Folder = p.ConfigDir(pr)
			if v.Folder == "" {
				v.Folder = p.Paths.ClaudeDir
			}
			v.Command = p.Platform.CLICommand(p.ConfigDir(pr))
		}
		out = append(out, v)
	}
	return out, nil
}

func cleanPath(s string) string {
	if s == "" {
		return ""
	}
	return filepath.Clean(s)
}

func (p *Profiles) runningSet() map[string]bool {
	apps, _ := p.Platform.RunningDesktop()
	m := map[string]bool{}
	for _, a := range apps {
		m[cleanPath(a.DataDir)] = true
	}
	return m
}

func (p *Profiles) find(id string) (domain.Profile, []domain.Profile, error) {
	all, err := p.All()
	if err != nil {
		return domain.Profile{}, nil, err
	}
	for _, pr := range all {
		if pr.ID == id {
			return pr, all, nil
		}
	}
	return domain.Profile{}, all, ErrUser{"That profile no longer exists."}
}

func (p *Profiles) saveUser(all []domain.Profile) error {
	var keep []domain.Profile
	for _, pr := range all {
		if pr.Builtin {
			def := domain.DefaultProfiles()
			for _, d := range def {
				if d.ID == pr.ID && d.Name != pr.Name {
					keep = append(keep, pr) // remember a renamed built-in
				}
			}
			continue
		}
		keep = append(keep, pr)
	}
	return p.Repo.Save(keep)
}

func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "p" + hex.EncodeToString(b)
}

// Create adds a profile with a display name and kind and prepares its folder.
func (p *Profiles) Create(name string, kind domain.ProfileKind) (domain.Profile, error) {
	clean, ok := domain.NormaliseProfileName(name)
	if !ok {
		return domain.Profile{}, ErrUser{fmt.Sprintf("Use a name of 1 to %d characters.", domain.MaxProfileName)}
	}
	if !domain.ValidKind(kind) {
		return domain.Profile{}, ErrUser{"Choose Desktop or Terminal."}
	}
	all, err := p.All()
	if err != nil {
		return domain.Profile{}, err
	}
	if domain.NameTaken(all, clean, "") {
		return domain.Profile{}, ErrUser{"Another profile already has that name."}
	}
	pr := domain.Profile{ID: newID(), Name: clean, Kind: kind}
	if kind == domain.KindDesktop {
		if err := os.MkdirAll(p.Paths.DesktopDir(pr.ID), 0o700); err != nil {
			return domain.Profile{}, err
		}
	} else if err := p.setUpCLI(p.Paths.CLIDir(pr.ID)); err != nil {
		return domain.Profile{}, err
	}
	if err := p.saveUser(append(all, pr)); err != nil {
		return domain.Profile{}, err
	}
	return pr, nil
}

// Rename changes a profile's display name. Folders never move.
func (p *Profiles) Rename(id, name string) error {
	clean, ok := domain.NormaliseProfileName(name)
	if !ok {
		return ErrUser{fmt.Sprintf("Use a name of 1 to %d characters.", domain.MaxProfileName)}
	}
	_, all, err := p.find(id)
	if err != nil {
		return err
	}
	if domain.NameTaken(all, clean, id) {
		return ErrUser{"Another profile already has that name."}
	}
	for i := range all {
		if all[i].ID == id {
			all[i].Name = clean
		}
	}
	return p.saveUser(all)
}

// Delete removes a profile. Its folder (and so its sign-in) is deleted only
// with deleteData; shared chats and settings are never touched.
func (p *Profiles) Delete(id string, deleteData bool) error {
	pr, all, err := p.find(id)
	if err != nil {
		return err
	}
	if pr.Builtin {
		return ErrUser{"The default profiles can't be deleted."}
	}
	if pr.Kind == domain.KindDesktop && p.runningSet()[cleanPath(p.DataDir(pr))] {
		return ErrUser{"Quit this profile's Claude window first."}
	}
	var keep []domain.Profile
	for _, x := range all {
		if x.ID != id {
			keep = append(keep, x)
		}
	}
	if err := p.saveUser(keep); err != nil {
		return err
	}
	if !deleteData {
		return nil
	}
	if pr.Kind == domain.KindCLI {
		return p.removeCLI(p.Paths.CLIDir(id))
	}
	return os.RemoveAll(p.Paths.DesktopDir(id))
}

// Open starts the profile: a new desktop window or a terminal.
func (p *Profiles) Open(id string) error {
	pr, _, err := p.find(id)
	if err != nil {
		return err
	}
	if pr.Kind == domain.KindCLI {
		if dir := p.ConfigDir(pr); dir != "" {
			if err := p.RefreshCLI(dir); err != nil {
				return err
			}
		}
		return p.Platform.OpenCLI(p.ConfigDir(pr))
	}
	if p.Platform.ClaudeDesktopPath() == "" {
		return ErrUser{"Claude desktop was not found on this computer."}
	}
	// Already open: bring it forward instead of starting a second copy on
	// the same data folder, which Claude doesn't support.
	apps, _ := p.Platform.RunningDesktop()
	for _, a := range apps {
		if cleanPath(a.DataDir) == cleanPath(p.DataDir(pr)) {
			if err := p.Platform.FocusDesktop(a.PID); err != nil {
				return ErrUser{"This profile is already open, but its window couldn't be brought forward."}
			}
			return nil
		}
	}
	return p.Platform.LaunchDesktop(p.DataDir(pr))
}

// SignInOthers returns the names of the other desktop profiles that are open
// and would be closed by SignInStart.
func (p *Profiles) SignInOthers(id string) ([]string, error) {
	pr, all, err := p.find(id)
	if err != nil {
		return nil, err
	}
	running := p.runningSet()
	var names []string
	for _, x := range all {
		if x.Kind == domain.KindDesktop && x.ID != pr.ID && running[cleanPath(p.DataDir(x))] {
			names = append(names, x.Name)
		}
	}
	return names, nil
}

// SignInStart prepares a desktop profile's first sign-in (CR-02 D11): the
// sign-in link goes to the first Claude window that is running, so every
// other window is asked to quit, then only this profile is opened. The
// windows that were open are remembered for SignInDone.
func (p *Profiles) SignInStart(id string) error {
	pr, _, err := p.find(id)
	if err != nil {
		return err
	}
	if pr.Kind != domain.KindDesktop {
		return p.Open(id)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	apps, ok := p.Platform.RunningDesktop()
	if !ok {
		return ErrUser{"Couldn't check which Claude windows are open."}
	}
	mine := cleanPath(p.DataDir(pr))
	p.reopen, p.signInID = nil, id
	for _, a := range apps {
		if cleanPath(a.DataDir) == mine {
			continue
		}
		p.reopen = append(p.reopen, a.DataDir)
		if err := p.Platform.QuitDesktop(a.PID); err != nil {
			return ErrUser{"Couldn't close the other Claude windows. Quit them yourself, then try again."}
		}
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		left := 0
		now, _ := p.Platform.RunningDesktop()
		for _, a := range now {
			if cleanPath(a.DataDir) != mine {
				left++
			}
		}
		if left == 0 {
			break
		}
		if time.Now().After(deadline) {
			return ErrUser{"Some Claude windows are still open. Quit them yourself, then try again."}
		}
		time.Sleep(500 * time.Millisecond)
	}
	if p.runningSet()[mine] {
		return nil
	}
	return p.Platform.LaunchDesktop(p.DataDir(pr))
}

// SignInDone reopens the windows that SignInStart closed.
func (p *Profiles) SignInDone() error {
	p.mu.Lock()
	reopen := p.reopen
	p.reopen, p.signInID = nil, ""
	p.mu.Unlock()
	var firstErr error
	for _, d := range reopen {
		if err := p.Platform.LaunchDesktop(d); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// DesktopDataDirs maps sync labels to Claude data folders: "default" plus
// "profile-<id>" for every desktop profile.
func (p *Profiles) DesktopDataDirs() (map[string]string, error) {
	all, err := p.All()
	if err != nil {
		return nil, err
	}
	out := map[string]string{"default": p.Paths.ClaudeData}
	for _, pr := range all {
		if pr.Kind == domain.KindDesktop && !pr.Builtin {
			out["profile-"+pr.ID] = p.Paths.DesktopDir(pr.ID)
		}
	}
	return out, nil
}

const copiedMarker = ".tandem-copied"

// setUpCLI creates a CLI profile folder: links to the shared items, and a
// copy of .claude.json without account fields (CR-02 D12, OQ4).
func (p *Profiles) setUpCLI(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := p.copyClaudeJSON(filepath.Join(dir, ".claude.json")); err != nil {
		return err
	}
	return p.RefreshCLI(dir)
}

func (p *Profiles) copyClaudeJSON(dst string) error {
	f, err := os.Open(p.Paths.ClaudeJSON)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 50<<20))
	if err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var m map[string]any
	if err := d.Decode(&m); err != nil {
		return nil // unreadable: let Claude Code start fresh
	}
	for _, k := range domain.AccountFieldsInClaudeJSON {
		delete(m, k)
	}
	out, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, out, 0o600)
}

// RefreshCLI makes sure a CLI profile links every shared item that exists
// now, and re-copies files that had to be copied because links weren't
// allowed (Windows without Developer Mode, OQ3) when the original changed.
func (p *Profiles) RefreshCLI(dir string) error {
	copied := map[string]bool{}
	if b, err := os.ReadFile(filepath.Join(dir, copiedMarker)); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if l != "" {
				copied[l] = true
			}
		}
	}
	changed := false
	for _, item := range domain.SharedCLIItems {
		src := filepath.Join(p.Paths.ClaudeDir, item)
		dst := filepath.Join(dir, item)
		st, err := os.Stat(src)
		if err != nil {
			continue
		}
		if copied[item] {
			if dt, err := os.Stat(dst); err == nil && !st.ModTime().After(dt.ModTime()) {
				continue
			}
			if err := copyFile(src, dst); err != nil {
				return err
			}
			continue
		}
		if _, err := os.Lstat(dst); err == nil {
			continue
		}
		if st.IsDir() {
			if err := p.Platform.LinkDir(src, dst); err != nil {
				return err
			}
			continue
		}
		ok, err := p.Platform.LinkFile(src, dst)
		if err != nil {
			return err
		}
		if !ok {
			if err := copyFile(src, dst); err != nil {
				return err
			}
			copied[item] = true
			changed = true
		}
	}
	if changed {
		var lines []string
		for k := range copied {
			lines = append(lines, k)
		}
		return os.WriteFile(filepath.Join(dir, copiedMarker), []byte(strings.Join(lines, "\n")), 0o600)
	}
	return nil
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// removeCLI deletes a CLI profile folder without following its links: each
// shared item is unlinked first, so the shared ~/.claude is never touched.
func (p *Profiles) removeCLI(dir string) error {
	for _, item := range domain.SharedCLIItems {
		path := filepath.Join(dir, item)
		st, err := os.Lstat(path)
		if err != nil {
			continue
		}
		// Go reports Windows junctions as ModeIrregular, symlinks as ModeSymlink.
		if st.Mode()&os.ModeSymlink != 0 || st.Mode()&os.ModeIrregular != 0 {
			if err := os.Remove(path); err != nil {
				return err
			}
		}
	}
	// Anything still linking out means removal isn't safe.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if fi, err := os.Lstat(filepath.Join(dir, e.Name())); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return ErrUser{"The profile folder has unexpected links; it was left in place."}
		}
	}
	return os.RemoveAll(dir)
}

// Migrate adopts profile folders that exist without a profiles file (made by
// the earlier Python tool or by hand), naming each after its folder.
func (p *Profiles) Migrate() error {
	saved, err := p.Repo.Load()
	if err != nil || len(saved) > 0 {
		return err
	}
	var found []domain.Profile
	for _, k := range []struct {
		dir  string
		kind domain.ProfileKind
	}{{filepath.Join(p.Paths.Home, "profiles"), domain.KindDesktop}, {filepath.Join(p.Paths.Home, "cli"), domain.KindCLI}} {
		entries, _ := os.ReadDir(k.dir)
		for _, e := range entries {
			if e.IsDir() && domain.ProfileIDRe.MatchString(e.Name()) && !domain.NameTaken(found, e.Name(), "") {
				found = append(found, domain.Profile{ID: e.Name(), Name: e.Name(), Kind: k.kind})
			}
		}
	}
	if len(found) == 0 {
		return nil
	}
	return p.Repo.Save(found)
}
