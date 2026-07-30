package update

import (
	"testing"

	"github.com/bestruirui/octopus/internal/conf"
)

func TestRepoSlugDerivesFromConfRepo(t *testing.T) {
	original := conf.Repo
	t.Cleanup(func() { conf.Repo = original })

	cases := []struct {
		name string
		repo string
		want string
	}{
		{name: "https url", repo: "https://github.com/mingtian886/octopus", want: "mingtian886/octopus"},
		{name: "trailing slash", repo: "https://github.com/mingtian886/octopus/", want: "mingtian886/octopus"},
		{name: "surrounding spaces", repo: "  https://github.com/owner/repo  ", want: "owner/repo"},
		{name: "http url", repo: "http://github.com/owner/repo", want: "owner/repo"},
		{name: "already slug", repo: "owner/repo", want: "owner/repo"},
		{name: "empty falls back", repo: "", want: defaultRepoSlug},
		{name: "non github url falls back", repo: "https://example.com/owner/repo", want: defaultRepoSlug},
		{name: "too many segments falls back", repo: "https://github.com/owner/repo/extra", want: defaultRepoSlug},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conf.Repo = tc.repo
			if got := repoSlug(); got != tc.want {
				t.Fatalf("repoSlug() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestUpdateEndpointsPointAtConfiguredRepo(t *testing.T) {
	if want := "https://api.github.com/repos/" + repoSlug() + "/releases/latest"; updateApiUrl != want {
		t.Fatalf("updateApiUrl = %q, want %q", updateApiUrl, want)
	}
}
