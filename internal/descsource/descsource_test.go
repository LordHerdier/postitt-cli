package descsource

import (
	"strings"
	"testing"
)

func TestCleanTldrPage(t *testing.T) {
	in := `# git stash

> Stash local Git changes in a temporary area.
> More information: <https://git-scm.com/docs/git-stash>.

- Stash current changes, except new (untracked) files:

` + "`git stash [push -m {{optional_stash_message}}]`" + `

- Stash current changes, including new (untracked) files:

` + "`git stash -u`" + `

- Show the changes as a diff:

` + "`git stash show -p`" + `
`
	got := cleanTldrPage(in)

	// The leading `# git stash` should be gone.
	if strings.Contains(got, "# git stash") {
		t.Errorf("expected `# git stash` header to be stripped, got:\n%s", got)
	}
	// The "More information:" line should be gone.
	if strings.Contains(got, "More information") {
		t.Errorf("expected More information line to be stripped, got:\n%s", got)
	}
	// The summary `> Stash...` line should still be there.
	if !strings.Contains(got, "Stash local Git changes") {
		t.Errorf("expected summary to be preserved, got:\n%s", got)
	}
	// Examples should still be there.
	if !strings.Contains(got, "git stash -u") {
		t.Errorf("expected examples to be preserved, got:\n%s", got)
	}
	// No leading or trailing blank lines.
	if strings.HasPrefix(got, "\n") {
		t.Errorf("got leading blank line:\n%q", got)
	}
	if strings.HasSuffix(got, "\n\n") {
		t.Errorf("got trailing blank lines:\n%q", got)
	}
}

func TestCleanTldrPage_NoHeaderAndNoFooter(t *testing.T) {
	// Some pages might not have either; we should handle that gracefully.
	in := `> Some description.

- Some example:

` + "`some-cmd`"
	got := cleanTldrPage(in)
	if !strings.Contains(got, "Some description") {
		t.Errorf("description lost: %s", got)
	}
	if !strings.Contains(got, "some-cmd") {
		t.Errorf("example lost: %s", got)
	}
}

func TestExtractManExcerpt(t *testing.T) {
	// A simulated rendered man page. Real man output uses backspace-overstrike
	// for bold/italic and is post-processed with `col -b`, but our extractor
	// works on the post-processed plain text.
	in := `GIT(1)                          Git Manual                          GIT(1)



NAME
       git - the stupid content tracker

SYNOPSIS
       git [--version] [--help] [-C <path>] [-c <name>=<value>]
           [--exec-path[=<path>]] [--html-path] [--man-path] [--info-path]
           [-p|--paginate|-P|--no-pager] [--no-replace-objects] [--bare]
           [--git-dir=<path>] [--work-tree=<path>] [--namespace=<name>]
           <command> [<args>]

DESCRIPTION
       Git is a fast, scalable, distributed revision control system with an
       unusually rich command set that provides both high-level operations
       and full access to internals.

OPTIONS
       --version
           Prints the Git suite version that the git program came from.
`
	got := extractManExcerpt(in)
	if !strings.Contains(got, "NAME") {
		t.Errorf("NAME header missing: %s", got)
	}
	if !strings.Contains(got, "the stupid content tracker") {
		t.Errorf("NAME body missing: %s", got)
	}
	if !strings.Contains(got, "SYNOPSIS") {
		t.Errorf("SYNOPSIS header missing: %s", got)
	}
	if !strings.Contains(got, "[--version]") {
		t.Errorf("SYNOPSIS body missing: %s", got)
	}
	// DESCRIPTION should NOT be included.
	if strings.Contains(got, "DESCRIPTION") {
		t.Errorf("DESCRIPTION should not be included, got:\n%s", got)
	}
	if strings.Contains(got, "fast, scalable") {
		t.Errorf("DESCRIPTION body leaked into excerpt: %s", got)
	}
}

func TestIsManSectionHeader(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"NAME", true},
		{"SYNOPSIS", true},
		{"EXIT STATUS", true},
		{"DESCRIPTION", true},
		{"   indented", false},      // leading whitespace
		{"", false},                 // empty
		{"name", false},             // lowercase
		{"Mixed Case", false},       // mixed
		{"NAME-WITH-DASH", false},   // non-letter
		{"git", false},              // not a header
	}
	for _, tc := range cases {
		got := isManSectionHeader(tc.line)
		if got != tc.want {
			t.Errorf("isManSectionHeader(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}
