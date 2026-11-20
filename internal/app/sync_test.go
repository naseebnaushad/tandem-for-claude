package app

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/naseebnaushad/tandem-for-claude/internal/adapters/fsstore"
	"github.com/naseebnaushad/tandem-for-claude/internal/domain"
)

const (
	org = "00000000-0000-4000-8000-0000000000aa"
	a1  = "11111111-1111-4111-8111-111111111111"
	a2  = "22222222-2222-4222-8222-222222222222"
	c1  = "aaaaaaaa-0000-4000-8000-000000000001"
	c2  = "aaaaaaaa-0000-4000-8000-000000000002"
	t1  = "bbbbbbbb-0000-4000-8000-000000000001"
)

type fakeProbe struct{ running bool }

func (f fakeProbe) Running() ([]string, bool) {
	if f.running {
		return []string{""}, true
	}
	return nil, true
}

type fixture struct {
	t                    *testing.T
	root, appData, projs string
	backups              fsstore.Backups
	state                fsstore.State
}

func newFixture(t *testing.T) *fixture {
	root := t.TempDir()
	f := &fixture{t: t, root: root, appData: filepath.Join(root, "AppSupport"), projs: filepath.Join(root, "projects"),
		backups: fsstore.Backups{Dir: filepath.Join(root, "home", "backups")},
		state:   fsstore.State{Path: filepath.Join(root, "home", "state.json")}}
	must(t, os.MkdirAll(filepath.Join(f.projs, "-tmp-project"), 0o700))
	must(t, os.WriteFile(filepath.Join(f.projs, "-tmp-project", t1+".jsonl"), nil, 0o600))
	return f
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func entry(chat string, ts int64, extra map[string]any) map[string]any {
	m := map[string]any{"sessionId": "local_" + chat, "cliSessionId": t1, "cwd": "/tmp/project",
		"title": "Chat " + chat[len(chat)-1:], "lastActivityAt": ts, "bridgeSessionIds": []string{"bridge-x"},
		"remoteControlUserEnabled": true}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func (f *fixture) folder(account, dataDir string) string {
	if dataDir == "" {
		dataDir = f.appData
	}
	p := filepath.Join(dataDir, "claude-code-sessions", account, org)
	must(f.t, os.MkdirAll(p, 0o700))
	return p
}

func (f *fixture) put(account, chat string, data map[string]any, dataDir string) {
	b, _ := json.Marshal(data)
	must(f.t, os.WriteFile(filepath.Join(f.folder(account, dataDir), "local_"+chat+".json"), b, 0o600))
}

func (f *fixture) read(account, chat, dataDir string) map[string]any {
	b, err := os.ReadFile(filepath.Join(f.folder(account, dataDir), "local_"+chat+".json"))
	must(f.t, err)
	var m map[string]any
	must(f.t, json.Unmarshal(b, &m))
	return m
}

func (f *fixture) sync(running, apply bool, dirs map[string]string) *SyncResult {
	if dirs == nil {
		dirs = map[string]string{"default": f.appData}
	}
	s := Syncer{Store: fsstore.NewStore(dirs), Transcripts: fsstore.Transcripts{Dir: f.projs},
		Probe: fakeProbe{running}, Backups: f.backups, State: f.state}
	r, err := s.Sync(apply)
	must(f.t, err)
	return r
}

// SYNC-019
func TestApplyCopiesAndClearsAccountFields(t *testing.T) {
	f := newFixture(t)
	f.put(a1, c1, entry(c1, 100, nil), "")
	f.folder(a2, "")
	r := f.sync(true, true, nil)
	if len(r.Applied) != 1 {
		t.Fatal(r.Actions)
	}
	cp := f.read(a2, c1, "")
	if cp["cliSessionId"] != t1 || len(cp["bridgeSessionIds"].([]any)) != 0 || cp["remoteControlUserEnabled"] != false {
		t.Fatal(cp)
	}
	if cp["lastActivityAt"].(float64) != 100 {
		t.Fatal("timestamp changed")
	}
	if len(f.read(a1, c1, "")["bridgeSessionIds"].([]any)) != 1 {
		t.Fatal("source changed")
	}
	// Windows has no POSIX permission bits; files there are protected by the profile's ACLs.
	st, _ := os.Stat(filepath.Join(f.folder(a2, ""), "local_"+c1+".json"))
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatal(st.Mode())
	}
	files, _ := os.ReadDir(f.folder(a2, ""))
	for _, e := range files {
		if strings.HasPrefix(e.Name(), ".tandem-") {
			t.Fatal("temp file left")
		}
	}
}

// SYNC-020
func TestDryRunWritesNothing(t *testing.T) {
	f := newFixture(t)
	f.put(a1, c1, entry(c1, 100, nil), "")
	f.folder(a2, "")
	r := f.sync(true, false, nil)
	if len(r.Actions) != 1 || r.Actions[0].Kind != domain.ActAdd {
		t.Fatal(r.Actions)
	}
	if files, _ := os.ReadDir(f.folder(a2, "")); len(files) != 0 || len(f.backups.List()) != 0 {
		t.Fatal("wrote something")
	}
}

// SYNC-021
func TestRunningAppNeverOverwritten(t *testing.T) {
	f := newFixture(t)
	f.put(a1, c1, entry(c1, 100, map[string]any{"title": "old"}), "")
	f.put(a2, c1, entry(c1, 200, map[string]any{"title": "new"}), "")
	f.sync(true, true, nil)
	if f.read(a1, c1, "")["title"] != "old" {
		t.Fatal("overwrote while running")
	}
	f.sync(false, true, nil)
	if f.read(a1, c1, "")["title"] != "new" {
		t.Fatal("not updated when closed")
	}
}

// SYNC-022
func TestTombstoneCopiedEntryKept(t *testing.T) {
	f := newFixture(t)
	f.put(a2, c1, entry(c1, 100, nil), "")
	must(t, os.WriteFile(filepath.Join(f.folder(a1, ""), "deleted_"+c1), []byte("1794479906673"), 0o600))
	f.sync(true, true, nil)
	b, err := os.ReadFile(filepath.Join(f.folder(a2, ""), "deleted_"+c1))
	if err != nil || string(b) != "1794479906673" {
		t.Fatal(err, string(b))
	}
	if _, err := os.Stat(filepath.Join(f.folder(a2, ""), "local_"+c1+".json")); err != nil {
		t.Fatal("entry deleted")
	}
}

// SYNC-023 and format guard (D15)
func TestBadFilesReportedAndPause(t *testing.T) {
	f := newFixture(t)
	must(t, os.WriteFile(filepath.Join(f.folder(a1, ""), "local_"+c1+".json"), []byte("{not json"), 0o600))
	f.put(a1, c2, map[string]any{"unexpected": true}, "")
	f.folder(a2, "")
	r := f.sync(true, true, nil)
	if len(r.Problems) != 2 || !r.Paused || len(r.Applied) != 0 {
		t.Fatal(r.Problems, r.Paused)
	}
}

// SYNC-024
func TestSymlinkedEntryIgnored(t *testing.T) {
	f := newFixture(t)
	target := filepath.Join(f.root, "outside.json")
	b, _ := json.Marshal(entry(c1, 1, nil))
	must(t, os.WriteFile(target, b, 0o600))
	must(t, os.Symlink(target, filepath.Join(f.folder(a1, ""), "local_"+c1+".json")))
	f.folder(a2, "")
	r := f.sync(true, true, nil)
	for _, a := range r.Actions {
		if a.Kind == domain.ActAdd {
			t.Fatal("copied a symlink")
		}
	}
}

// SYNC-025
func TestAddNeverReplaces(t *testing.T) {
	f := newFixture(t)
	f.put(a2, c1, entry(c1, 1, map[string]any{"title": "mine"}), "")
	st := fsstore.NewStore(map[string]string{"default": f.appData})
	done, err := st.WriteEntry(domain.StoreID{DataDir: "default", Account: a2, Org: org}, c1, entry(c1, 1, map[string]any{"title": "theirs"}), false)
	if err != nil || done || f.read(a2, c1, "")["title"] != "mine" {
		t.Fatal(done, err)
	}
}

// SYNC-026
func TestProfileFolderTakesPart(t *testing.T) {
	f := newFixture(t)
	prof := filepath.Join(f.root, "profiles", "work")
	f.put(a1, c1, entry(c1, 100, nil), "")
	f.folder(a2, prof)
	f.sync(true, true, map[string]string{"default": f.appData, "profile-work": prof})
	if f.read(a2, c1, prof)["sessionId"] != "local_"+c1 {
		t.Fatal("not copied into profile")
	}
}

// SYNC-027
func TestWarnOnlyWhenBothSeatsUsed(t *testing.T) {
	f := newFixture(t)
	f.put(a1, c1, entry(c1, 100, nil), "")
	f.folder(a2, "")
	f.sync(true, true, nil)
	f.put(a1, c1, entry(c1, 150, nil), "")
	if hasKind(f.sync(true, false, nil).Actions, domain.ActWarn) {
		t.Fatal("warned for one seat")
	}
	f.put(a2, c1, entry(c1, 160, nil), "")
	if !hasKind(f.sync(true, false, nil).Actions, domain.ActWarn) {
		t.Fatal("no warning for two tandem")
	}
}

func hasKind(acts []domain.Action, k domain.ActionKind) bool {
	for _, a := range acts {
		if a.Kind == k {
			return true
		}
	}
	return false
}

// SYNC-028
func TestCorruptStateReadsEmpty(t *testing.T) {
	f := newFixture(t)
	must(t, os.MkdirAll(filepath.Dir(f.state.Path), 0o700))
	must(t, os.WriteFile(f.state.Path, []byte("[1,2"), 0o600))
	if len(f.state.Load()) != 0 {
		t.Fatal("corrupt state not empty")
	}
}

// SYNC-029
func TestBackupAndRestoreKeepsCurrent(t *testing.T) {
	f := newFixture(t)
	f.put(a1, c1, entry(c1, 100, map[string]any{"title": "before"}), "")
	dirs := map[string]string{"default": filepath.Join(f.appData, "claude-code-sessions")}
	id, err := f.backups.Create(dirs, "test")
	must(t, err)
	st, _ := os.Stat(filepath.Join(f.backups.Dir, id+".tar.gz"))
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0o600 {
		t.Fatal(st.Mode())
	}
	f.put(a1, c1, entry(c1, 100, map[string]any{"title": "after"}), "")
	got, err := f.backups.Restore(id, dirs)
	must(t, err)
	if len(got) != 1 || f.read(a1, c1, "")["title"] != "before" {
		t.Fatal(got)
	}
	files, _ := os.ReadDir(f.appData)
	kept := 0
	for _, e := range files {
		if strings.HasPrefix(e.Name(), "claude-code-sessions.before-restore-") {
			kept++
		}
	}
	if kept != 1 {
		t.Fatal("current folder not kept")
	}
}

// SYNC-030
func TestUnsafeArchiveRejected(t *testing.T) {
	f := newFixture(t)
	must(t, os.MkdirAll(f.backups.Dir, 0o700))
	out, _ := os.Create(filepath.Join(f.backups.Dir, "20990101-000000-evil.tar.gz"))
	gz := gzip.NewWriter(out)
	tw := tar.NewWriter(gz)
	must(t, tw.WriteHeader(&tar.Header{Name: "default/../../escape.txt", Size: 1, Mode: 0o600, Typeflag: tar.TypeReg}))
	tw.Write([]byte("x"))
	tw.Close()
	gz.Close()
	out.Close()
	if _, err := f.backups.Restore("20990101-000000-evil", map[string]string{"default": filepath.Join(f.appData, "claude-code-sessions")}); err == nil {
		t.Fatal("unsafe archive accepted")
	}
	if _, err := os.Stat(filepath.Join(f.root, "escape.txt")); err == nil {
		t.Fatal("escaped")
	}
}

// SYNC-031
func TestKeepsLast20Backups(t *testing.T) {
	f := newFixture(t)
	f.put(a1, c1, entry(c1, 1, nil), "")
	dirs := map[string]string{"default": filepath.Join(f.appData, "claude-code-sessions")}
	for i := 0; i < 23; i++ {
		_, err := f.backups.Create(dirs, "n")
		must(t, err)
	}
	if n := len(f.backups.List()); n != 20 {
		t.Fatal(n)
	}
}
