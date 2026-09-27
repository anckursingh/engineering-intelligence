package ontology

// Relationship vocabulary, finalized per §12 before Phase-2 data accumulates.
// Do not rename persisted relationship types casually — every name below is a
// data contract, and every row is exercised by at least one test (AC-KG-001
// or the sync suite):
//
//	BELONGS_TO      source Repository → target Organization
//	                meaning: the repo lives under the org
//	                inverse: org contains repos (inbound traversal)
//	                cardinality: N:1
//	                test: TestSyncRun1Full (repo reaches org)
//
//	AUTHORED        source Engineer → target Commit | PullRequest
//	                meaning: the engineer wrote it
//	                inverse: authored by (inbound traversal)
//	                cardinality: N:M
//	                test: AC-KG-001 + TestSyncRun1Full (commit → author)
//
//	IMPLEMENTS      source PullRequest → target Issue
//	                meaning: the PR body references/closes the issue
//	                inverse: implemented by (inbound traversal)
//	                cardinality: N:M
//	                test: AC-KG-001 + TestPRToIssueLinks (§13.4)
//
//	TARGETS         source PullRequest → target Repository
//	                meaning: the PR is raised against the repo
//	                inverse: targeted by (inbound traversal)
//	                cardinality: N:1
//	                test: AC-KG-001
//
//	REVIEWED_BY     source PullRequest → target Engineer
//	                meaning: the engineer reviewed the PR
//	                inverse: reviewed (inbound traversal)
//	                cardinality: N:M
//	                test: AC-KG-001 + tenant-scoped resolution test
//
//	MERGED_AS       source PullRequest → target Commit
//	                meaning: the PR merged as this commit
//	                inverse: merged-as (inbound traversal)
//	                cardinality: 1:1 per merge commit (head-commit
//	                modeling joins in §13.3 without renaming this edge)
//	                test: AC-KG-001 + TestSyncRun1Full + TestMergeCommitModel
//
//	CONTAINS_REVIEW source PullRequest → target Review
//	                meaning: the review belongs to the PR
//	                inverse: part of PR (inbound traversal)
//	                cardinality: 1:N
//	                test: AC-KG-001 + TestSyncRun1Full
//
//	RESOLVES_TO     source SourceIdentity → target Engineer
//	                meaning: this source identity is that canonical engineer;
//	                identity-layer edge, never a source-domain fact
//	                inverse: resolved identities (inbound traversal)
//	                cardinality: N:1 — many claims, one engineer
//	                test: resolver suite (§9) + AC-ID-001/AC-ID-003
//
//	AI_CONTRIBUTES  source CodeContribution → target PullRequest
//	                meaning: this AI contribution shaped the PR (§26-27)
//	                inverse: AI-assisted PRs (inbound traversal)
//	                cardinality: N:1 per PR
//	                test: TestPopulationCollectsContributions (population walk)
type RelType string

const (
	RelBelongsTo      RelType = "BELONGS_TO"
	RelAuthored       RelType = "AUTHORED"
	RelImplements     RelType = "IMPLEMENTS"
	RelTargets        RelType = "TARGETS"
	RelReviewedBy     RelType = "REVIEWED_BY"
	RelMergedAs       RelType = "MERGED_AS"
	RelContainsReview RelType = "CONTAINS_REVIEW"
	RelPassed         RelType = "PASSED" // PullRequest → Build; reserved until Build fetch lands
	RelResolvesTo     RelType = "RESOLVES_TO"
	RelAIContributes  RelType = "AI_CONTRIBUTES"
)
