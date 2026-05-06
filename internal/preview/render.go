// Package preview renders the right-hand pane shown by fzf when a command is
// highlighted in the picker. fzf calls `cheatshh _preview <id>` once per
// highlight change, which executes Render with a freshly opened store.
package preview

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/charlotte/cheatshh/internal/descsource"
	"github.com/charlotte/cheatshh/internal/store"
)

// ANSI color codes. Kept minimal and standard so the output looks reasonable
// in any terminal that supports basic color. We don't try to detect terminal
// capability — fzf is already requiring a real TTY at this point.
const (
	bold      = "\033[1m"
	dim       = "\033[2m"
	cyan      = "\033[36m"
	yellow    = "\033[33m"
	green     = "\033[32m"
	reset     = "\033[0m"
	separator = "─"
)

// Render writes the preview for a single command to w. Layout:
//
//   COMMAND (3 lines):
//     for f in *.log; do
//       gzip "$f"
//     done
//
//   TAGS:    git, stash
//   USED:    23 times — last 2 days ago
//   PINNED:  yes
//
//   ── your description ──
//   Restore the most recently stashed changes...
//
//   ── tldr (gzip) ──
//   ...page contents...
//
// The user's description and the external reference (tldr/man page body)
// are both shown when present. When the user hasn't written a description,
// the reference section becomes the primary content rather than a single
// "(no description)" line.
func Render(w io.Writer, s *store.Store, id int64, includeReference bool) error {
	c, err := s.Get(id)
	if err != nil {
		// Render an error message rather than failing — fzf will display
		// whatever we write to stdout regardless of exit code.
		fmt.Fprintf(w, "%scould not load command #%d: %v%s\n", yellow, id, err, reset)
		return nil
	}

	writeCommand(w, c.Command)
	fmt.Fprintln(w)
	writeMetadata(w, c)

	// User description: only render the section if there's something to show.
	// Skipping the "(no description)" placeholder avoids visual noise when
	// the user just hasn't written one — the reference content fills the gap.
	if c.Description != "" {
		fmt.Fprintln(w)
		writeDescription(w, c.Description)
	}

	if includeReference {
		fmt.Fprintln(w)
		writeReference(w, c.Command)
	}
	return nil
}

// writeCommand renders the command itself, prominent at the top. Multi-line
// commands get a "(N lines)" header and each line is indented for visual
// separation from labels.
func writeCommand(w io.Writer, cmd string) {
	if !strings.Contains(cmd, "\n") {
		fmt.Fprintf(w, "%s%sCOMMAND:%s %s\n", bold, cyan, reset, cmd)
		return
	}
	lines := strings.Split(cmd, "\n")
	fmt.Fprintf(w, "%s%sCOMMAND%s %s(%d lines):%s\n",
		bold, cyan, reset, dim, len(lines), reset)
	for _, l := range lines {
		fmt.Fprintf(w, "  %s\n", l)
	}
}

// writeMetadata renders the tags/usage/bookmark trio of summary fields.
func writeMetadata(w io.Writer, c *store.Command) {
	tagStr := dim + "(none)" + reset
	if len(c.Tags) > 0 {
		tagStr = strings.Join(c.Tags, ", ")
	}
	fmt.Fprintf(w, "%sTAGS:%s    %s\n", bold, reset, tagStr)

	useStr := fmt.Sprintf("%d times", c.UseCount)
	if c.UseCount == 1 {
		useStr = "1 time"
	}
	if c.UseCount == 0 {
		useStr = dim + "never used" + reset
	} else if c.LastUsed != nil {
		useStr += " — last " + humanizeAgo(*c.LastUsed)
	}
	fmt.Fprintf(w, "%sUSED:%s    %s\n", bold, reset, useStr)

	if c.Bookmarked {
		fmt.Fprintf(w, "%sPINNED:%s  %syes%s\n", bold, reset, green, reset)
	}
}

// writeDescription prints the description block. We assume the caller has
// already determined the description is non-empty (Render skips this section
// entirely when there's nothing to show).
func writeDescription(w io.Writer, desc string) {
	fmt.Fprintf(w, "%s── description ──%s\n", dim, reset)
	fmt.Fprintln(w, desc)
}

// writeReference attempts to fetch and display reference material for the
// command from external sources, in this order:
//
//   1. tldr — full page body (examples, common usage)
//   2. man  — NAME + SYNOPSIS excerpt as a fallback
//
// Output is silent on lookup failure: if neither source has anything to
// say, the section is omitted. This is what we want — a "no reference"
// placeholder would just be noise.
func writeReference(w io.Writer, cmd string) {
	name := firstProgramWord(cmd)
	if name == "" {
		return
	}

	// tldr first; longer timeout than the summary case because we're
	// rendering the whole page and it's worth the extra ~100ms.
	ctx, cancel := context.WithTimeout(context.Background(), 2500*time.Millisecond)
	defer cancel()

	tldr := &descsource.Tldr{}
	if page, err := tldr.Page(ctx, name); err == nil && page != "" {
		fmt.Fprintf(w, "%s── tldr (%s) ──%s\n", dim, name, reset)
		fmt.Fprintln(w, page)
		return
	}

	// man fallback. We give man a fresh context with its own budget, since
	// rendering a man page is the slow case (groff + col).
	ctx2, cancel2 := context.WithTimeout(context.Background(), 3500*time.Millisecond)
	defer cancel2()

	man := &descsource.Man{}
	if page, err := man.Page(ctx2, name); err == nil && page != "" {
		fmt.Fprintf(w, "%s── man (%s) ──%s\n", dim, name, reset)
		fmt.Fprintln(w, page)
		fmt.Fprintf(w, "\n%s(press Alt-M for the full man page)%s\n", dim, reset)
		return
	}
}

// firstProgramWord pulls the program name out of a command line, mirroring
// the helper in descsource. Duplicated to avoid an internal import cycle
// in the simple case.
func firstProgramWord(cmdline string) string {
	for _, f := range strings.Fields(cmdline) {
		if strings.Contains(f, "=") && !strings.ContainsAny(f, "/.") {
			continue
		}
		if f == "sudo" || f == "doas" || f == "env" {
			continue
		}
		if strings.HasPrefix(f, "-") {
			continue
		}
		if strings.ContainsAny(f, "/") {
			return ""
		}
		return f
	}
	return ""
}

// humanizeAgo returns a short relative-time phrase: "5 minutes ago",
// "2 days ago", "just now", etc. Cheap and good-enough; not trying to
// compete with a humanize library.
func humanizeAgo(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	case d < 30*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	case d < 365*24*time.Hour:
		months := int(d.Hours() / 24 / 30)
		if months == 1 {
			return "1 month ago"
		}
		return fmt.Sprintf("%d months ago", months)
	default:
		years := int(d.Hours() / 24 / 365)
		if years == 1 {
			return "1 year ago"
		}
		return fmt.Sprintf("%d years ago", years)
	}
}
