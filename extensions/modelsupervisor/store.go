package modelsupervisor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// stateSchema is the on-disk format version. A file written by a newer
// supervisor is refused rather than guessed at.
const stateSchema = 1

// State is the persisted intent: which instances should exist. It deliberately
// holds no PIDs, timestamps or log content, so a restart never resurrects stale
// runtime values as if they were facts.
type State struct {
	Schema    int    `json:"schema"`
	Instances []Spec `json:"instances"`
}

// Store persists State as a JSON snapshot. A JSON snapshot is used instead of a
// database because the payload is a handful of rows with no concurrent readers
// and no migration history worth carrying.
type Store struct {
	path string
}

// NewStore returns a store bound to a file path. The file is created on first
// Save; Load on a missing file yields an empty state.
func NewStore(path string) *Store { return &Store{path: path} }

// Path reports the backing file, for diagnostics.
func (s *Store) Path() string { return s.path }

// Load reads the snapshot. A missing file is not an error: it means nothing has
// been persisted yet. A corrupt or future-version file is an error and must be
// left untouched so the operator can inspect it.
func (s *Store) Load() (State, error) {
	if s.path == "" {
		return State{}, errors.New("state path required")
	}
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return State{Schema: stateSchema}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("read state: %w", err)
	}
	var state State
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return State{}, fmt.Errorf("decode state: %w", err)
	}
	if state.Schema != stateSchema {
		return State{}, fmt.Errorf("unsupported state schema %d", state.Schema)
	}
	seenID := make(map[string]bool, len(state.Instances))
	seenPort := make(map[int]bool, len(state.Instances))
	for _, spec := range state.Instances {
		if err := spec.Validate(); err != nil {
			return State{}, fmt.Errorf("invalid persisted instance %q: %w", spec.ID, err)
		}
		if seenID[spec.ID] {
			return State{}, fmt.Errorf("duplicate persisted instance %q", spec.ID)
		}
		if seenPort[spec.Port] {
			return State{}, fmt.Errorf("duplicate persisted port %d", spec.Port)
		}
		seenID[spec.ID], seenPort[spec.Port] = true, true
	}
	return state, nil
}

// Save writes the snapshot atomically: a temporary file in the same directory is
// renamed over the target, so a crash mid-write cannot leave a truncated state.
func (s *Store) Save(state State) error {
	if s.path == "" {
		return errors.New("state path required")
	}
	state.Schema = stateSchema
	sort.Slice(state.Instances, func(i, j int) bool { return state.Instances[i].ID < state.Instances[j].ID })
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create state temporary: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("write state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close state: %w", err)
	}
	if err := os.Rename(tmpName, s.path); err != nil {
		return fmt.Errorf("replace state: %w", err)
	}
	return nil
}
