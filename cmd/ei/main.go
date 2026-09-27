// Command ei is the Engineering Intelligence CLI (§38 runnable MVP): sync
// the connectors and serve the intelligence API, all over one AIKOQL store
// (AIKOQL_MCP_BIN names the server binary; --db names its database dir).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/ancku/aikoql-sdk"

	"github.com/anckursingh/engineering-intelligence/internal/claudecode"
	"github.com/anckursingh/engineering-intelligence/internal/github"
	"github.com/anckursingh/engineering-intelligence/internal/intelligence"
	"github.com/anckursingh/engineering-intelligence/internal/jira"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
)

type repoList []string

func (r *repoList) String() string     { return fmt.Sprint([]string(*r)) }
func (r *repoList) Set(v string) error { *r = append(*r, v); return nil }

const usage = `usage:
  ei sync github --owner ORG [--repo REPO]... [--checkpoint FILE] [--since RFC3339] --db DIR
  ei sync jira --base-url URL --email EMAIL --token TOKEN --project KEY [--since RFC3339] --db DIR
  ei ingest claude --dir DIR --db DIR
  ei serve --db DIR [--addr :8080]

All commands need AIKOQL_MCP_BIN pointing at the aikoql-mcp server binary
and --db naming its database directory. GITHUB_TOKEN enables authenticated
GitHub requests (60 req/hr otherwise).`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "ei:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("bad command\n%s", usage)
	}
	switch args[0] {
	case "sync":
		if len(args) < 2 {
			return fmt.Errorf("bad command\n%s", usage)
		}
		switch args[1] {
		case "github":
			return runGitHub(args[2:])
		case "jira":
			return runJira(args[2:])
		}
	case "ingest":
		if len(args) >= 2 && args[1] == "claude" {
			return runClaude(args[2:])
		}
	case "serve":
		return runServe(args[1:])
	}
	return fmt.Errorf("bad command\n%s", usage)
}

