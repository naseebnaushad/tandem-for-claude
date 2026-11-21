// Package app holds the use cases. It depends only on domain and ports, so
// the window, tray and command line are thin front-ends over it.
package app

import (
	"github.com/naseebnaushad/tango-for-claude/internal/domain"
	"github.com/naseebnaushad/tango-for-claude/internal/ports"
)

// SyncResult reports what a sync planned, did and couldn't do.
type SyncResult struct {
	Snapshots  []*domain.Snapshot
	Actions    []domain.Action
	Problems   []string
	AppRunning bool
	// Paused is true when the format guard stopped writes (CR-0.1.0-02 D15).
	Paused    bool
	Applied   []domain.Action
	Unchanged []domain.Action // target appeared meanwhile, or Claude started
	Failed    map[string]string
	BackupID  string
}

// Syncer runs the sync use case.
type Syncer struct {
	Store       ports.SessionStore
	Transcripts ports.TranscriptIndex
	Probe       ports.ProcessProbe
	Backups     ports.Backups
	State       ports.SyncState
}

func (s Syncer) claudeRunning() bool {
	dirs, ok := s.Probe.Running()
	return !ok || len(dirs) > 0 // unsure counts as running: add-only is the safe mode
}

// Sync plans and, with apply, carries out the plan after a backup. Errors on
// one chat are recorded and the rest continue; nothing is ever deleted. If any
// chat list file has an unexpected format, writes are paused: the app may
// have changed its format and guessing could damage it.
func (s Syncer) Sync(apply bool) (*SyncResult, error) {
	running := s.claudeRunning()
	snaps, problems, err := s.Store.Snapshots()
	if err != nil {
		return nil, err
	}
	formatProblems := len(problems) > 0
	ids := s.Transcripts.IDs()
	if ids == nil {
		problems = append(problems, "transcript folder not readable; transcript check skipped")
	}
	previous := s.State.Load()
	res := &SyncResult{
		Snapshots:  snaps,
		Actions:    domain.PlanSync(snaps, ids, running, previous),
		Problems:   problems,
		AppRunning: running,
		Failed:     map[string]string{},
		Paused:     formatProblems,
	}
	if !apply || res.Paused {
		return res, nil
	}

	var writes []domain.Action
	for _, a := range res.Actions {
		if a.Writes() {
			writes = append(writes, a)
		}
	}
	if len(writes) > 0 {
		id, err := s.Backups.Create(s.Store.SessionDirs(), "sync")
		if err != nil {
			return res, err // no backup, no writes
		}
		res.BackupID = id
	}
	for _, a := range writes {
		// Claude may have started since planning; never race it on an update.
		if a.Kind == domain.ActUpdate && s.claudeRunning() {
			res.Unchanged = append(res.Unchanged, a)
			continue
		}
		var done bool
		var err error
		if a.Kind == domain.ActTombstone {
			done, err = s.Store.WriteTombstone(*a.Target, a.ChatID, a.TombstoneValue)
		} else {
			var raw map[string]any
			raw, err = s.Store.ReadEntry(*a.Source, a.ChatID)
			if err == nil {
				done, err = s.Store.WriteEntry(*a.Target, a.ChatID, domain.MakeCopy(raw), a.Kind == domain.ActUpdate)
			}
		}
		switch {
		case err != nil:
			res.Failed[string(a.Kind)+" "+a.ChatID+" -> "+a.Target.Label()] = "could not write"
		case done:
			res.Applied = append(res.Applied, a)
		default:
			res.Unchanged = append(res.Unchanged, a)
		}
	}

	after := snaps
	if len(writes) > 0 {
		if again, _, err := s.Store.Snapshots(); err == nil {
			after = again
		}
	}
	warned := map[string]bool{}
	for _, a := range res.Actions {
		if a.Kind == domain.ActWarn {
			warned[a.ChatID] = true
		}
	}
	if err := s.State.Save(domain.NextSynced(after, previous, warned)); err != nil {
		res.Problems = append(res.Problems, "could not save sync state")
	}
	return res, nil
}
