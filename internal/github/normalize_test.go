package github

import (
	"testing"
	"time"

	gh "github.com/google/go-github/v92/github"
)

func tst(t time.Time) *gh.Timestamp { return &gh.Timestamp{Time: t} }
func strptr(s string) *string       { return &s }
func int64ptr(i int64) *int64       { return &i }
func intptr(i int) *int             { return &i }
func boolptr(b bool) *bool          { return &b }

// The PR list endpoint never populates `merged` (nil in every list response);
// merged_at is the authoritative merge signal there. Dogfood: aikoql#6 closed
// with merged_at set normalized as Merged=false and dropped out of every
// merged-PR metric.
func TestToPullRequestMergedFromMergedAt(t *testing.T) {
	mergedAt := time.Date(2026, 9, 24, 5, 25, 45, 0, time.UTC)
	got := toPullRequest(&gh.PullRequest{
		Merged:   nil,
		MergedAt: &gh.Timestamp{Time: mergedAt},
	}, "anckursingh", "aikoql")
	if !got.Merged {
		t.Fatalf("toPullRequest with merged_at set: Merged = false, want true")
	}
	if !got.MergedAt.Equal(mergedAt) {
		t.Fatalf("MergedAt = %v, want %v", got.MergedAt, mergedAt)
	}

	// Open PR: no merged_at → not merged.
	if got := toPullRequest(&gh.PullRequest{Merged: nil}, "anckursingh", "aikoql"); got.Merged {
		t.Fatalf("toPullRequest without merged_at: Merged = true, want false")
	}
}

// The PR list endpoint carries additions/deletions — the PR size metric's
// only data source (roadmap Layer A).
func TestToPullRequestMapsDiffSize(t *testing.T) {
	got := toPullRequest(&gh.PullRequest{
		Additions: intptr(120),
		Deletions: intptr(35),
	}, "anckursingh", "aikoql")
	if got.Additions != 120 || got.Deletions != 35 {
		t.Fatalf("diff size = %d+/%d-, want 120+/35-", got.Additions, got.Deletions)
	}
}

// The PR list endpoint leaves merge_method and requested_reviewers
// unpopulated — they come with the single-PR GET, along with labels and
// draft. toPullRequestDetail maps all four (roadmap Milestone B: richer PR
// metadata). merge_method needs the raw decode: go-github's PullRequest
// struct drops it.
func TestToPullRequestMapsRicherMetadata(t *testing.T) {
	got := toPullRequestDetail(&pullDetail{
		PullRequest: gh.PullRequest{
			Labels:             []*gh.Label{{Name: "enhancement"}, {Name: "bug"}},
			Draft:              boolptr(true),
			RequestedReviewers: []*gh.User{{Login: strptr("bob")}, {Login: strptr("ann")}},
		},
		MergeMethod: "squash",
	}, "acme", "widgets")
	if len(got.Labels) != 2 || got.Labels[0] != "enhancement" || got.Labels[1] != "bug" {
		t.Fatalf("labels = %v, want [enhancement bug]", got.Labels)
	}
	if !got.Draft {
		t.Fatalf("draft = false, want true")
	}
	if got.MergeMethod != "squash" {
		t.Fatalf("merge method = %q, want squash", got.MergeMethod)
	}
	if len(got.RequestedReviewers) != 2 || got.RequestedReviewers[0] != "bob" || got.RequestedReviewers[1] != "ann" {
		t.Fatalf("requested reviewers = %v, want [bob ann]", got.RequestedReviewers)
	}
}

// toBuild maps a workflow run. The list endpoint carries no completed_at:
// a completed run's updated_at is its completion (GitHub stops touching the
// record once the run finishes), an unfinished run has no completion time.
func TestToBuild(t *testing.T) {
	started := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
	finished := started.Add(30 * time.Minute)
	got := toBuild(&gh.WorkflowRun{
		ID:           int64ptr(200),
		Name:         strptr("CI"),
		HeadSHA:      strptr("abc"),
		Status:       strptr("completed"),
		Conclusion:   strptr("success"),
		HTMLURL:      strptr("https://github.com/acme/widgets/actions/runs/200"),
		RunStartedAt: tst(started),
		UpdatedAt:    tst(finished),
	}, "acme", "widgets")
	if got.Repository != "acme/widgets" || got.ID != 200 || got.Name != "CI" || got.HeadSHA != "abc" {
		t.Fatalf("toBuild fields = %+v", got)
	}
	if got.Conclusion != "success" || got.Status != "completed" || !got.StartedAt.Equal(started) || !got.CompletedAt.Equal(finished) {
		t.Fatalf("toBuild completed run = %+v, want conclusion/status + completion times", got)
	}

	// In-progress run: updated_at is not a completion time.
	got = toBuild(&gh.WorkflowRun{
		ID:           int64ptr(201),
		Status:       strptr("in_progress"),
		RunStartedAt: tst(started),
		UpdatedAt:    tst(finished),
	}, "acme", "widgets")
	if !got.CompletedAt.IsZero() {
		t.Fatalf("in-progress run CompletedAt = %v, want zero (updated_at is not completion)", got.CompletedAt)
	}
}
