// Package identity maps source identities to canonical engineers.
// v1 is GitHub-only and purely local: rules are pure functions, and every
// decision is explainable (AC-ID-002) via identity_key + identity_rule.
package identity

import (
	"regexp"
	"strings"
)

// GitHub's noreply addresses encode the account login.
var noreplyRe = regexp.MustCompile(`^(?:\d+\+)?(.+)@users\.noreply\.github\.com$`)

// Person is a resolved source identity.
type Person struct {
	Name  string
	Email string // normalized; may be empty
	Login string // "" if the source identity is not linked to a GitHub user
	Key   string // dedup key: "login:<lower>" or "email:<normalized>"; "" when unusable
	Rule  string // "login" | "github_noreply" | "email"; "" when unusable
}

// NormalizeEmail lowercases and trims whitespace.
// ponytail: plus-addressing is not stripped — rare in work contexts, and
// stripping it risks merging distinct inboxes.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// Resolve maps a commit author (or issue/PR author) to a Person, in priority
// order: linked login, noreply-address login, email, unusable.
func Resolve(name, email, login string) Person {
	login = strings.TrimSpace(login)
	if login != "" {
		return Person{
			Name:  strings.TrimSpace(name),
			Email: NormalizeEmail(email),
			Login: login,
			Key:   "login:" + strings.ToLower(login),
			Rule:  "login",
		}
	}
	if m := noreplyRe.FindStringSubmatch(strings.TrimSpace(email)); m != nil {
		login = m[1]
		return Person{
			Name:  strings.TrimSpace(name),
			Email: NormalizeEmail(email),
			Login: login,
			Key:   "login:" + strings.ToLower(login),
			Rule:  "github_noreply",
		}
	}
	if norm := NormalizeEmail(email); norm != "" {
		return Person{
			Name:  strings.TrimSpace(name),
			Email: norm,
			Key:   "email:" + norm,
			Rule:  "email",
		}
	}
	// ponytail: ghost authors get no engineer and no AUTHORED edge.
	return Person{Name: strings.TrimSpace(name)}
}
