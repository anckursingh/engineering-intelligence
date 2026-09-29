// .env support: credentials and store config live in an untracked .env file
// (already gitignored). Deliberately minimal — no export prefix, no inline
// comments, no interpolation; the shell env is the source of truth.
package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// loadDotEnv reads KEY=value lines from path and exports each KEY that is
// not already set in the process environment. Blank lines and # comments are
// skipped; a value keeps everything after the first '=' (a '#' inside a
// token is data, never silently truncated); optional surrounding quotes are
// stripped. A missing file is not an error.
func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if len(val) >= 2 && ((val[0] == '"' && val[len(val)-1] == '"') ||
			(val[0] == '\'' && val[len(val)-1] == '\'')) {
			val = val[1 : len(val)-1]
		}
		if _, exists := os.LookupEnv(key); !exists {
			os.Setenv(key, val)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}
