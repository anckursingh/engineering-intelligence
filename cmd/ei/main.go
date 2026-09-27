// Command ei is the Engineering Intelligence CLI. Slice 1: sync GitHub.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/github"
)

type repoList []string

func (r *repoList) String() string     { return fmt.Sprint([]string(*r)) }
func (r *repoList) Set(v string) error { *r = append(*r, v); return nil }

const usage = `usage:
  ei sync github --owner ORG [--repo REPO]... [--checkpoint FILE] [--since RFC3339]

GITHUB_TOKEN env var enables authenticated requests (60 req/hr otherwise).`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ei:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) < 2 || args[0] != "sync" || args[1] != "github" {
		return fmt.Errorf("bad command\n%s", usage)
	}
	fs := flag.NewFlagSet("ei sync github", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(fs.Output(), usage) }
	owner := fs.String("owner", "", "GitHub organization login")
	var repos repoList
	fs.Var(&repos, "repo", "repository name (repeatable; empty = all org repos)")
	checkpointPath := fs.String("checkpoint", ".ei/checkpoint.json", "checkpoint file")
	sinceStr := fs.String("since", "", "backfill override (RFC3339); empty = checkpoint watermark")
	if err := fs.Parse(args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *owner == "" {
		return fmt.Errorf("--owner is required\n%s", usage)
	}
	var since time.Time
	if *sinceStr != "" {
		t, err := time.Parse(time.RFC3339, *sinceStr)
		if err != nil {
			return fmt.Errorf("--since: %w", err)
		}
		since = t
	}

	cfg := github.Config{
		Owner:          *owner,
		Repos:          repos,
		CheckpointPath: *checkpointPath,
		Since:          since,
		Token:          os.Getenv("GITHUB_TOKEN"),
	}
	res, err := github.Sync(context.Background(), cfg)
	if err != nil {
		return err
	}
	printSummary(res, *checkpointPath)
	return nil
}

func printSummary(res *github.SyncResult, path string) {
	fmt.Printf("run %s\n", res.RunID)
	types := make([]string, 0, len(res.Counts))
	for t := range res.Counts {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		c := res.Counts[t]
		if c == (github.Count{}) {
			continue // re-seen unchanged: nothing to report
		}
		fmt.Printf("  %-14s new %d, updated %d, skipped %d\n", t, c.New, c.Updated, c.Skipped)
	}
	fmt.Printf("relationships: %d\ncheckpoint: %s\n", res.Relationships, path)
}
