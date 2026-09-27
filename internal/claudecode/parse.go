// parse.go reads one Claude Code transcript (.jsonl) into normalized §25
// records. A transcript is a log the tool itself wrote: every extracted
// object is backed by a line in it, and the DIRECT attribution cites the
// tool_use record as evidence. Lines this connector does not understand
// (mode markers, summaries, file-history snapshots) are skipped, never
// guessed from.
package claudecode

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// source is the telemetry source name, carried as an attribute per §25.
const source = "claude-code"

// repoOf derives owner/name for a working directory (test seam).
type repoOf func(cwd string) string

// line is the subset of a transcript entry this connector reads.
type line struct {
	Type      string          `json:"type"`
	UUID      string          `json:"uuid"`
	SessionID string          `json:"sessionId"`
	Timestamp string          `json:"timestamp"`
	Cwd       string          `json:"cwd"`
	Message   json.RawMessage `json:"message"`
}

type message struct {
	Role    string            `json:"role"`
	ID      string            `json:"id"`
	Model   string            `json:"model"`
	Content []json.RawMessage `json:"content"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	IsError   bool            `json:"is_error"`
}

// parsed is what one transcript file proves.
type parsed struct {
	session       ontology.CodingSession
	interactions  []ontology.Interaction
	runs          []ontology.AgentRun
	tasks         []ontology.AgentTask
	contributions []ontology.CodeContribution
	unattributed  int // code edits whose repository could not be derived
	unparsed      int // transcript lines that were not valid JSON
}

// toolUse is one tool_use block, matched to its result after the scan.
type toolUse struct {
	id   string
	name string
	run  string // assistant message uuid owning the block
	ts   time.Time
	cwd  string
	path string // file_path for code tools, "" otherwise
}

// toolResult is one tool_result block.
type toolResult struct {
	text      string
	isError   bool
	timestamp time.Time
}

// prURLRe matches the PR URL a `gh pr create`-style output records.
var prURLRe = regexp.MustCompile(`github\.com/([^/\s]+)/([^/\s]+)/pull/(\d+)`)

// codeTools are the tool_use names that edit repository files.
var codeTools = map[string]bool{"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true}

func parseFile(path string, repo repoOf) (*parsed, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("claudecode: open %s: %w", path, err)
	}
	defer f.Close()
	return parseTranscript(f, filepath.Base(path), repo)
}

func parseTranscript(r io.Reader, file string, repo repoOf) (*parsed, error) {
	p := &parsed{}
	results := map[string]toolResult{}
	var uses []toolUse
	prNumbers := map[string]int{} // repo -> first PR URL seen (transcripts are chronological)
	var lastModel string
	var firstTS, lastTS time.Time

	scan := bufio.NewScanner(r)
	scan.Buffer(make([]byte, 1024*1024), 32*1024*1024) // assistant lines can be tens of MB
	for scan.Scan() {
		var l line
		if err := json.Unmarshal(scan.Bytes(), &l); err != nil {
			p.unparsed++ // a transcript being written ends mid-line; skip, don't fail
			continue
		}
		ts := parseTS(l.Timestamp)
		if ts.IsZero() {
			continue // no timestamp: nothing temporal to record (mode/last-prompt lines)
		}
		if firstTS.IsZero() || ts.Before(firstTS) {
			firstTS = ts
		}
		if ts.After(lastTS) {
			lastTS = ts
		}
		if p.session.SessionID == "" && l.SessionID != "" {
			p.session.SessionID = l.SessionID
		}
		var m message
		if len(l.Message) == 0 || json.Unmarshal(l.Message, &m) != nil {
			continue
		}
		switch m.Role {
		case "assistant":
			if m.Model != "" {
				lastModel = m.Model
			}
			runID := ""
			for _, raw := range m.Content {
				var b block
				if json.Unmarshal(raw, &b) != nil || b.Type != "tool_use" {
					continue
				}
				if runID == "" {
					runID = l.UUID
					if runID == "" {
						runID = m.ID
					}
					if runID == "" {
						break // unidentifiable run: skip its tasks too
					}
					p.runs = append(p.runs, ontology.AgentRun{
						Source: source, ID: runID, Agent: lastModel,
						StartedAt: ts,
					})
				}
				uses = append(uses, toolUse{id: b.ID, name: b.Name, run: runID, ts: ts, cwd: l.Cwd, path: filePath(b)})
			}
		case "user":
			for _, raw := range m.Content {
				var b block
				if json.Unmarshal(raw, &b) != nil {
					continue
				}
				switch b.Type {
				case "text":
					if b.Text != "" {
						p.interactions = append(p.interactions, ontology.Interaction{
							Source: source, ID: l.UUID, Agent: source,
							Model: lastModel, StartedAt: ts, EndedAt: ts,
						})
					}
				case "tool_result":
					text := blockText(b.Content)
					results[b.ToolUseID] = toolResult{text: text, isError: b.IsError, timestamp: ts}
					if m := prURLRe.FindStringSubmatch(text); m != nil {
						ownerRepo := m[1] + "/" + m[2]
						if _, seen := prNumbers[ownerRepo]; !seen {
							prNumbers[ownerRepo] = atoi(m[3])
						}
					}
				}
			}
		}
	}
	if err := scan.Err(); err != nil {
		return nil, fmt.Errorf("claudecode: scan %s: %w", file, err)
	}
	if p.session.SessionID == "" {
		p.session.SessionID = strings.TrimSuffix(file, filepath.Ext(file)) // fallback: file name
	}
	if !firstTS.IsZero() {
		p.session.StartedAt = firstTS
		p.session.EndedAt = lastTS
	}
	p.session.Source = source
	p.session.Agent = lastModel

	runEnd := map[string]time.Time{}
	runFailed := map[string]bool{}
	for _, u := range uses {
		tk := ontology.AgentTask{
			Source: source, ID: u.id,
			Run:         ontology.AgentRunExternalID(source, u.run),
			Description: u.name,
			Status:      "pending", // no result seen: the run has not finished in this file
		}
		if r, ok := results[u.id]; ok {
			tk.Status = "completed"
			if r.isError {
				tk.Status = "failed"
			}
			tk.CompletedAt = r.timestamp
			if r.isError && strings.Contains(strings.ToLower(r.text), "permission denied") {
				tk.HumanIntervention = true
			}
			if end, seen := runEnd[u.run]; !seen || r.timestamp.After(end) {
				runEnd[u.run] = r.timestamp
			}
			if r.isError {
				runFailed[u.run] = true
			}
		}
		p.tasks = append(p.tasks, tk)

		if u.path == "" {
			continue
		}
		repoName := ""
		if repo != nil {
			repoName = repo(u.cwd)
		}
		if repoName == "" {
			p.unattributed++ // no repository identity: the edit can never match a PR
			continue
		}
		p.contributions = append(p.contributions, ontology.CodeContribution{
			Source:     source,
			ID:         p.session.SessionID + ":" + u.id,
			Repository: repoName,
			PRNumber:   prNumbers[repoName],
			Session:    ontology.SessionExternalID(source, p.session.SessionID),
			Attribution: ontology.Attribution{
				Level:        ontology.AttributionDirect,
				Source:       source,
				Evidence:     "claude-code transcript " + file + ": " + u.id,
				AttributedAt: u.ts,
			},
		})
	}
	for i := range p.runs {
		if end, ok := runEnd[p.runs[i].ID]; ok {
			p.runs[i].EndedAt = end
		}
		if runFailed[p.runs[i].ID] {
			p.runs[i].Status = "failed"
		} else {
			p.runs[i].Status = "completed" // no failure observed; a truncated run just has no EndedAt
		}
	}
	return p, nil
}

func parseTS(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// filePath returns the edited file for code tools, "" for anything else.
func filePath(b block) string {
	if !codeTools[b.Name] {
		return ""
	}
	var in struct {
		FilePath string `json:"file_path"`
	}
	if err := json.Unmarshal(b.Input, &in); err != nil || in.FilePath == "" {
		return ""
	}
	return in.FilePath
}

// blockText concatenates the text blocks of a tool_result content array;
// a plain-string content is returned as-is.
func blockText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var blocks []block
	if json.Unmarshal(raw, &blocks) != nil {
		return ""
	}
	var sb strings.Builder
	for _, b := range blocks {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

// defaultRepo reads <cwd>/.git/config for the origin remote's owner/name.
// ponytail: no directory walk-up — Claude Code sessions run at the repo root.
func defaultRepo(cwd string) string {
	f, err := os.Open(filepath.Join(cwd, ".git", "config"))
	if err != nil {
		return ""
	}
	defer f.Close()
	ownerRepo, err := ownerRepoFromGitConfig(f)
	if err != nil {
		return ""
	}
	return ownerRepo
}

var (
	httpsURLRe = regexp.MustCompile(`^https://github\.com/([^/]+)/([^.]+?)(?:\.git)?/?$`)
	sshURLRe   = regexp.MustCompile(`^git@github\.com:([^/]+)/([^.]+?)(?:\.git)?$`)
)

// ownerRepoFromGitConfig extracts owner/name from an origin remote URL.
func ownerRepoFromGitConfig(r io.Reader) (string, error) {
	sc := bufio.NewScanner(r)
	origin := false
	for sc.Scan() {
		trimmed := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(trimmed, "[remote "):
			origin = strings.Contains(trimmed, "\"origin\"")
		case strings.HasPrefix(trimmed, "["):
			origin = false
		case origin && strings.HasPrefix(trimmed, "url = "):
			u := strings.TrimSpace(strings.TrimPrefix(trimmed, "url = "))
			for _, re := range []*regexp.Regexp{httpsURLRe, sshURLRe} {
				if m := re.FindStringSubmatch(u); m != nil {
					return m[1] + "/" + m[2], nil
				}
			}
			return "", fmt.Errorf("claudecode: unsupported remote url %q", u)
		}
	}
	if err := sc.Err(); err != nil {
		return "", fmt.Errorf("claudecode: read git config: %w", err)
	}
	return "", fmt.Errorf("claudecode: no origin remote url")
}
