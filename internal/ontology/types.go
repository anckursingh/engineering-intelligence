// Package ontology holds the canonical engineering entities. It belongs to
// Engineering Intelligence, not AIKOQL: these types are converted to generic
// knowledge objects before persistence, so the store never sees them.
package ontology

import "time"

type Organization struct {
	Login       string    `json:"login"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	HTMLURL     string    `json:"html_url,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// User is a personal GitHub account that owns repositories — the sync owner
// when the account is not an organization. Distinct from Engineer: an
// Engineer is a person derived from activity, a User is the account itself.
type User struct {
	Login     string    `json:"login"`
	Name      string    `json:"name,omitempty"`
	Bio       string    `json:"bio,omitempty"`
	HTMLURL   string    `json:"html_url,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Engineer struct {
	Name         string `json:"name"`
	Email        string `json:"email,omitempty"` // normalized; often empty (GitHub hides emails)
	Login        string `json:"login,omitempty"` // "" if unlinked commit author
	AvatarURL    string `json:"avatar_url,omitempty"`
	IdentityKey  string `json:"identity_key"`  // dedup key; "" only for ghost authors
	IdentityRule string `json:"identity_rule"` // "login" | "github_noreply" | "email"
}

type Repository struct {
	Owner         string    `json:"owner"`
	Name          string    `json:"name"`
	Description   string    `json:"description,omitempty"`
	Language      string    `json:"language,omitempty"`
	DefaultBranch string    `json:"default_branch"`
	Archived      bool      `json:"archived"`
	Private       bool      `json:"private"`
	HTMLURL       string    `json:"html_url,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
	PushedAt      time.Time `json:"pushed_at"`
}

type Issue struct {
	Repository  string    `json:"repository"` // owner/name
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	Body        string    `json:"body,omitempty"`
	State       string    `json:"state"`
	AuthorLogin string    `json:"author_login,omitempty"`
	Labels      []string  `json:"labels,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	ClosedAt    time.Time `json:"closed_at,omitempty"`
}

type Commit struct {
	Repository  string    `json:"repository"`
	SHA         string    `json:"sha"`
	Message     string    `json:"message"`
	AuthorName  string    `json:"author_name"`
	AuthorEmail string    `json:"author_email,omitempty"`
	AuthorLogin string    `json:"author_login,omitempty"` // "" if GitHub could not link the author
	CommittedAt time.Time `json:"committed_at"`
}

type PullRequest struct {
	Repository         string    `json:"repository"`
	Number             int       `json:"number"`
	Title              string    `json:"title"`
	Body               string    `json:"body,omitempty"`
	State              string    `json:"state"`
	Merged             bool      `json:"merged"`
	AuthorLogin        string    `json:"author_login,omitempty"`
	BaseRef            string    `json:"base_ref"`
	HeadRef            string    `json:"head_ref"`
	MergeCommitSHA     string    `json:"merge_commit_sha,omitempty"`
	Additions          int       `json:"additions,omitempty"`
	Deletions          int       `json:"deletions,omitempty"`
	Labels             []string  `json:"labels,omitempty"`
	Draft              bool      `json:"draft"`
	MergeMethod        string    `json:"merge_method,omitempty"`        // merge|squash|rebase; single-PR GET only
	RequestedReviewers []string  `json:"requested_reviewers,omitempty"` // current request snapshot, not history
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	MergedAt           time.Time `json:"merged_at,omitempty"`
}

type Review struct {
	Repository    string `json:"repository"`
	PRNumber      int    `json:"pr_number"`
	ID            int64  `json:"id"`
	ReviewerLogin string `json:"reviewer_login"`
	// State carries GitHub's review states verbatim: APPROVED,
	// CHANGES_REQUESTED, COMMENTED (submitted without a verdict), DISMISSED.
	// "requested" is not a Review state — GitHub keeps no request history;
	// the current request set lives on PullRequest.RequestedReviewers and the
	// REQUESTED_REVIEW edge.
	State       string    `json:"state"`
	SubmittedAt time.Time `json:"submitted_at"`
}

// Build is defined now so AC-KG-001 can complete next increment; the fetch
// needs the Actions/Checks API family and lands with it.
type Build struct {
	Repository  string    `json:"repository"`
	ID          int64     `json:"id"` // workflow run id
	Name        string    `json:"name"`
	HeadSHA     string    `json:"head_sha"`
	Conclusion  string    `json:"conclusion"`
	Status      string    `json:"status"`
	StartedAt   time.Time `json:"started_at"`
	CompletedAt time.Time `json:"completed_at"`
	HTMLURL     string    `json:"html_url,omitempty"`
}

// Deployment is a GitHub deployment record. GitHub's deployments API carries
// no html_url; provenance uses the API URL. State/status deliberately absent:
// the deployments list has no terminal state (statuses are a separate
// endpoint) and nothing consumes it yet.
type Deployment struct {
	Repository  string    `json:"repository"`
	ID          int64     `json:"id"`
	Environment string    `json:"environment"`
	SHA         string    `json:"sha"` // the deployed commit
	Ref         string    `json:"ref,omitempty"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Service is the product-level entity a deployment affects. In the
// GitHub-only world the service IS the repo-scoped environment name (one
// repo, many environments = many services); non-GitHub service registries
// join through the same type later.
type Service struct {
	Repository string `json:"repository"`
	Name       string `json:"name"` // the environment name
}
