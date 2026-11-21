package domain

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

const (
	org  = "00000000-0000-4000-8000-0000000000aa"
	org2 = "00000000-0000-4000-8000-0000000000bb"
	a1   = "11111111-1111-4111-8111-111111111111"
	a2   = "22222222-2222-4222-8222-222222222222"
	c1   = "aaaaaaaa-0000-4000-8000-000000000001"
	c2   = "aaaaaaaa-0000-4000-8000-000000000002"
)

type ent struct {
	id, cli string
	ts      int64
}

func snap(account, o, dataDir string, entries []ent, tomb map[string]string) *Snapshot {
	s := NewSnapshot(StoreID{DataDir: dataDir, Account: account, Org: o})
	for _, e := range entries {
		s.Entries[e.id] = EntryInfo{ChatID: e.id, CLISessionID: e.cli, LastActivityMs: e.ts, Title: "t" + e.id[len(e.id)-1:]}
	}
	for k, v := range tomb {
		s.Tombstones[k] = v
	}
	return s
}

func kinds(acts []Action) []string {
	var out []string
	for _, a := range acts {
		tgt := "-"
		if a.Target != nil {
			tgt = a.Target.Account[:1]
		}
		out = append(out, string(a.Kind)+" "+a.ChatID[len(a.ChatID)-1:]+" "+tgt)
	}
	sort.Strings(out)
	return out
}

func set(ids ...string) map[string]bool {
	m := map[string]bool{}
	for _, i := range ids {
		m[i] = true
	}
	return m
}

