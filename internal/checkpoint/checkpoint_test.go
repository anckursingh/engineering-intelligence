package checkpoint

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingIsFresh(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "none.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Version != 1 {
		t.Errorf("version = %d, want 1", c.Version)
	}
	if !c.Github.UpdatedSince.IsZero() {
		t.Errorf("UpdatedSince = %s, want zero", c.Github.UpdatedSince)
	}
	if c.Github.Repos == nil || len(c.Github.Repos) != 0 {
		t.Errorf("Repos = %v, want empty map", c.Github.Repos)
	}
}

func TestSaveLoadRoundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "checkpoint.json") // nested: Save must MkdirAll

	since := time.Now().UTC().Truncate(time.Second)
	c := &Checkpoint{
		Version: 1,
		Github:  CheckpointGithub{UpdatedSince: since, Repos: map[string]string{"o/r": "abc"}},
		Runs:    []RunRecord{{StartedAt: since, EndedAt: since.Add(time.Minute), Owner: "o", NewObjects: 3}},
	}
	if err := c.Save(path); err != nil {
		t.Fatal(err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Github.UpdatedSince.Equal(since) {
		t.Errorf("UpdatedSince = %s, want %s", got.Github.UpdatedSince, since)
	}
	if got.Github.Repos["o/r"] != "abc" {
		t.Errorf("Repos = %v", got.Github.Repos)
	}
	if len(got.Runs) != 1 || got.Runs[0].NewObjects != 3 {
		t.Errorf("Runs = %+v", got.Runs)
	}

	// atomic write: no .tmp left behind
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf(".tmp file left behind: %v", err)
	}
}

// TestLoadCorruptFailsLoudly pins the corruption contract (§34): a corrupt
// checkpoint errors — it is never silently reset, because a zero watermark
// would re-ingest history AND clobber the corrupt file. Both connectors
// (github, jira) share this path.
func TestLoadCorruptFailsLoudly(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load of a corrupt checkpoint must error")
	}
}
