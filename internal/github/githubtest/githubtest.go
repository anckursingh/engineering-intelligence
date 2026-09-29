// Package githubtest exports the fake GitHub world shared by the connector's
// unit tests (internal/github), the black-box acceptance suite
// (test/acceptance) and the e2e suite (test/e2e) — one fixture, not three
// copies.
package githubtest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	gh "github.com/google/go-github/v92/github"

	"github.com/anckursingh/engineering-intelligence/internal/github"
	"github.com/anckursingh/engineering-intelligence/internal/knowledge"
)

// Fixture SHAs (the acme/widgets default branch). Squash/Rebase are the
// merge-kind variants (§13.3); Dead is a merge SHA absent from history;
// PrSha lives only on PR#3's branch — never on the default branch.
var (
	ShaA      = strings.Repeat("a", 40)
	ShaB      = strings.Repeat("b", 40)
	SquashSha = strings.Repeat("c", 40)
	RebaseSha = strings.Repeat("d", 40)
	DeadSha   = strings.Repeat("e", 40)
	PrSha     = strings.Repeat("f", 40)
)

func tst(t time.Time) *gh.Timestamp { return &gh.Timestamp{Time: t} }
func strptr(s string) *string       { return &s }
func intptr(i int) *int             { return &i }
func int64ptr(i int64) *int64       { return &i }
func boolptr(b bool) *bool          { return &b }

// World is the mutable fake "acme/widgets" GitHub world: one org, one repo,
// two commits, two issues, one merged PR, one review. Tests mutate it between
// sync runs to create deltas; it serves its own httptest server.
type World struct {
	mu sync.Mutex

	org     *gh.Organization
	user    *gh.User
	repos   []*gh.Repository
	commits []*gh.RepositoryCommit
	issues  []*gh.Issue
	issue1  *gh.Issue // /issues/{n} on-demand refetch
	prs     []*gh.PullRequest
	reviews []*gh.PullRequestReview
	runs    []*gh.WorkflowRun

	// prCommits serves /pulls/{n}/commits — the PR's own branch commits,
	// which the default-branch walk never sees. Absent numbers answer the
	// empty list, like the real API for a PR with no commits left.
	prCommits map[int][]*gh.RepositoryCommit

	// prDetails serves /pulls/{n} as the wire body: the real endpoint's
	// response carries merge_method/requested_reviewers, which the list
	// entries above (and go-github's PullRequest struct) lack. Absent
	// numbers fall back to the list entry, like the real API's shape.
	prDetails map[int]map[string]any

	userOwner      bool // /orgs/{owner} 404s; /users/{owner} serves the account
	failOrgOnce    bool
	failOrg        bool
	orgCalls       int
	issueListCalls int
	emptyRepos     map[string]bool // commits endpoint answers GitHub's real 409 for these

	server *httptest.Server
}

