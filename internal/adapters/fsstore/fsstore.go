// Package fsstore implements the file-system ports: Claude desktop chat lists,
// the transcript index, the last-sync state and tar.gz backups.
package fsstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/naseebnaushad/tandem-for-claude/internal/domain"
)

const (
	// SessionsDir is the folder in a Claude data folder that holds the chat lists.
	SessionsDir   = "claude-code-sessions"
	maxEntryBytes = 10 << 20
	maxTombstone  = 64
	maxStateBytes = 5 << 20
	tempPrefix    = ".tandem-"
	filePerm      = 0o600
	dirPerm       = 0o700
)

// ReadCapped reads a regular, non-symlink file of at most limit bytes.
func ReadCapped(path string, limit int64) ([]byte, error) {
	st, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !st.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	if st.Size() > limit {
		return nil, errors.New("file too large")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("file too large")
	}
	return data, nil
}

// WriteAtomic writes via a temp file in the same folder. With overwrite the
// temp file is renamed over the target; without it the temp file is hard-
// linked to the target, which fails if the target exists, so an existing
// file is never replaced even if the app writes it at the same moment.
func WriteAtomic(path string, data []byte, overwrite bool) (bool, error) {
	f, err := os.CreateTemp(filepath.Dir(path), tempPrefix)
	if err != nil {
		return false, err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err := f.Chmod(filePerm); err != nil && !isWindows() {
		f.Close()
		return false, err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return false, err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return false, err
	}
	if err := f.Close(); err != nil {
		return false, err
	}
	if overwrite {
		return true, os.Rename(tmp, path)
	}
	if err := os.Link(tmp, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// Store reads and writes the chat lists in one or more Claude data folders.
type Store struct {
	dirs map[string]string // label -> Claude data folder
}

// NewStore returns a store over the given data folders (label -> folder).
func NewStore(dirs map[string]string) *Store {
	cp := map[string]string{}
	for k, v := range dirs {
		cp[k] = v
	}
	return &Store{dirs: cp}
}

// SessionDirs maps labels to existing claude-code-sessions folders.
func (s *Store) SessionDirs() map[string]string {
	out := map[string]string{}
	for label, d := range s.dirs {
		p := filepath.Join(d, SessionsDir)
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			out[label] = p
		}
	}
	return out
}

func (s *Store) folder(id domain.StoreID) (string, error) {
	d, ok := s.dirs[id.DataDir]
	if !ok || !domain.UUIDRe.MatchString(id.Account) || !domain.UUIDRe.MatchString(id.Org) {
		return "", errors.New("unknown store")
	}
	return filepath.Join(d, SessionsDir, id.Account, id.Org), nil
}

// Snapshots loads every store under every data folder.
func (s *Store) Snapshots() ([]*domain.Snapshot, []string, error) {
	var snaps []*domain.Snapshot
	var problems []string
	dirs := s.SessionDirs()
	labels := make([]string, 0, len(dirs))
	for l := range dirs {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	for _, label := range labels {
		root := dirs[label]
		accounts, err := os.ReadDir(root)
		if err != nil {
			return nil, nil, err
		}
		for _, acc := range accounts {
			if !acc.IsDir() || !domain.UUIDRe.MatchString(acc.Name()) {
				continue
			}
			orgs, err := os.ReadDir(filepath.Join(root, acc.Name()))
			if err != nil {
				continue
			}
			for _, org := range orgs {
				if !org.IsDir() || !domain.UUIDRe.MatchString(org.Name()) {
					continue
				}
				id := domain.StoreID{DataDir: label, Account: acc.Name(), Org: org.Name()}
				snaps = append(snaps, s.load(id, filepath.Join(root, acc.Name(), org.Name()), &problems))
			}
		}
	}
	return snaps, problems, nil
}

func (s *Store) load(id domain.StoreID, folder string, problems *[]string) *domain.Snapshot {
	snap := domain.NewSnapshot(id)
	files, err := os.ReadDir(folder)
	if err != nil {
		*problems = append(*problems, id.Label()+": folder not readable")
		return snap
	}
	for _, f := range files {
		name := f.Name()
		if m := domain.EntryRe.FindStringSubmatch(name); m != nil {
			raw, err := ReadCapped(filepath.Join(folder, name), maxEntryBytes)
			var info domain.EntryInfo
			ok := false
			if err == nil {
				if entry, derr := decodeEntry(raw); derr == nil {
					info, ok = domain.ValidateEntry(m[1], entry)
				}
			}
			if !ok {
				*problems = append(*problems, fmt.Sprintf("%s: skipped %s (unexpected format or too large)", id.Label(), name))
				continue
			}
			snap.Entries[info.ChatID] = info
			continue
		}
		if m := domain.TombstoneRe.FindStringSubmatch(name); m != nil {
			raw, err := ReadCapped(filepath.Join(folder, name), maxTombstone)
			text := strings.TrimSpace(string(raw))
			if err != nil || !domain.ValidTombstone(text) {
				*problems = append(*problems, fmt.Sprintf("%s: skipped %s (unexpected format)", id.Label(), name))
				continue
			}
			snap.Tombstones[m[1]] = text
		}
	}
	return snap
}

func decodeEntry(raw []byte) (map[string]any, error) {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var m map[string]any
	if err := d.Decode(&m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, errors.New("not an object")
	}
	return m, nil
}

// ReadEntry returns one validated raw entry.
func (s *Store) ReadEntry(id domain.StoreID, chatID string) (map[string]any, error) {
	folder, err := s.folder(id)
	if err != nil {
		return nil, err
	}
	raw, err := ReadCapped(filepath.Join(folder, "local_"+chatID+".json"), maxEntryBytes)
	if err != nil {
		return nil, err
	}
	m, err := decodeEntry(raw)
	if err != nil {
		return nil, err
	}
	if _, ok := domain.ValidateEntry(chatID, m); !ok {
		return nil, errors.New("entry has an unexpected format")
	}
	return m, nil
}

// WriteEntry writes one entry atomically.
func (s *Store) WriteEntry(id domain.StoreID, chatID string, data map[string]any, overwrite bool) (bool, error) {
	if !domain.UUIDRe.MatchString(chatID) {
		return false, errors.New("invalid chat id")
	}
	folder, err := s.folder(id)
	if err != nil {
		return false, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(data); err != nil {
		return false, err
	}
	return WriteAtomic(filepath.Join(folder, "local_"+chatID+".json"), bytes.TrimRight(buf.Bytes(), "\n"), overwrite)
}

// WriteTombstone writes a deleted marker unless one exists.
func (s *Store) WriteTombstone(id domain.StoreID, chatID, value string) (bool, error) {
	if !domain.UUIDRe.MatchString(chatID) || !domain.ValidTombstone(value) {
		return false, errors.New("invalid tombstone")
	}
	folder, err := s.folder(id)
	if err != nil {
		return false, err
	}
	return WriteAtomic(filepath.Join(folder, "deleted_"+chatID), []byte(value), false)
}

// Transcripts indexes <projects>/<project>/<id>.jsonl by file name only.
type Transcripts struct{ Dir string }

// IDs returns the transcript ids, or nil when the folder can't be read.
func (t Transcripts) IDs() map[string]bool {
	projects, err := os.ReadDir(t.Dir)
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, p := range projects {
		if !p.IsDir() {
			continue
		}
		files, err := os.ReadDir(filepath.Join(t.Dir, p.Name()))
		if err != nil {
			continue
		}
		for _, f := range files {
			if id, ok := strings.CutSuffix(f.Name(), ".jsonl"); ok && domain.UUIDRe.MatchString(id) {
				out[id] = true
			}
		}
	}
	return out
}

// stateKeyRe is "<label>|<account>|<chat>"; older formats are ignored.
var stateKeyRe = regexp.MustCompile(`^[a-z0-9-]{1,48}\|[0-9a-f-]{36}\|[0-9a-f-]{36}$`)

// State keeps the last-sync state in a JSON file. A bad file reads as empty.
type State struct{ Path string }

// Load returns the saved state or an empty map.
func (s State) Load() map[string]int64 {
	out := map[string]int64{}
	raw, err := ReadCapped(s.Path, maxStateBytes)
	if err != nil {
		return out
	}
	var m map[string]json.Number
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if d.Decode(&m) != nil {
		return out
	}
	for k, v := range m {
		if n, err := v.Int64(); err == nil && stateKeyRe.MatchString(k) {
			out[k] = n
		}
	}
	return out
}

// Save writes the state atomically.
func (s State) Save(m map[string]int64) error {
	if err := os.MkdirAll(filepath.Dir(s.Path), dirPerm); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = WriteAtomic(s.Path, data, true)
	return err
}
