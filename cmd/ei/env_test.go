// .env loading: KEY=value lines load, blank/# lines skip, optional quotes
// strip, a value keeps everything after the first '=' (a '#' inside a token
// is data, never a comment), existing process env is never overridden, and
// a missing file is not an error.
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := `# comment
EIDOTENV_A=1
EIDOTENV_SPACE=two words
EIDOTENV_QUOTED="quoted value"
EIDOTENV_SINGLE='sq'
EIDOTENV_HASH=keep#data

EIDOTENV_KEEP=file
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EIDOTENV_KEEP", "process") // the shell always wins

	if err := loadDotEnv(path); err != nil {
		t.Fatalf("loadDotEnv: %v", err)
	}
	want := map[string]string{
		"EIDOTENV_A":      "1",
		"EIDOTENV_SPACE":  "two words",
		"EIDOTENV_QUOTED": "quoted value",
		"EIDOTENV_SINGLE": "sq",
		"EIDOTENV_HASH":   "keep#data",
		"EIDOTENV_KEEP":   "process", // pre-existing, never overridden
	}
	for k, v := range want {
		if got := os.Getenv(k); got != v {
			t.Errorf("env %s = %q, want %q", k, got, v)
		}
		t.Cleanup(func() { os.Unsetenv(k) })
	}
}

func TestLoadDotEnvMissingFile(t *testing.T) {
	if err := loadDotEnv(filepath.Join(t.TempDir(), "none.env")); err != nil {
		t.Errorf("missing .env should not be an error, got %v", err)
	}
}
