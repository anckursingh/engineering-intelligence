package github

import (
	"testing"
	"time"

	gh "github.com/google/go-github/v92/github"
)

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
