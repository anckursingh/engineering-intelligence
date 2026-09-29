package github

import (
	"fmt"
	"time"

	gh "github.com/google/go-github/v92/github"

	"github.com/anckursingh/engineering-intelligence/internal/ontology"
)

// normalize.go extracts canonical entities from go-github types.
// Extraction only — identity and relationship policy live in sync.go.
// owner/repo come from the sync context: list endpoints don't reliably
// include the full repository object.

// ts converts go-github's value-typed Timestamp (zero when absent) to a
// zero time.Time, keeping absent and present-but-epoch times distinct.
func ts(t gh.Timestamp) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return t.Time
}

func repoRef(owner, repo string) string { return fmt.Sprintf("%s/%s", owner, repo) }

func toOrganization(o *gh.Organization) ontology.Organization {
	return ontology.Organization{
		Login:       o.GetLogin(),
		Name:        o.GetName(),
		Description: o.GetDescription(),
		HTMLURL:     o.GetHTMLURL(),
		CreatedAt:   ts(o.GetCreatedAt()),
		UpdatedAt:   ts(o.GetUpdatedAt()),
	}
}

func toUser(u *gh.User) ontology.User {
	return ontology.User{
		Login:     u.GetLogin(),
		Name:      u.GetName(),
		Bio:       u.GetBio(),
		HTMLURL:   u.GetHTMLURL(),
		CreatedAt: ts(u.GetCreatedAt()),
		UpdatedAt: ts(u.GetUpdatedAt()),
	}
}

func toRepository(r *gh.Repository, fallbackOwner string) ontology.Repository {
	owner := r.GetOwner().GetLogin()
	if owner == "" {
		owner = fallbackOwner
	}
	return ontology.Repository{
		Owner:         owner,
		Name:          r.GetName(),
		Description:   r.GetDescription(),
		Language:      r.GetLanguage(),
		DefaultBranch: r.GetDefaultBranch(),
		Archived:      r.GetArchived(),
		Private:       r.GetPrivate(),
		HTMLURL:       r.GetHTMLURL(),
		CreatedAt:     ts(r.GetCreatedAt()),
		UpdatedAt:     ts(r.GetUpdatedAt()),
		PushedAt:      ts(r.GetPushedAt()),
	}
}

func toIssue(i *gh.Issue, owner, repo string) ontology.Issue {
	labels := make([]string, 0, len(i.Labels))
	for _, l := range i.Labels {
		labels = append(labels, l.GetName())
	}
	return ontology.Issue{
		Repository:  repoRef(owner, repo),
		Number:      i.GetNumber(),
		Title:       i.GetTitle(),
		Body:        i.GetBody(),
		State:       i.GetState(),
		AuthorLogin: i.GetUser().GetLogin(),
		Labels:      labels,
		CreatedAt:   ts(i.GetCreatedAt()),
		UpdatedAt:   ts(i.GetUpdatedAt()),
		ClosedAt:    ts(i.GetClosedAt()),
	}
}

func toCommit(c *gh.RepositoryCommit, owner, repo string) ontology.Commit {
	comm := c.GetCommit() // nil if GitHub could not resolve the commit object
	var name, email, message string
	var committedAt time.Time
	if comm != nil {
		name = comm.GetAuthor().GetName()
		email = comm.GetAuthor().GetEmail()
		message = comm.GetMessage()
		committedAt = ts(comm.GetAuthor().GetDate())
	}
	return ontology.Commit{
		Repository:  repoRef(owner, repo),
		SHA:         c.GetSHA(),
		Message:     message,
		AuthorName:  name,
		AuthorEmail: email,
		AuthorLogin: c.GetAuthor().GetLogin(),
		CommittedAt: committedAt,
	}
}

func toPullRequest(p *gh.PullRequest, owner, repo string) ontology.PullRequest {
	return ontology.PullRequest{
		Repository:     repoRef(owner, repo),
		Number:         p.GetNumber(),
		Title:          p.GetTitle(),
		Body:           p.GetBody(),
		State:          p.GetState(),
		Merged:         p.GetMerged(),
		AuthorLogin:    p.GetUser().GetLogin(),
		BaseRef:        p.GetBase().GetRef(),
		HeadRef:        p.GetHead().GetRef(),
		MergeCommitSHA: p.GetMergeCommitSHA(),
		CreatedAt:      ts(p.GetCreatedAt()),
		UpdatedAt:      ts(p.GetUpdatedAt()),
		MergedAt:       ts(p.GetMergedAt()),
	}
}

func toReview(r *gh.PullRequestReview, owner, repo string, prNumber int) ontology.Review {
	return ontology.Review{
		Repository:    repoRef(owner, repo),
		PRNumber:      prNumber,
		ID:            r.GetID(),
		ReviewerLogin: r.GetUser().GetLogin(),
		State:         r.GetState(),
		SubmittedAt:   ts(r.GetSubmittedAt()),
	}
}