// NewWorld builds the fixture world and starts its HTTP server (closed by
// t.Cleanup).
func NewWorld(t *testing.T) *World {
	t.Helper()
	t0 := time.Now().UTC().Add(-10 * 24 * time.Hour)
	w := &World{
		org: &gh.Organization{
			Login:       strptr("acme"),
			Name:        strptr("Acme Inc"),
			Description: strptr("test org"),
			HTMLURL:     strptr("https://github.com/acme"),
			CreatedAt:   tst(t0),
			UpdatedAt:   tst(t0.Add(24 * time.Hour)),
		},
		repos: []*gh.Repository{{
			Name:          strptr("widgets"),
			FullName:      strptr("acme/widgets"),
			Owner:         &gh.User{Login: strptr("acme")},
			Description:   strptr("widgets"),
			Language:      strptr("Go"),
			DefaultBranch: strptr("main"),
			HTMLURL:       strptr("https://github.com/acme/widgets"),
			CreatedAt:     tst(t0),
			UpdatedAt:     tst(t0.Add(24 * time.Hour)),
			PushedAt:      tst(t0.Add(24 * time.Hour)),
		}},
		commits: []*gh.RepositoryCommit{
			{
				SHA:     strptr(ShaA),
				HTMLURL: strptr("https://github.com/acme/widgets/commit/" + ShaA),
				Author:  &gh.User{Login: strptr("ann")},
				Commit: &gh.Commit{
					Message: strptr("first commit"),
					Author: &gh.CommitAuthor{
						Name:  strptr("Ann Coder"),
						Email: strptr("1234567+ann@users.noreply.github.com"),
						Date:  tst(t0.Add(2 * time.Hour)),
					},
				},
			},
			{
				SHA:     strptr(ShaB),
				HTMLURL: strptr("https://github.com/acme/widgets/commit/" + ShaB),
				Commit: &gh.Commit{
					Message: strptr("second commit"),
					Author: &gh.CommitAuthor{
						Name:  strptr("Bob"),
						Email: strptr("bob@corp.example"),
						Date:  tst(t0.Add(3 * time.Hour)),
					},
				},
			},
		},
		issues: []*gh.Issue{
			{
				Number:    intptr(1),
				Title:     strptr("Bug: widget wobbles"),
				Body:      strptr("it wobbles"),
				State:     strptr("open"),
				User:      &gh.User{Login: strptr("ann")},
				Labels:    []*gh.Label{{Name: "bug"}},
				HTMLURL:   strptr("https://github.com/acme/widgets/issues/1"),
				CreatedAt: tst(t0.Add(24 * time.Hour)),
				UpdatedAt: tst(t0.Add(25 * time.Hour)),
			},
			{
				Number:    intptr(2),
				Title:     strptr("Typo in README"),
				State:     strptr("closed"),
				User:      &gh.User{Login: strptr("bob")},
				HTMLURL:   strptr("https://github.com/acme/widgets/issues/2"),
				CreatedAt: tst(t0.Add(26 * time.Hour)),
				UpdatedAt: tst(t0.Add(27 * time.Hour)),
				ClosedAt:  tst(t0.Add(27 * time.Hour)),
			},
		},
		issue1: &gh.Issue{
			Number:    intptr(1),
			Title:     strptr("Bug: widget wobbles"),
			State:     strptr("open"),
			User:      &gh.User{Login: strptr("ann")},
			HTMLURL:   strptr("https://github.com/acme/widgets/issues/1"),
			CreatedAt: tst(t0.Add(24 * time.Hour)),
			UpdatedAt: tst(t0.Add(25 * time.Hour)),
		},
		prs: []*gh.PullRequest{{
			Number:         intptr(3),
			Title:          strptr("Fix wobbles"),
			Body:           strptr("Closes #1"),
			State:          strptr("closed"),
			Merged:         boolptr(true),
			User:           &gh.User{Login: strptr("ann")},
			Base:           &gh.PullRequestBranch{Ref: strptr("main")},
			Head:           &gh.PullRequestBranch{Ref: strptr("fix/wobbles")},
			MergeCommitSHA: strptr(ShaB),
			HTMLURL:        strptr("https://github.com/acme/widgets/pull/3"),
			CreatedAt:      tst(t0.Add(30 * time.Hour)),
			UpdatedAt:      tst(t0.Add(31 * time.Hour)),
			MergedAt:       tst(t0.Add(31 * time.Hour)),
		}},
		reviews: []*gh.PullRequestReview{
			{
				ID:          int64ptr(1001),
				User:        &gh.User{Login: strptr("ann")},
				State:       strptr("APPROVED"),
				HTMLURL:     strptr("https://github.com/acme/widgets/pull/3#pullrequestreview-1001"),
				SubmittedAt: tst(t0.Add(30*time.Hour + 30*time.Minute)),
			},
			{
				// The dismissed leg of the lifecycle: state flows verbatim.
				ID:          int64ptr(1002),
				User:        &gh.User{Login: strptr("ann")},
				State:       strptr("DISMISSED"),
				HTMLURL:     strptr("https://github.com/acme/widgets/pull/3#pullrequestreview-1002"),
				SubmittedAt: tst(t0.Add(30*time.Hour + 45*time.Minute)),
			},
		},
		// PR#3's branch commit: only the PR's commit list carries it (the
		// default-branch walk never sees PrSha). Same author as ShaA — the
		// sync must resolve to the one ann, not a second engineer.
		prCommits: map[int][]*gh.RepositoryCommit{
			3: {{
				SHA:     strptr(PrSha),
				HTMLURL: strptr("https://github.com/acme/widgets/commit/" + PrSha),
				Author:  &gh.User{Login: strptr("ann")},
				Commit: &gh.Commit{
					Message: strptr("fix the wobbles"),
					Author: &gh.CommitAuthor{
						Name:  strptr("Ann Coder"),
						Email: strptr("1234567+ann@users.noreply.github.com"),
						Date:  tst(t0.Add(29 * time.Hour)),
					},
				},
			}},
		},
		// The single-PR GET for #3: the richer record (labels/draft/merge
		// method/requested reviewers) in wire form — a raw map, since the
		// connector decodes it directly.
		prDetails: map[int]map[string]any{
			3: {
				"number":              3,
				"title":               "Fix wobbles",
				"body":                "Closes #1",
				"state":               "closed",
				"merged":              true,
				"user":                map[string]any{"login": "ann"},
				"base":                map[string]any{"ref": "main"},
				"head":                map[string]any{"ref": "fix/wobbles"},
				"merge_commit_sha":    ShaB,
				"labels":              []map[string]any{{"name": "enhancement"}},
				"draft":               false,
				"merge_method":        "squash",
				"requested_reviewers": []map[string]any{{"login": "bob"}},
				"html_url":            "https://github.com/acme/widgets/pull/3",
				"created_at":          t0.Add(30 * time.Hour).Format(time.RFC3339),
				"updated_at":          t0.Add(31 * time.Hour).Format(time.RFC3339),
				"merged_at":           t0.Add(31 * time.Hour).Format(time.RFC3339),
			},
		},
	}
	w.server = httptest.NewServer(w)
	t.Cleanup(w.server.Close)
	return w
}

