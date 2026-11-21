// Package domain holds the pure rules of Tango for Claude: which chat list
// entries to copy between accounts, and what makes a valid entry, tombstone or
// profile name. It does no I/O, so every rule can be tested with plain values.
package domain

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// AccountTiedFields are reset in copies: they belong to the account that made
// the chat (Remote Control) and must not be reused by another account.
var AccountTiedFields = map[string]any{
	"bridgeSessionIds":          []any{},
	"remoteControlUserEnabled":  false,
	"remoteControlAutoEligible": false,
	"steeredByRemoteClient":     false,
}

const uuidPat = `[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`

var (
	// UUIDRe matches a lowercase UUID, the form the app uses for folders and ids.
	UUIDRe = regexp.MustCompile(`^` + uuidPat + `$`)
	// EntryRe matches a chat list entry file name and captures the chat id.
	EntryRe = regexp.MustCompile(`^local_(` + uuidPat + `)\.json$`)
	// TombstoneRe matches a deleted-chat marker file name and captures the chat id.
	TombstoneRe = regexp.MustCompile(`^deleted_(` + uuidPat + `)$`)
)

// StoreID names one chat list: <data folder>/claude-code-sessions/<account>/<org>.
type StoreID struct {
	DataDir string // label of the Claude data folder, e.g. "default" or "profile-seat2"
	Account string
	Org     string
}

// Label is a short human-readable name such as "default:1a2b3c4d".
func (s StoreID) Label() string {
	acc := s.Account
	if len(acc) > 8 {
		acc = acc[:8]
	}
	return s.DataDir + ":" + acc
}

// EntryInfo is what the planner needs to know about one chat list entry.
type EntryInfo struct {
	ChatID         string
	CLISessionID   string
	LastActivityMs int64
	Title          string
}

// Snapshot is the content of one store.
type Snapshot struct {
	Store      StoreID
	Entries    map[string]EntryInfo // chat id -> entry
	Tombstones map[string]string    // chat id -> marker content
}

// NewSnapshot returns an empty snapshot for a store.
func NewSnapshot(s StoreID) *Snapshot {
	return &Snapshot{Store: s, Entries: map[string]EntryInfo{}, Tombstones: map[string]string{}}
}

// ActionKind says what an Action does.
type ActionKind string

// Action kinds. Only Add, Update and Tombstone write anything.
const (
	ActAdd       ActionKind = "add"
	ActUpdate    ActionKind = "update"
	ActTombstone ActionKind = "tombstone"
	ActSkip      ActionKind = "skip"
	ActWarn      ActionKind = "warn"
)

// Action is one step of a sync plan.
type Action struct {
	Kind           ActionKind
	ChatID         string
	Target         *StoreID
	Source         *StoreID
	TombstoneValue string
	Reason         string
	Title          string
}

// Writes reports whether the action changes files.
func (a Action) Writes() bool {
	return a.Kind == ActAdd || a.Kind == ActUpdate || a.Kind == ActTombstone
}

// StateKey is the key of one chat copy in the last-sync state.
func StateKey(s StoreID, chatID string) string {
	return s.DataDir + "|" + s.Account + "|" + chatID
}

type copyOf struct {
	snap *Snapshot
	info EntryInfo
}

// PlanSync works out which files to write so every store of an organisation
// lists every chat. Rules (CR-0.1.0-01 section 4):
//   - a chat with a tombstone anywhere is never copied, and the tombstone is
//     copied to stores of the same org where the chat is still live;
//   - a live chat missing from a store is added from its newest copy;
//   - an older copy is updated from the newest only when Claude is closed;
//   - a chat whose transcript is missing is skipped;
//   - a warning is added when two copies changed since the last sync;
//   - nothing is ever deleted.
//
// transcripts is the set of transcript ids on disk; nil skips that check.
// synced maps StateKey to each copy's activity time at the last sync.
func PlanSync(snaps []*Snapshot, transcripts map[string]bool, appRunning bool, synced map[string]int64) []Action {
	var actions []Action
	byOrg := map[string][]*Snapshot{}
	for _, s := range snaps {
		byOrg[s.Store.Org] = append(byOrg[s.Store.Org], s)
	}
	for _, org := range sortedKeys(byOrg) {
		group := byOrg[org]
		sort.Slice(group, func(i, j int) bool {
			a, b := group[i].Store, group[j].Store
			if a.DataDir != b.DataDir {
				return a.DataDir < b.DataDir
			}
			return a.Account < b.Account
		})
		if len(group) < 2 {
			continue
		}

		deleted := map[string]string{}
		for _, s := range group {
			for id, v := range s.Tombstones {
				if _, ok := deleted[id]; !ok {
					deleted[id] = v
				}
			}
		}
		// A tombstone matters only where the chat is still live; elsewhere the
		// chat is never copied in anyway.
		for _, id := range sortedKeys(deleted) {
			for _, s := range group {
				_, hasT := s.Tombstones[id]
				_, live := s.Entries[id]
				if !hasT && live {
					t := s.Store
					actions = append(actions, Action{Kind: ActTombstone, ChatID: id, Target: &t, TombstoneValue: deleted[id]})
				}
			}
		}

		copies := map[string][]copyOf{}
		for _, s := range group {
			for id, info := range s.Entries {
				copies[id] = append(copies[id], copyOf{s, info})
			}
		}
		for _, id := range sortedKeys(copies) {
			if _, gone := deleted[id]; gone {
				continue
			}
			found := copies[id]
			newest := found[0]
			for _, c := range found[1:] {
				if newer(c, newest) {
					newest = c
				}
			}
			src := newest.snap.Store
			if transcripts != nil && !transcripts[newest.info.CLISessionID] {
				actions = append(actions, Action{Kind: ActSkip, ChatID: id, Source: &src, Reason: "transcript not found", Title: newest.info.Title})
				continue
			}

			changed := 0
			for _, c := range found {
				if before, ok := synced[StateKey(c.snap.Store, id)]; ok && before != c.info.LastActivityMs {
					changed++
				}
			}
			if changed > 1 {
				actions = append(actions, Action{Kind: ActWarn, ChatID: id, Reason: "used on more than one seat since the last sync", Title: newest.info.Title})
			}

			present := map[StoreID]EntryInfo{}
			for _, c := range found {
				present[c.snap.Store] = c.info
			}
			for _, s := range group {
				t := s.Store
				have, ok := present[t]
				switch {
				case !ok:
					actions = append(actions, Action{Kind: ActAdd, ChatID: id, Target: &t, Source: &src, Title: newest.info.Title})
				case have.LastActivityMs < newest.info.LastActivityMs && appRunning:
					actions = append(actions, Action{Kind: ActSkip, ChatID: id, Target: &t, Source: &src, Reason: "older copy; updated when Claude is closed", Title: newest.info.Title})
				case have.LastActivityMs < newest.info.LastActivityMs:
					actions = append(actions, Action{Kind: ActUpdate, ChatID: id, Target: &t, Source: &src, Title: newest.info.Title})
				}
			}
		}
	}
	return actions
}

