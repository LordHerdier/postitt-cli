package main

import (
	"strings"
	"testing"
)

// Tests for parseEditBuffer, exercising both single-line and multi-line
// values across all fields. We feed the buffer through the same path the
// editor flow uses.

func TestParseEditBuffer_SingleLine(t *testing.T) {
	in := `# header comment
command = "git stash pop"
description = "restore most recent stash"
tags = ["git", "stash"]
bookmarked = false
`
	got, err := parseEditBuffer(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Command != "git stash pop" {
		t.Errorf("Command = %q, want %q", got.Command, "git stash pop")
	}
	if got.Description != "restore most recent stash" {
		t.Errorf("Description = %q", got.Description)
	}
	if !equalStringSlice(got.Tags, []string{"git", "stash"}) {
		t.Errorf("Tags = %v", got.Tags)
	}
	if got.Bookmarked {
		t.Errorf("Bookmarked = true, want false")
	}
}

func TestParseEditBuffer_MultiLineCommand(t *testing.T) {
	in := `command = """
for f in *.log; do
  gzip "$f"
done
"""
description = "compress all log files"
tags = []
bookmarked = true
`
	got, err := parseEditBuffer(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "for f in *.log; do\n  gzip \"$f\"\ndone"
	if got.Command != want {
		t.Errorf("Command = %q, want %q", got.Command, want)
	}
	if !got.Bookmarked {
		t.Errorf("Bookmarked = false, want true")
	}
}

func TestParseEditBuffer_MultiLineDescription(t *testing.T) {
	in := `command = "echo hi"
description = """
line one
line two
line three
"""
tags = ["a"]
bookmarked = false
`
	got, err := parseEditBuffer(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "line one\nline two\nline three"
	if got.Description != want {
		t.Errorf("Description = %q, want %q", got.Description, want)
	}
}

func TestParseEditBuffer_EmbeddedQuotesInMultiLine(t *testing.T) {
	// Triple-quoted blocks preserve content verbatim — single and double
	// quotes inside should pass through.
	in := `command = """
echo "hello 'world'"
echo 'still "fine"'
"""
description = ""
tags = []
bookmarked = false
`
	got, err := parseEditBuffer(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := `echo "hello 'world'"` + "\n" + `echo 'still "fine"'`
	if got.Command != want {
		t.Errorf("Command = %q, want %q", got.Command, want)
	}
}

func TestParseEditBuffer_UnterminatedTripleQuote(t *testing.T) {
	in := `command = """
this never closes
description = "x"
`
	if _, err := parseEditBuffer(in); err == nil {
		t.Fatal("expected error for unterminated triple-quoted string, got nil")
	} else if !strings.Contains(err.Error(), "unterminated") {
		t.Errorf("error = %v, want one mentioning 'unterminated'", err)
	}
}

func TestParseEditBuffer_EmptyCommandIsError(t *testing.T) {
	in := `command = ""
description = "x"
tags = []
bookmarked = false
`
	if _, err := parseEditBuffer(in); err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
}

func TestParseEditBuffer_TripleQuoteSameLineClose(t *testing.T) {
	// The "block closes on the same line" case: `key = """foo"""`. Unusual
	// but we handle it.
	in := `command = """git status"""
description = ""
tags = []
bookmarked = false
`
	got, err := parseEditBuffer(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Command != "git status" {
		t.Errorf("Command = %q, want %q", got.Command, "git status")
	}
}

func TestParseEditBuffer_UnknownKeyIgnored(t *testing.T) {
	// Forward-compat: future fields shouldn't break old parsers.
	in := `command = "x"
description = ""
tags = []
bookmarked = false
future_field = "ignored"
`
	got, err := parseEditBuffer(in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Command != "x" {
		t.Errorf("Command = %q", got.Command)
	}
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