// openStore spawns the AIKOQL server named by AIKOQL_MCP_BIN over stdio on
// dbDir and returns a KnowledgeStore on it. The returned close shuts the
// server down; the store is unusable afterwards.
func openStore(dbDir string) (knowledge.KnowledgeStore, func(), error) {
	bin := os.Getenv("AIKOQL_MCP_BIN")
	if bin == "" {
		return nil, nil, fmt.Errorf("AIKOQL_MCP_BIN not set (path to the aikoql-mcp server binary)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	c, err := aikoql.DialStdio(ctx, bin, "serve", dbDir)
	if err != nil {
		return nil, nil, fmt.Errorf("dial aikoql: %w", err)
	}
	if err := c.Initialize(ctx); err != nil {
		c.Close()
		return nil, nil, fmt.Errorf("initialize aikoql: %w", err)
	}
	return knowledge.NewAikoql(c), func() { c.Close() }, nil
}

func runGitHub(args []string) error {
	fs := flag.NewFlagSet("ei sync github", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(fs.Output(), usage) }
	owner := fs.String("owner", "", "GitHub organization login")
	var repos repoList
	fs.Var(&repos, "repo", "repository name (repeatable; empty = all org repos)")
	checkpointPath := fs.String("checkpoint", ".ei/checkpoint.json", "checkpoint file")
	sinceStr := fs.String("since", "", "backfill override (RFC3339); empty = checkpoint watermark")
	dbDir := fs.String("db", "", "AIKOQL database directory")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *owner == "" {
		return fmt.Errorf("--owner is required\n%s", usage)
	}
	if *dbDir == "" {
		return fmt.Errorf("--db is required\n%s", usage)
	}
	since, err := parseSince(*sinceStr)
	if err != nil {
		return err
	}
	store, closeStore, err := openStore(*dbDir)
	if err != nil {
		return err
	}
	defer closeStore()

	res, err := github.Sync(context.Background(), github.Config{
		Owner:          *owner,
		Repos:          repos,
		CheckpointPath: *checkpointPath,
		Since:          since,
		Token:          os.Getenv("GITHUB_TOKEN"),
		Store:          store,
	})
	if err != nil {
		return err
	}
	printGitHub(res, *checkpointPath)
	return nil
}

func runJira(args []string) error {
	fs := flag.NewFlagSet("ei sync jira", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(fs.Output(), usage) }
	baseURL := fs.String("base-url", "", "Jira base URL (e.g. https://acme.atlassian.net)")
	email := fs.String("email", "", "Jira account email")
	token := fs.String("token", "", "Jira API token")
	project := fs.String("project", "", "Jira project key")
	checkpointPath := fs.String("checkpoint", ".ei/jira-checkpoint.json", "checkpoint file")
	sinceStr := fs.String("since", "", "backfill override (RFC3339); empty = checkpoint watermark")
	dbDir := fs.String("db", "", "AIKOQL database directory")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *baseURL == "" || *email == "" || *token == "" || *project == "" {
		return fmt.Errorf("--base-url, --email, --token and --project are required\n%s", usage)
	}
	if *dbDir == "" {
		return fmt.Errorf("--db is required\n%s", usage)
	}
	since, err := parseSince(*sinceStr)
	if err != nil {
		return err
	}
	store, closeStore, err := openStore(*dbDir)
	if err != nil {
		return err
	}
	defer closeStore()

	res, err := jira.Sync(context.Background(), jira.Config{
		BaseURL:        *baseURL,
		Email:          *email,
		Token:          *token,
		Project:        *project,
		CheckpointPath: *checkpointPath,
		Since:          since,
		Store:          store,
	})
	if err != nil {
		return err
	}
	fmt.Printf("run %s\n", res.RunID)
	types := make([]string, 0, len(res.Counts))
	for t := range res.Counts {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		c := res.Counts[t]
		fmt.Printf("  %-14s new %d, updated %d\n", t, c.New, c.Updated)
	}
	fmt.Printf("checkpoint: %s\n", *checkpointPath)
	return nil
}

func runClaude(args []string) error {
	fs := flag.NewFlagSet("ei ingest claude", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(fs.Output(), usage) }
	dir := fs.String("dir", "", "Claude Code transcript directory (.jsonl files)")
	dbDir := fs.String("db", "", "AIKOQL database directory")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *dir == "" {
		return fmt.Errorf("--dir is required\n%s", usage)
	}
	if *dbDir == "" {
		return fmt.Errorf("--db is required\n%s", usage)
	}
	store, closeStore, err := openStore(*dbDir)
	if err != nil {
		return err
	}
	defer closeStore()

	res, err := claudecode.Sync(context.Background(), claudecode.Config{Dir: *dir, Store: store})
	if err != nil {
		return err
	}
	fmt.Printf("run %s\n", res.RunID)
	types := make([]string, 0, len(res.Counts))
	for t := range res.Counts {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		c := res.Counts[t]
		if c == (claudecode.Count{}) {
			continue // re-seen unchanged: nothing to report
		}
		fmt.Printf("  %-18s new %d, updated %d, skipped %d\n", t, c.New, c.Updated, c.Skipped)
	}
	fmt.Printf("sessions: %d, relationships: %d\n", res.Sessions, res.Relationships)
	if res.Unlinked+res.Unattributed+res.Unparsed > 0 {
		fmt.Printf("gaps: unlinked %d, unattributed %d, unparsed lines %d\n",
			res.Unlinked, res.Unattributed, res.Unparsed)
	}
	return nil
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("ei serve", flag.ContinueOnError)
	fs.Usage = func() { fmt.Fprint(fs.Output(), usage) }
	dbDir := fs.String("db", "", "AIKOQL database directory")
	addr := fs.String("addr", ":8080", "listen address")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *dbDir == "" {
		return fmt.Errorf("--db is required\n%s", usage)
	}
	store, _, err := openStore(*dbDir) // the store lives for the process lifetime
	if err != nil {
		return err
	}
	fmt.Printf("serving Engineering Intelligence API on %s\n", *addr)
	return http.ListenAndServe(*addr, intelligence.NewAPI(store))
}

func parseSince(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("--since: %w", err)
	}
	return t, nil
}

func printGitHub(res *github.SyncResult, path string) {
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
