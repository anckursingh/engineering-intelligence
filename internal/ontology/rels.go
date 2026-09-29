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
//	PART_OF         source Commit → target PullRequest
//	                meaning: the commit belongs to the PR's branch — ingested
//	                from the PR's own commit list, which the default-branch
//	                walk never sees
//	                inverse: PR containing the commit (inbound traversal)
//	                cardinality: N:M (a PR rebases and force-pushes; the
//	                superseded commits keep their edges)
//	                test: TestSyncPRCommits
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
//	REQUESTED_REVIEW source PullRequest → target Engineer
//	                meaning: the PR's current requested-reviewers snapshot —
//	                GitHub keeps no request history or timestamps, so the
//	                edge is the request set at sync time, not a lifecycle
//	                event (a Review with State "DISMISSED" marks the end)
//	                inverse: requested review (inbound traversal)
//	                cardinality: N:M
//	                test: TestReviewLifecycleStates
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
//	CONTAINS_BUILD  source Repository → target Build
//	                meaning: the workflow run happened in the repo
//	                inverse: runs in repo (inbound traversal)
//	                cardinality: 1:N
//	                test: TestSyncCI + TestPopulationFromScope
//
//	HAS_BUILD       source PullRequest → target Build
//	                meaning: the run covers the PR — the edge never claims an
//	                outcome; a run's success or failure lives on the Build's
//	                conclusion, not on the edge
//	                inverse: builds covering the PR (inbound traversal)
//	                cardinality: N:M
//	                test: TestSyncCI
//
//	PRODUCED        source Build → target Deployment
//	                meaning: the workflow run's head commit is the deployed
//	                sha — the strongest link GitHub exposes; deployments
//	                created by Actions carry no run id, so the edge is sha
//	                association, never a causation claim
//	                inverse: deployments from this build (inbound traversal)
//	                cardinality: N:M (re-runs at the same sha)
//	                test: TestSyncDeployments
//
//	AFFECTS         source Deployment → target Service
//	                meaning: the deployment touched the service
//	                inverse: affected by (inbound traversal)
//	                cardinality: N:M
//	                test: TestSyncDeployments
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
	RelBelongsTo       RelType = "BELONGS_TO"
	RelAuthored        RelType = "AUTHORED"
	RelImplements      RelType = "IMPLEMENTS"
	RelPartOf          RelType = "PART_OF" // Commit → PullRequest; from the PR's commit list
	RelTargets         RelType = "TARGETS"
	RelReviewedBy      RelType = "REVIEWED_BY"
	RelRequestedReview RelType = "REQUESTED_REVIEW" // PullRequest → Engineer; current request snapshot
	RelMergedAs        RelType = "MERGED_AS"
	RelContainsReview  RelType = "CONTAINS_REVIEW"
	RelContainsBuild   RelType = "CONTAINS_BUILD"
	RelHasBuild        RelType = "HAS_BUILD" // PullRequest → Build; outcome lives on the Build
	RelProduced        RelType = "PRODUCED"  // Build → Deployment; sha association, never causation
	RelAffects         RelType = "AFFECTS"   // Deployment → Service
	RelResolvesTo      RelType = "RESOLVES_TO"
	RelAIContributes   RelType = "AI_CONTRIBUTES"
)
