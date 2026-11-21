package fsstore

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// KeepBackups is how many backups are kept; older ones are removed.
const KeepBackups = 20

var (
	backupIDRe = regexp.MustCompile(`^[0-9]{8}-[0-9]{6}(-[a-z0-9-]{1,40})?$`)
	noteRe     = regexp.MustCompile(`[^a-z0-9-]+`)
)

// Backups stores gzipped tar archives of claude-code-sessions folders.
// Members are "<label>/claude-code-sessions/...". Archives are 0600 in a
// 0700 folder.
type Backups struct{ Dir string }

func (b Backups) path(id string) string { return filepath.Join(b.Dir, id+".tar.gz") }

// Create archives the given folders (label -> claude-code-sessions folder).
func (b Backups) Create(dirs map[string]string, note string) (string, error) {
	if err := os.MkdirAll(b.Dir, dirPerm); err != nil {
		return "", err
	}
	_ = os.Chmod(b.Dir, dirPerm)
	id := time.Now().Format("20060102-150405")
	if n := strings.Trim(noteRe.ReplaceAllString(strings.ToLower(note), "-"), "-"); n != "" {
		id += "-" + n
	}
	base := id
	for i := 2; ; i++ {
		if _, err := os.Stat(b.path(id)); errors.Is(err, fs.ErrNotExist) {
			break
		}
		id = fmt.Sprintf("%s-%d", base, i)
	}
	tmp := b.path(id) + ".part"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
	if err != nil {
		return "", err
	}
	ok := false
	defer func() {
		if !ok {
			os.Remove(tmp)
		}
	}()
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	labels := make([]string, 0, len(dirs))
	for l := range dirs {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	for _, label := range labels {
		if err := addTree(tw, dirs[label], label+"/"+SessionsDir); err != nil {
			f.Close()
			return "", err
		}
	}
	if err := tw.Close(); err != nil {
		f.Close()
		return "", err
	}
	if err := gz.Close(); err != nil {
		f.Close()
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, b.path(id)); err != nil {
		return "", err
	}
	ok = true
	b.prune()
	return id, nil
}

func addTree(tw *tar.Writer, root, prefix string) error {
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		name := prefix
		if rel != "." {
			name = prefix + "/" + filepath.ToSlash(rel)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: name + "/", Mode: 0o700, ModTime: info.ModTime()})
		case info.Mode().IsRegular():
			if strings.HasPrefix(d.Name(), tempPrefix) || strings.HasPrefix(d.Name(), oldTempPrefix) {
				return nil
			}
			if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: name, Mode: 0o600, Size: info.Size(), ModTime: info.ModTime()}); err != nil {
				return err
			}
			f, err := os.Open(p)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.CopyN(tw, f, info.Size())
			return err
		default:
			return nil // symlinks and special files are not backed up
		}
	})
}

func (b Backups) prune() {
	for _, old := range b.List()[min(len(b.List()), KeepBackups):] {
		os.Remove(b.path(old))
	}
}

// List returns backup ids, newest first.
func (b Backups) List() []string {
	files, err := os.ReadDir(b.Dir)
	if err != nil {
		return nil
	}
	var ids []string
	for _, f := range files {
		if id, ok := strings.CutSuffix(f.Name(), ".tar.gz"); ok && backupIDRe.MatchString(id) {
			ids = append(ids, id)
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(ids)))
	return ids
}

// Restore puts back the folders of a backup whose label is in dirs. Each
// current folder is renamed to "claude-code-sessions.before-restore-<time>",
// never deleted. Archives with unexpected members are rejected whole.
func (b Backups) Restore(id string, dirs map[string]string) ([]string, error) {
	if !backupIDRe.MatchString(id) {
		return nil, errors.New("no such backup")
	}
	f, err := os.Open(b.path(id))
	if err != nil {
		return nil, errors.New("no such backup")
	}
	defer f.Close()
	work, err := os.MkdirTemp(b.Dir, ".restore-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(work)
	if err := extract(f, work, dirs); err != nil {
		return nil, err
	}
	stamp := time.Now().Format("20060102-150405")
	var restored []string
	entries, _ := os.ReadDir(work)
	for _, e := range entries {
		label := e.Name()
		dst := dirs[label]
		if _, err := os.Stat(dst); err == nil {
			if err := os.Rename(dst, dst+".before-restore-"+stamp); err != nil {
				return restored, err
			}
		}
		if err := os.Rename(filepath.Join(work, label, SessionsDir), dst); err != nil {
			return restored, err
		}
		restored = append(restored, label)
	}
	sort.Strings(restored)
	return restored, nil
}

const maxRestoreBytes = 2 << 30

func extract(r io.Reader, work string, dirs map[string]string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	var total int64
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		clean := path.Clean(h.Name)
		parts := strings.Split(clean, "/")
		if path.IsAbs(h.Name) || strings.Contains(h.Name, "..") || len(parts) < 2 ||
			dirs[parts[0]] == "" || parts[1] != SessionsDir ||
			(h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeDir) {
			return fmt.Errorf("backup has an unexpected member: %q", h.Name)
		}
		dst := filepath.Join(work, filepath.FromSlash(clean))
		if h.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(dst, dirPerm); err != nil {
				return err
			}
			continue
		}
		total += h.Size
		if total > maxRestoreBytes {
			return errors.New("backup is too large")
		}
		if err := os.MkdirAll(filepath.Dir(dst), dirPerm); err != nil {
			return err
		}
		out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, filePerm)
		if err != nil {
			return err
		}
		if _, err := io.CopyN(out, tr, h.Size); err != nil {
			out.Close()
			return err
		}
		if err := out.Close(); err != nil {
			return err
		}
	}
}