// SyncConfig returns the github.Config that talks to this world's server.
// The transport rewrites api.github.com → the httptest server (go-github v92
// has no exported BaseURL).
func (w *World) SyncConfig(dir string, store knowledge.KnowledgeStore) github.Config {
	u, err := url.Parse(w.server.URL)
	if err != nil {
		panic(err) // test helper: httptest URLs always parse
	}
	return github.Config{
		Owner:          "acme",
		CheckpointPath: filepath.Join(dir, "checkpoint.json"),
		HTTPClient:     &http.Client{Transport: rewriteTransport{base: u}},
		Sleep:          func(time.Duration) {},
		Store:          store,
	}
}

// NewUserWorld builds the same fixture world but with the owner as a
// personal account: /orgs/{owner} 404s (forcing the connector's user
// fallback) and /users/{owner} serves the account.
func NewUserWorld(t *testing.T) *World {
	w := NewWorld(t)
	w.userOwner = true
	w.user = &gh.User{
		Login:     strptr("acme"),
		Name:      strptr("Ace Coder"),
		Bio:       strptr("test user"),
		HTMLURL:   strptr("https://github.com/acme"),
		CreatedAt: w.org.CreatedAt,
		UpdatedAt: w.org.UpdatedAt,
	}
	return w
}

// NewIdentityWorld is the lean identity fixture: org + repo + two commits
// (ann via noreply login, bob via email only) — the identity carriers of the
// full world, nothing else. The live acceptance leg budgets ~120 aikoql
// calls per subtest (§15 ACs), and the full world's issues/PRs/reviews are
// irrelevant to identity resolution.
func NewIdentityWorld(t *testing.T) *World {
	w := NewWorld(t)
	w.issues = nil
	w.prs = nil
	w.reviews = nil
	return w
}

// AddDeltaActivity mutates the world between runs: issue #1 edited, #4 created.
func (w *World) AddDeltaActivity() {
	now := time.Now().UTC()
	w.mu.Lock()
	defer w.mu.Unlock()
	w.issues[0].UpdatedAt = tst(now.Add(2 * time.Hour))
	w.issues = append(w.issues, &gh.Issue{
		Number:    intptr(4),
		Title:     strptr("New bug"),
		State:     strptr("open"),
		User:      &gh.User{Login: strptr("ann")},
		HTMLURL:   strptr("https://github.com/acme/widgets/issues/4"),
		CreatedAt: tst(now.Add(time.Hour)),
		UpdatedAt: tst(now.Add(time.Hour)),
	})
}

