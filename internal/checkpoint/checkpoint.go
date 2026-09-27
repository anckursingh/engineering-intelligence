// Package checkpoint persists connector sync cursors as a JSON file.
// Checkpoints are operational state, not engineering knowledge (ARCHITECTURE.md §4).
package checkpoint

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Checkpoint struct {
	Version int              `json:"version"`
	Github  CheckpointGithub `json:"github"`
	Jira    CheckpointJira   `json:"jira"`
	Runs    []RunRecord      `json:"runs,omitempty"` // last 5 runs
}

type CheckpointGithub struct {
	UpdatedSince time.Time         `json:"updated_since"` // issues/PRs watermark
	Repos        map[string]string `json:"repos"`         // "owner/name" → default-branch head SHA
}

type CheckpointJira struct {
	UpdatedSince time.Time `json:"updated_since"` // issue search watermark
}

type RunRecord struct {
	StartedAt  time.Time `json:"started_at"`
	EndedAt    time.Time `json:"ended_at"`
	Owner      string    `json:"owner,omitempty"`   // github runs
	Project    string    `json:"project,omitempty"` // jira runs
	NewObjects int       `json:"new_objects"`
}

// Load reads the checkpoint; a missing file is a fresh checkpoint, not an error.
func Load(path string) (*Checkpoint, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Checkpoint{Version: 1, Github: CheckpointGithub{Repos: map[string]string{}}}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("checkpoint: read %s: %w", path, err)
	}
	c := &Checkpoint{}
	if err := json.Unmarshal(b, c); err != nil {
		return nil, fmt.Errorf("checkpoint: parse %s: %w", path, err)
	}
	if c.Github.Repos == nil {
		c.Github.Repos = map[string]string{}
	}
	if c.Version == 0 {
		c.Version = 1
	}
	return c, nil
}

// Save writes atomically: tmp file in the same directory, then rename.
func (c *Checkpoint) Save(path string) error {
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("checkpoint: marshal: %w", err)
	}
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("checkpoint: mkdir %s: %w", dir, err)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("checkpoint: write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("checkpoint: rename to %s: %w", path, err)
	}
	return nil
}
