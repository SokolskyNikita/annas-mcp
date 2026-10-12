package mirror

import (
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	// staleAfter drops saved states nothing was learned about for this long:
	// their holds have ended and their successes are too old to trust.
	staleAfter = freshFor
	// saveEvery is how often successes alone rewrite the file.
	saveEvery = time.Minute

	maxStateFileBytes = 64 << 10
)

type stateFile struct {
	Mirrors map[string]HostState `json:"mirrors"`
}

// Store shares Health with other processes, such as CLI runs and other MCP
// clients, through a small JSON file, so a new process can use a mirror that
// another one confirmed instead of checking, and skip one another held. The
// file is advisory: when it is missing, unreadable or stale, selection works
// as it would without it.
type Store struct {
	log    *slog.Logger
	path   string
	health *Health

	mu     sync.Mutex
	saved  map[string]HostState
	warned bool
}

// NewStore shares health through the file at path. Health reads it on first
// use, not here.
func NewStore(path string, health *Health, logs ...*slog.Logger) *Store {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if len(logs) > 0 && logs[0] != nil {
		log = logs[0]
	}
	s := &Store{path: path, health: health, saved: make(map[string]HostState), log: log}
	health.store = s
	return s
}

func (s *Store) load() {
	states := s.read()
	s.health.merge(states)
	s.mu.Lock()
	defer s.mu.Unlock()
	maps.Copy(s.saved, states)
}

// changed saves the states when a mirror goes on or off hold, and at most
// once a minute when only successes changed.
func (s *Store) changed(host string, state HostState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	saved := s.saved[host]
	if state.HeldUntil.Equal(saved.HeldUntil) && state.LastOK.Sub(saved.LastOK) < saveEvery {
		return
	}
	// Another process may have learned something newer since this one
	// read the file; the newer state of each mirror wins.
	ours := s.health.snapshot()
	merged := s.read()
	for host, state := range ours {
		if latest(state).After(latest(merged[host])) {
			merged[host] = state
		}
	}
	if err := writeStateFile(s.path, stateFile{Mirrors: merged}); err != nil {
		if !s.warned {
			s.warned = true
			s.log.Warn("Could not save mirror health; other runs will check the mirrors themselves", "error", err)
		}
		return
	}
	maps.Copy(s.saved, ours)
}

// read returns the saved states that still apply. Entries from the future
// are dropped and holds longer than maxHold are cut short, so a damaged file
// cannot keep a mirror out of use.
func (s *Store) read() map[string]HostState {
	states := make(map[string]HostState)
	file, err := os.Open(s.path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			s.log.Warn("Could not read mirror health", "error", err)
		}
		return states
	}
	defer file.Close()
	var saved stateFile
	data, err := io.ReadAll(io.LimitReader(file, maxStateFileBytes))
	if err != nil || json.Unmarshal(data, &saved) != nil {
		return states
	}
	now := s.health.now()
	for host, state := range saved.Mirrors {
		last := latest(state)
		if last.Before(now.Add(-staleAfter)) || state.LastOK.After(now.Add(time.Minute)) || state.LastFail.After(now.Add(time.Minute)) {
			continue
		}
		if limit := state.LastFail.Add(maxHold); state.HeldUntil.After(limit) {
			state.HeldUntil = limit
		}
		states[host] = state
	}
	return states
}

// writeStateFile replaces the file in one rename, so processes running side
// by side never read half of it.
func writeStateFile(path string, saved stateFile) error {
	data, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".mirrors-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(temp.Name(), path)
}