// AddEmptyRepo appends a repo whose commits endpoint answers GitHub's real
// 409 "Git Repository is empty" (the API returns it instead of an empty
// list).
func (w *World) AddEmptyRepo(name string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.emptyRepos == nil {
		w.emptyRepos = map[string]bool{}
	}
	w.emptyRepos[name] = true
	w.repos = append(w.repos, &gh.Repository{
		Name:          strptr(name),
		FullName:      strptr("acme/" + name),
		Owner:         &gh.User{Login: strptr("acme")},
		DefaultBranch: strptr("main"),
		HTMLURL:       strptr("https://github.com/acme/" + name),
		CreatedAt:     w.org.CreatedAt,
		UpdatedAt:     w.org.UpdatedAt,
		PushedAt:      w.org.UpdatedAt,
	})
}

// AddMergeVariants appends the §13.3 merge-kind PRs: #4 squash merge, #5
// rebase merge (both land their commit on the default branch), #6 a merged
// PR whose merge SHA is not in branch history.
func (w *World) AddMergeVariants() {
	w.mu.Lock()
	defer w.mu.Unlock()
	t0 := time.Now().UTC().Add(-5 * 24 * time.Hour)
	w.commits = append(w.commits,
		&gh.RepositoryCommit{
			SHA:     strptr(SquashSha),
			HTMLURL: strptr("https://github.com/acme/widgets/commit/" + SquashSha),
			Author:  &gh.User{Login: strptr("ann")},
			Commit: &gh.Commit{
				Message: strptr("squashed wobble fix"),
				Author:  &gh.CommitAuthor{Name: strptr("Ann Coder"), Email: strptr("1234567+ann@users.noreply.github.com"), Date: tst(t0)},
			},
		},
		&gh.RepositoryCommit{
			SHA:     strptr(RebaseSha),
			HTMLURL: strptr("https://github.com/acme/widgets/commit/" + RebaseSha),
			Author:  &gh.User{Login: strptr("ann")},
			Commit: &gh.Commit{
				Message: strptr("rebased wobble fix"),
				Author:  &gh.CommitAuthor{Name: strptr("Ann Coder"), Email: strptr("1234567+ann@users.noreply.github.com"), Date: tst(t0.Add(time.Hour))},
			},
		},
	)
	for i, sha := range []string{SquashSha, RebaseSha, DeadSha} {
		w.prs = append(w.prs, &gh.PullRequest{
			Number:         intptr(4 + i),
			Title:          strptr("Merge variant"),
			State:          strptr("closed"),
			Merged:         boolptr(true),
			User:           &gh.User{Login: strptr("ann")},
			Base:           &gh.PullRequestBranch{Ref: strptr("main")},
			Head:           &gh.PullRequestBranch{Ref: strptr("variant/branch")},
			MergeCommitSHA: strptr(sha),
			HTMLURL:        strptr("https://github.com/acme/widgets/pull/" + strconv.Itoa(4+i)),
			CreatedAt:      tst(t0.Add(2 * time.Hour)),
			UpdatedAt:      tst(t0.Add(3 * time.Hour)),
			MergedAt:       tst(t0.Add(3 * time.Hour)),
		})
	}
}

// AddLinkVariants appends the §13.4 linking PRs: #7 references one issue
// twice, #8 references an issue that does not exist.
func (w *World) AddLinkVariants() {
	w.mu.Lock()
	defer w.mu.Unlock()
	t0 := time.Now().UTC().Add(-5 * 24 * time.Hour)
	for i, body := range []string{"Closes #2, fixes #2", "Closes #99"} {
		w.prs = append(w.prs, &gh.PullRequest{
			Number:    intptr(7 + i),
			Title:     strptr("Link variant"),
			Body:      strptr(body),
			State:     strptr("open"),
			User:      &gh.User{Login: strptr("ann")},
			Base:      &gh.PullRequestBranch{Ref: strptr("main")},
			Head:      &gh.PullRequestBranch{Ref: strptr("variant/branch")},
			HTMLURL:   strptr("https://github.com/acme/widgets/pull/" + strconv.Itoa(7+i)),
			CreatedAt: tst(t0.Add(2 * time.Hour)),
			UpdatedAt: tst(t0.Add(3 * time.Hour)),
		})
	}
}

