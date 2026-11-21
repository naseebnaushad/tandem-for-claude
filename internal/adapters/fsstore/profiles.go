package fsstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/naseebnaushad/tango-for-claude/internal/domain"
)

const maxSmallFile = 1 << 20

// ProfileFile stores profiles in a JSON file (0600). Entries with an invalid
// id, name or kind are dropped on load rather than trusted.
type ProfileFile struct{ Path string }

// Load returns the saved profiles; a missing file is an empty list.
func (f ProfileFile) Load() ([]domain.Profile, error) {
	raw, err := ReadCapped(f.Path, maxSmallFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []domain.Profile
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, errors.New("profiles file is damaged")
	}
	var out []domain.Profile
	for _, p := range list {
		name, ok := domain.NormaliseProfileName(p.Name)
		if ok && domain.ProfileIDRe.MatchString(p.ID) && domain.ValidKind(p.Kind) {
			p.Name = name
			out = append(out, p)
		}
	}
	return out, nil
}

// Save writes the profiles atomically.
func (f ProfileFile) Save(list []domain.Profile) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), dirPerm); err != nil {
		return err
	}
	if list == nil {
		list = []domain.Profile{}
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	_, err = WriteAtomic(f.Path, data, true)
	return err
}

// JSONFile loads and saves any small JSON value atomically (0600).
type JSONFile struct{ Path string }

// Load decodes the file into v; a missing or damaged file leaves v unchanged.
func (f JSONFile) Load(v any) {
	if raw, err := ReadCapped(f.Path, maxSmallFile); err == nil {
		_ = json.Unmarshal(raw, v)
	}
}

// Save encodes v into the file.
func (f JSONFile) Save(v any) error {
	if err := os.MkdirAll(filepath.Dir(f.Path), dirPerm); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = WriteAtomic(f.Path, data, true)
	return err
}