func eq(t *testing.T, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func hasWarn(acts []Action) bool {
	for _, a := range acts {
		if a.Kind == ActWarn {
			return true
		}
	}
	return false
}

// SYNC-001
func TestAddsMissingBothWays(t *testing.T) {
	acts := PlanSync([]*Snapshot{
		snap(a1, org, "default", []ent{{c1, "t1", 100}}, nil),
		snap(a2, org, "default", []ent{{c2, "t2", 100}}, nil),
	}, set("t1", "t2"), true, nil)
	eq(t, kinds(acts), []string{"add 1 2", "add 2 1"})
}

// SYNC-002
func TestInSyncNoActions(t *testing.T) {
	acts := PlanSync([]*Snapshot{
		snap(a1, org, "default", []ent{{c1, "t1", 100}}, nil),
		snap(a2, org, "default", []ent{{c1, "t1", 100}}, nil),
	}, set("t1"), true, nil)
	if len(acts) != 0 {
		t.Fatal(acts)
	}
}

// SYNC-003
func TestTombstoneBlocksAndSpreadsToLiveCopy(t *testing.T) {
	acts := PlanSync([]*Snapshot{
		snap(a1, org, "default", nil, map[string]string{c1: "1700000000000"}),
		snap(a2, org, "default", []ent{{c1, "t1", 100}}, nil),
		snap(a2, org, "profile-x", nil, nil),
	}, set("t1"), true, nil)
	eq(t, kinds(acts), []string{"tombstone 1 2"})
	if acts[0].TombstoneValue != "1700000000000" || acts[0].Target.DataDir != "default" {
		t.Fatal(acts[0])
	}
}

// SYNC-004
func TestOldTombstoneWithoutLiveCopyNotCopied(t *testing.T) {
	acts := PlanSync([]*Snapshot{
		snap(a1, org, "default", nil, map[string]string{c1: "1"}),
		snap(a2, org, "default", nil, nil),
	}, set(), true, nil)
	if len(acts) != 0 {
		t.Fatal(acts)
	}
}

// SYNC-005
func TestNewerUpdatesOlderOnlyWhenClosed(t *testing.T) {
	s := []*Snapshot{
		snap(a1, org, "default", []ent{{c1, "t1", 100}}, nil),
		snap(a2, org, "default", []ent{{c1, "t1", 200}}, nil),
	}
	eq(t, kinds(PlanSync(s, set("t1"), true, nil)), []string{"skip 1 1"})
	closed := PlanSync(s, set("t1"), false, nil)
	eq(t, kinds(closed), []string{"update 1 1"})
	if closed[0].Source.Account != a2 {
		t.Fatal("wrong source")
	}
}

// SYNC-006
func TestMissingTranscriptSkipped(t *testing.T) {
	acts := PlanSync([]*Snapshot{
		snap(a1, org, "default", []ent{{c1, "gone", 100}}, nil),
		snap(a2, org, "default", nil, nil),
	}, set("other"), true, nil)
	if len(acts) != 1 || acts[0].Kind != ActSkip || acts[0].Reason != "transcript not found" {
		t.Fatal(acts)
	}
}

// SYNC-007
func TestTranscriptCheckOffWhenUnknown(t *testing.T) {
	acts := PlanSync([]*Snapshot{
		snap(a1, org, "default", []ent{{c1, "t1", 1}}, nil),
		snap(a2, org, "default", nil, nil),
	}, nil, true, nil)
	eq(t, kinds(acts), []string{"add 1 2"})
}

// SYNC-008
func TestOrgsNeverMix(t *testing.T) {
	acts := PlanSync([]*Snapshot{
		snap(a1, org, "default", []ent{{c1, "t1", 100}}, nil),
		snap(a2, org2, "default", nil, nil),
	}, set("t1"), true, nil)
	if len(acts) != 0 {
		t.Fatal(acts)
	}
}

// SYNC-009
func TestProfileStoreIncluded(t *testing.T) {
	acts := PlanSync([]*Snapshot{
		snap(a1, org, "default", []ent{{c1, "t1", 100}}, nil),
		snap(a2, org, "profile-work", nil, nil),
	}, set("t1"), true, nil)
	if acts[0].Target.DataDir != "profile-work" {
		t.Fatal(acts)
	}
}

// SYNC-010
func TestBothSeatsChangedWarns(t *testing.T) {
	s1 := snap(a1, org, "default", []ent{{c1, "t1", 150}}, nil)
	s2 := snap(a2, org, "default", []ent{{c1, "t1", 160}}, nil)
	synced := map[string]int64{StateKey(s1.Store, c1): 100, StateKey(s2.Store, c1): 100}
	if !hasWarn(PlanSync([]*Snapshot{s1, s2}, set("t1"), true, synced)) {
		t.Fatal("expected warning")
	}
}

// SYNC-011
func TestNeverDeletes(t *testing.T) {
	acts := PlanSync([]*Snapshot{
		snap(a1, org, "default", []ent{{c1, "t1", 1}}, map[string]string{c2: "5"}),
		snap(a2, org, "default", []ent{{c2, "t2", 9}}, nil),
	}, set("t1", "t2"), false, nil)
	eq(t, kinds(acts), []string{"add 1 2", "tombstone 2 2"})
}

// SYNC-012
func TestOneSeatChangedNoWarn(t *testing.T) {
	s1 := snap(a1, org, "default", []ent{{c1, "t1", 150}}, nil)
	s2 := snap(a2, org, "default", []ent{{c1, "t1", 100}}, nil)
	synced := map[string]int64{StateKey(s1.Store, c1): 100, StateKey(s2.Store, c1): 100}
	if hasWarn(PlanSync([]*Snapshot{s1, s2}, set("t1"), true, synced)) {
		t.Fatal("unexpected warning")
	}
}

// SYNC-013
func TestNextSyncedHoldsWarnedUntilAgree(t *testing.T) {
	s1 := snap(a1, org, "default", []ent{{c1, "t1", 150}, {c2, "t2", 7}}, nil)
	s2 := snap(a2, org, "default", []ent{{c1, "t1", 160}, {c2, "t2", 7}}, nil)
	prev := map[string]int64{StateKey(s1.Store, c1): 100, StateKey(s2.Store, c1): 100}
	out := NextSynced([]*Snapshot{s1, s2}, prev, map[string]bool{c1: true})
	if out[StateKey(s1.Store, c1)] != 100 || out[StateKey(s2.Store, c2)] != 7 {
		t.Fatal(out)
	}
	s2.Entries[c1] = EntryInfo{ChatID: c1, CLISessionID: "t1", LastActivityMs: 150}
	out = NextSynced([]*Snapshot{s1, s2}, prev, map[string]bool{c1: true})
	if out[StateKey(s2.Store, c1)] != 150 {
		t.Fatal(out)
	}
}

// SYNC-034
func TestStaleCopiesElsewhereNoWarn(t *testing.T) {
	s1 := snap(a1, org, "default", []ent{{c1, "t1", 300}}, nil)
	s2 := snap(a2, org, "default", []ent{{c1, "t1", 100}}, nil)
	s3 := snap(a2, org, "profile-x", []ent{{c1, "t1", 200}}, nil)
	synced := map[string]int64{StateKey(s1.Store, c1): 200, StateKey(s2.Store, c1): 100, StateKey(s3.Store, c1): 200}
	if hasWarn(PlanSync([]*Snapshot{s1, s2, s3}, set("t1"), true, synced)) {
		t.Fatal("unexpected warning")
	}
}

func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	var m map[string]any
	if err := d.Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

// SYNC-014
func TestValidateEntry(t *testing.T) {
	good := `{"sessionId":"local_` + c1 + `","cliSessionId":"x","cwd":"/p","lastActivityAt":1794479906673,"title":"T"}`
	info, ok := ValidateEntry(c1, decode(t, good))
	if !ok || info.LastActivityMs != 1794479906673 {
		t.Fatal(info, ok)
	}
	if _, ok := ValidateEntry(c2, decode(t, good)); ok {
		t.Fatal("id mismatch accepted")
	}
	if _, ok := ValidateEntry(c1, decode(t, `{"sessionId":"local_`+c1+`"}`)); ok {
		t.Fatal("missing fields accepted")
	}
	bad := `{"sessionId":"local_` + c1 + `","cliSessionId":"x","cwd":"/p","lastActivityAt":true}`
	if info, ok := ValidateEntry(c1, decode(t, bad)); !ok || info.LastActivityMs != 0 {
		t.Fatal(info)
	}
}

// SYNC-015
func TestValidTombstone(t *testing.T) {
	if !ValidTombstone("1794479906673") || ValidTombstone("12a") || ValidTombstone("") {
		t.Fatal("tombstone rules")
	}
}

// SYNC-016
func TestMakeCopyResetsAccountFieldsOnly(t *testing.T) {
	raw := map[string]any{"sessionId": "s", "bridgeSessionIds": []any{"b"}, "remoteControlUserEnabled": true, "title": "T"}
	out := MakeCopy(raw)
	if len(out["bridgeSessionIds"].([]any)) != 0 || out["remoteControlUserEnabled"] != false {
		t.Fatal(out)
	}
	if _, ok := out["steeredByRemoteClient"]; ok {
		t.Fatal("added a field")
	}
	if len(raw["bridgeSessionIds"].([]any)) != 1 {
		t.Fatal("source changed")
	}
}

// SYNC-017
func TestProfileNames(t *testing.T) {
	for _, ok := range []string{"Work", "Seat 2", "Ünïcødé", "a"} {
		if _, valid := NormaliseProfileName(ok); !valid {
			t.Fatal(ok)
		}
	}
	for _, bad := range []string{"", "   ", "\x1b\x07", strings.Repeat("x", 41)} {
		if _, valid := NormaliseProfileName(bad); valid {
			t.Fatalf("%q accepted", bad)
		}
	}
	if n, _ := NormaliseProfileName("  W\x1bork‮ "); n != "Work" {
		t.Fatalf("%q", n)
	}
}

// SYNC-018
func TestCleanText(t *testing.T) {
	if got := CleanText("a\x1b[31mb‮c", 0); got != "a[31mbc" {
		t.Fatal(got)
	}
	if got := CleanText(strings.Repeat("x", 100), 10); len([]rune(got)) != 10 {
		t.Fatal(got)
	}
}
