package identity

import "testing"

func TestNormalizeEmail(t *testing.T) {
	cases := []struct{ in, want string }{
		{" a@B.c ", "a@b.c"},
		{"A@X.COM", "a@x.com"},
		{"", ""},
		{"  ", ""},
	}
	for _, c := range cases {
		if got := NormalizeEmail(c.in); got != c.want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		desc               string
		name, email, login string
		wantKey, wantRule  string
	}{
		{"login wins over email", "Ann", "ann@corp.example", "octo", "login:octo", "login"},
		{"noreply with numeric id", "Ann", "1234567+octo@users.noreply.github.com", "", "login:octo", "github_noreply"},
		{"noreply plain", "Ann", "octo@users.noreply.github.com", "", "login:octo", "github_noreply"},
		{"email fallback normalizes", "Bob", " Bob@Corp.Example ", "", "email:bob@corp.example", "email"},
		{"nothing usable", "Ghost", "", "", "", ""},
		{"login is trimmed", "Ann", "x@y.z", " octo ", "login:octo", "login"},
	}
	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			p := Resolve(tc.name, tc.email, tc.login)
			if p.Key != tc.wantKey || p.Rule != tc.wantRule {
				t.Errorf("Resolve(%q, %q, %q) = %q/%q, want %q/%q",
					tc.name, tc.email, tc.login, p.Key, p.Rule, tc.wantKey, tc.wantRule)
			}
		})
	}
}