// AddBuilds appends the CI fixture: one passed PR-linked run, one failed run,
// one in-progress run, one cancelled run (t0 = now − 10d, matching
// NewWorld's clock) — the outcome-vocabulary tests need every conclusion.
func (w *World) AddBuilds() {
	w.mu.Lock()
	defer w.mu.Unlock()
	t0 := time.Now().UTC().Add(-10 * 24 * time.Hour)
	w.runs = append(w.runs,
		&gh.WorkflowRun{
			ID:           int64ptr(200),
			Name:         strptr("CI"),
			HeadSHA:      strptr(ShaB),
			Status:       strptr("completed"),
			Conclusion:   strptr("success"),
			HTMLURL:      strptr("https://github.com/acme/widgets/actions/runs/200"),
			RunStartedAt: tst(t0.Add(32 * time.Hour)),
			UpdatedAt:    tst(t0.Add(33 * time.Hour)),
			PullRequests: []*gh.PullRequest{{Number: intptr(3)}},
		},
		&gh.WorkflowRun{
			ID:           int64ptr(201),
			Name:         strptr("CI"),
			HeadSHA:      strptr(ShaB),
			Status:       strptr("completed"),
			Conclusion:   strptr("failure"),
			HTMLURL:      strptr("https://github.com/acme/widgets/actions/runs/201"),
			RunStartedAt: tst(t0.Add(34 * time.Hour)),
			UpdatedAt:    tst(t0.Add(35 * time.Hour)),
		},
		&gh.WorkflowRun{
			ID:           int64ptr(202),
			Name:         strptr("CI"),
			HeadSHA:      strptr(ShaB),
			Status:       strptr("in_progress"),
			HTMLURL:      strptr("https://github.com/acme/widgets/actions/runs/202"),
			RunStartedAt: tst(t0.Add(36 * time.Hour)),
			UpdatedAt:    tst(t0.Add(36 * time.Hour)),
		},
		&gh.WorkflowRun{
			ID:           int64ptr(203),
			Name:         strptr("CI"),
			HeadSHA:      strptr(ShaB),
			Status:       strptr("completed"),
			Conclusion:   strptr("cancelled"),
			HTMLURL:      strptr("https://github.com/acme/widgets/actions/runs/203"),
			RunStartedAt: tst(t0.Add(37 * time.Hour)),
			UpdatedAt:    tst(t0.Add(37 * time.Hour)),
		},
	)
}

// FinishInProgressBuild completes run 202 (the delta test's second-run state).
func (w *World) FinishInProgressBuild() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.runs[2].Status = strptr("completed")
	w.runs[2].Conclusion = strptr("failure")
	w.runs[2].UpdatedAt = tst(time.Now().UTC())
}

// FailFirstOrgCallOnce makes the first org call return a 403 rate-limit
// response (the retry test).
func (w *World) FailFirstOrgCallOnce() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.failOrgOnce = true
}

// FailOrgAlways makes every org call return 500 (crash-safety tests).
func (w *World) FailOrgAlways() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.failOrg = true
}

// OrgCalls returns how many times the org endpoint was hit.
func (w *World) OrgCalls() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.orgCalls
}

// IssueListCalls returns how many times the issues list was fetched.
func (w *World) IssueListCalls() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.issueListCalls
}

// AssertReaches verifies that Traverse from a start koid reaches the wanted
// external IDs.
func AssertReaches(t *testing.T, store knowledge.KnowledgeStore, from, rel string, dir knowledge.Direction, depth int, wantExtIDs ...string) {
	t.Helper()
	objs, err := store.Traverse(context.Background(), from, rel, dir, depth)
	if err != nil {
		t.Fatalf("traverse %s: %v", rel, err)
	}
	got := map[string]bool{}
	for _, o := range objs {
		got[o.ExternalID] = true
	}
	for _, id := range wantExtIDs {
		if !got[id] {
			t.Errorf("%s dir=%v depth=%d: missing %s (got %v)", rel, dir, depth, id, got)
		}
	}
}

