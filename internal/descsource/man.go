package descsource

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// Man looks up a description from `man -f <name>` (whatis), which returns
// a one-line summary if the man page exists. Falls back to the NAME section
// of the full man page if whatis isn't available.
type Man struct{}

func (Man) Name() string { return "man" }

func (m *Man) Lookup(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", ErrNotFound
	}
	if _, err := exec.LookPath("man"); err != nil {
		return "", ErrNotFound
	}

	// `man -f` is whatis: outputs e.g. "git (1) - the stupid content tracker"
	out, err := runWithTimeout(2*time.Second, "man", "-f", name)
	if err == nil {
		desc := extractWhatisSummary(string(out))
		if desc != "" {
			return desc, nil
		}
	} else {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			// Non-exit errors (timeout, missing binary) propagate as not-found.
			return "", ErrNotFound
		}
	}

	// Fallback: render the full page and grab the first line of the NAME
	// section. This is slower but works on systems where `whatis` isn't
	// indexed. We cap the timeout shorter here to avoid stalling on huge
	// pages.
	out, err = runWithTimeout(3*time.Second, "man", name)
	if err != nil {
		return "", ErrNotFound
	}
	if desc := extractManNameSection(string(out)); desc != "" {
		return desc, nil
	}
	return "", ErrNotFound
}

// extractWhatisSummary parses output like:
//   git (1) - the stupid content tracker
// returning "The stupid content tracker." (capitalized, period-terminated).
func extractWhatisSummary(text string) string {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		idx := strings.Index(line, " - ")
		if idx < 0 {
			continue
		}
		desc := strings.TrimSpace(line[idx+3:])
		if desc == "" {
			continue
		}
		// Capitalize first letter, ensure trailing period.
		desc = strings.ToUpper(desc[:1]) + desc[1:]
		if !strings.HasSuffix(desc, ".") {
			desc += "."
		}
		return desc
	}
	return ""
}

// extractManNameSection finds the line under "NAME" in a rendered man page
// and returns the description portion. Format:
//
//   NAME
//          git - the stupid content tracker
//
// We look for the first non-blank line after a NAME header.
func extractManNameSection(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) != "NAME" {
			continue
		}
		for j := i + 1; j < len(lines); j++ {
			body := strings.TrimSpace(lines[j])
			if body == "" {
				continue
			}
			idx := strings.Index(body, " - ")
			if idx < 0 {
				return ""
			}
			desc := strings.TrimSpace(body[idx+3:])
			if desc == "" {
				return ""
			}
			desc = strings.ToUpper(desc[:1]) + desc[1:]
			if !strings.HasSuffix(desc, ".") {
				desc += "."
			}
			return desc
		}
	}
	return ""
}

// Page returns a concise excerpt from the man page suitable for embedding
// in the preview pane: the NAME section plus the SYNOPSIS section. The full
// DESCRIPTION is left out — it's typically pages long, and the preview pane
// is small. Users who want the full page can press the "open man" keybind
// in the picker to get it in pager view.
//
// Returns ErrNotFound if no man page exists for the command.
func (m *Man) Page(ctx context.Context, name string) (string, error) {
	if name == "" {
		return "", ErrNotFound
	}
	if _, err := exec.LookPath("man"); err != nil {
		return "", ErrNotFound
	}
	out, err := runWithTimeout(3*time.Second, "man", name)
	if err != nil {
		return "", ErrNotFound
	}
	excerpt := extractManExcerpt(string(out))
	if excerpt == "" {
		return "", ErrNotFound
	}
	return excerpt, nil
}

// extractManExcerpt pulls the NAME and SYNOPSIS sections from a rendered
// man page. We stop at the first known section heading after SYNOPSIS,
// which is typically "DESCRIPTION" — that's the bit we don't want
// because it can be huge.
//
// man pages from `groff` use uppercase section headers in column 0:
//
//   NAME
//          foo - frobnicate the bar
//
//   SYNOPSIS
//          foo [OPTIONS] FILE
//
//   DESCRIPTION
//          ...long prose...
//
// We rely on that convention. If a page doesn't follow it (rare), we return
// whatever we managed to extract before bailing.
func extractManExcerpt(text string) string {
	lines := strings.Split(text, "\n")
	var out []string
	inSection := false
	for _, line := range lines {
		t := strings.TrimRight(line, " \t")
		// Section headers are uppercase tokens at column 0. We treat
		// NAME and SYNOPSIS as in-bounds and anything else as the
		// boundary that ends our excerpt.
		if isManSectionHeader(t) {
			switch t {
			case "NAME", "SYNOPSIS":
				inSection = true
				out = append(out, t)
				continue
			default:
				// Hit DESCRIPTION (or whatever); we're done.
				return strings.TrimRight(strings.Join(out, "\n"), "\n")
			}
		}
		if inSection {
			out = append(out, t)
		}
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

// isManSectionHeader returns true if the line looks like a top-level man
// page section header: all uppercase letters (allowing spaces), at the
// start of a line, no leading whitespace.
func isManSectionHeader(line string) bool {
	if line == "" || line[0] == ' ' || line[0] == '\t' {
		return false
	}
	hasUpper := false
	for _, r := range line {
		switch {
		case r >= 'A' && r <= 'Z':
			hasUpper = true
		case r == ' ':
			// permitted (e.g. "EXIT STATUS")
		default:
			return false
		}
	}
	return hasUpper
}