func newer(a, b copyOf) bool {
	if a.info.LastActivityMs != b.info.LastActivityMs {
		return a.info.LastActivityMs > b.info.LastActivityMs
	}
	if a.snap.Store.DataDir != b.snap.Store.DataDir {
		return a.snap.Store.DataDir > b.snap.Store.DataDir
	}
	return a.snap.Store.Account > b.snap.Store.Account
}

// NextSynced records each copy's activity time at the end of a sync. A warned
// chat whose copies still differ keeps its earlier times, so the warning stays
// until the copies agree again. Copies seen for the first time are recorded.
func NextSynced(snaps []*Snapshot, previous map[string]int64, warned map[string]bool) map[string]int64 {
	type key struct{ org, chat string }
	times := map[key]map[int64]bool{}
	for _, s := range snaps {
		for id, info := range s.Entries {
			k := key{s.Store.Org, id}
			if times[k] == nil {
				times[k] = map[int64]bool{}
			}
			times[k][info.LastActivityMs] = true
		}
	}
	out := map[string]int64{}
	for _, s := range snaps {
		for id, info := range s.Entries {
			k := StateKey(s.Store, id)
			prev, had := previous[k]
			if warned[id] && len(times[key{s.Store.Org, id}]) > 1 && had {
				out[k] = prev
			} else {
				out[k] = info.LastActivityMs
			}
		}
	}
	return out
}

// MakeCopy returns a shallow copy of a raw entry with the account-tied fields reset.
func MakeCopy(entry map[string]any) map[string]any {
	out := make(map[string]any, len(entry))
	for k, v := range entry {
		out[k] = v
	}
	for k, v := range AccountTiedFields {
		if _, ok := out[k]; ok {
			if _, isList := v.([]any); isList {
				out[k] = []any{}
			} else {
				out[k] = v
			}
		}
	}
	return out
}

// ValidateEntry checks a decoded entry (decoded with UseNumber) and returns
// its info, or false when its shape is not what the app writes (CR-01 D5).
func ValidateEntry(chatID string, entry map[string]any) (EntryInfo, bool) {
	sid, ok1 := entry["sessionId"].(string)
	cli, ok2 := entry["cliSessionId"].(string)
	_, ok3 := entry["cwd"].(string)
	if !ok1 || !ok2 || !ok3 || sid != "local_"+chatID {
		return EntryInfo{}, false
	}
	last := intField(entry, "lastActivityAt")
	if last == 0 {
		last = intField(entry, "createdAt")
	}
	title, _ := entry["title"].(string)
	return EntryInfo{ChatID: chatID, CLISessionID: cli, LastActivityMs: last, Title: title}, true
}

func intField(m map[string]any, k string) int64 {
	n, ok := m[k].(json.Number)
	if !ok {
		return 0
	}
	v, err := n.Int64()
	if err != nil {
		return 0
	}
	return v
}

// ValidTombstone reports whether a marker holds a millisecond timestamp only.
func ValidTombstone(s string) bool {
	if s == "" || len(s) > 20 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// CleanText removes control and bidi-override characters and caps the length
// (in characters) of text shown to the user.
func CleanText(s string, limit int) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsControl(r) || (r >= 0x202a && r <= 0x202e) || (r >= 0x2066 && r <= 0x2069) {
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	if limit > 0 && utf8.RuneCountInString(out) > limit {
		rs := []rune(out)
		out = string(rs[:limit-1]) + "…"
	}
	return out
}

// MaxProfileName is the longest allowed profile display name, in characters.
const MaxProfileName = 40

// NormaliseProfileName trims and cleans a display name and reports whether it
// is acceptable: 1-40 characters after removing control characters.
func NormaliseProfileName(name string) (string, bool) {
	clean := strings.TrimSpace(CleanText(name, 0))
	n := utf8.RuneCountInString(clean)
	return clean, n >= 1 && n <= MaxProfileName
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