func (w *World) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	w.mu.Lock()
	defer w.mu.Unlock()
	switch {
	case r.URL.Path == "/orgs/acme":
		w.orgCalls++
		if w.userOwner {
			http.NotFound(rw, r)
			return
		}
		if w.failOrg {
			rw.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(rw).Encode(map[string]string{"message": "boom"})
			return
		}
		if w.failOrgOnce && w.orgCalls == 1 {
			rw.Header().Set("X-RateLimit-Remaining", "0")
			rw.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(-time.Hour).Unix(), 10))
			rw.WriteHeader(http.StatusForbidden)
			json.NewEncoder(rw).Encode(map[string]string{"message": "API rate limit exceeded"})
			return
		}
		encode(rw, w.org)
	case r.URL.Path == "/orgs/acme/repos":
		encode(rw, w.repos)
	case r.URL.Path == "/users/acme":
		if !w.userOwner {
			http.NotFound(rw, r)
			return
		}
		encode(rw, w.user)
	case r.URL.Path == "/users/acme/repos":
		if !w.userOwner {
			http.NotFound(rw, r)
			return
		}
		encode(rw, w.repos)
	case strings.HasPrefix(r.URL.Path, "/repos/acme/widgets/pulls/") && strings.HasSuffix(r.URL.Path, "/commits"):
		n, err := strconv.Atoi(strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/acme/widgets/pulls/"), "/commits"))
		if err != nil {
			http.NotFound(rw, r)
			return
		}
		if cs, ok := w.prCommits[n]; ok {
			encode(rw, cs)
			return
		}
		encode(rw, []*gh.RepositoryCommit{})
	case strings.HasPrefix(r.URL.Path, "/repos/acme/") && strings.HasSuffix(r.URL.Path, "/commits"):
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/acme/"), "/commits")
		if w.emptyRepos[name] {
			rw.WriteHeader(http.StatusConflict)
			if err := json.NewEncoder(rw).Encode(map[string]any{
				"message":           "Git Repository is empty.",
				"documentation_url": "https://docs.github.com/rest/commits/commits",
			}); err != nil {
				panic(err) // test server: no recovery needed
			}
			return
		}
		encode(rw, w.commits)
	case strings.HasPrefix(r.URL.Path, "/repos/acme/") && strings.HasSuffix(r.URL.Path, "/issues"):
		if r.URL.Path == "/repos/acme/widgets/issues" {
			w.issueListCalls++
			encode(rw, w.issues)
			return
		}
		encode(rw, []*gh.Issue{}) // other repos: empty (like the real API)
	case r.URL.Path == "/repos/acme/widgets/issues/1":
		encode(rw, w.issue1)
	case strings.HasPrefix(r.URL.Path, "/repos/acme/") && strings.HasSuffix(r.URL.Path, "/pulls"):
		if r.URL.Path == "/repos/acme/widgets/pulls" {
			encode(rw, w.prs)
			return
		}
		encode(rw, []*gh.PullRequest{})
	case strings.HasPrefix(r.URL.Path, "/repos/acme/widgets/pulls/") && strings.HasSuffix(r.URL.Path, "/reviews"):
		if r.URL.Path == "/repos/acme/widgets/pulls/3/reviews" {
			encode(rw, w.reviews)
			return
		}
		encode(rw, []*gh.PullRequestReview{})
	case strings.HasPrefix(r.URL.Path, "/repos/acme/widgets/pulls/"):
		// single-PR GET: the wire detail when the world enriches it, the
		// list entry otherwise.
		n, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/repos/acme/widgets/pulls/"))
		if err != nil {
			http.NotFound(rw, r)
			return
		}
		if d, ok := w.prDetails[n]; ok {
			encode(rw, d)
			return
		}
		for _, p := range w.prs {
			if p.GetNumber() == n {
				encode(rw, p)
				return
			}
		}
		http.NotFound(rw, r)
	case strings.HasPrefix(r.URL.Path, "/repos/acme/") && strings.HasSuffix(r.URL.Path, "/actions/runs"):
		if r.URL.Path == "/repos/acme/widgets/actions/runs" {
			encode(rw, &gh.WorkflowRuns{TotalCount: intptr(len(w.runs)), WorkflowRuns: w.runs})
			return
		}
		encode(rw, &gh.WorkflowRuns{WorkflowRuns: []*gh.WorkflowRun{}})
	default:
		http.NotFound(rw, r)
	}
}

func encode(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		panic(err) // test server: no recovery needed
	}
}

// rewriteTransport redirects every request to the httptest server.
type rewriteTransport struct{ base *url.URL }

func (rt rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = rt.base.Scheme
	req.URL.Host = rt.base.Host
	return http.DefaultTransport.RoundTrip(req)
}
