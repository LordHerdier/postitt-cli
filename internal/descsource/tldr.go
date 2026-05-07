package descsource

import (
	"bufio"
	"context"
	"os/exec"
	"strings"
	"time"
)

// Tldr looks up information by running `tldr <name>`. Provides both a one-
// line summary (Lookup) and the full page body (Page) — the latter is what
// the preview pane shows so the user can see actual usage examples.
type Tldr struct {
	// Bin overrides the binary name; defaults to "tldr".
	Bin string
}

func (Tldr) Name() string { return "tldr" }

func (t *Tldr) Lookup(ctx context.Context, name string) (string, error) {
	out, err := t.fetch(name)
	if err != nil {
		return "", err
	}
	desc := extractTldrSummary(string(out))
	if desc == "" {
		return "", ErrNotFound
	}
	return desc, nil
}

// Page returns the full tldr page body for the given command name, with
// minor cosmetic cleanup applied (the leading `# command-name` header is
// stripped, since the preview already shows the command name elsewhere).
// Returns ErrNotFound if no page exists.
func (t *Tldr) Page(ctx context.Context, name string) (string, error) {
	out, err := t.fetch(name)
	if err != nil {
		return "", err
	}
	body := cleanTldrPage(string(out))
	if body == "" {
		return "", ErrNotFound
	}
	return body, nil
}

// fetch runs tldr and returns its raw stdout, or ErrNotFound on any failure.
// We deliberately collapse all error cases into ErrNotFound — the preview
// pane should never block or surface tldr-internal errors.
//
// No --color flag is passed: when stdout is a pipe the tldr client strips
// ANSI codes by default. (The -c/--color flag on tldr 3.x *enables* color,
// the opposite of what "--color=never" implies, so we omit it entirely.)
func (t *Tldr) fetch(name string) ([]byte, error) {
	if name == "" {
		return nil, ErrNotFound
	}
	bin := t.Bin
	if bin == "" {
		bin = "tldr"
	}
	if _, err := exec.LookPath(bin); err != nil {
		return nil, ErrNotFound
	}
	out, err := runWithTimeout(2*time.Second, bin, name)
	if err != nil {
		// Any error (including non-zero exit when no page exists) is
		// treated as "no description for this command."
		return nil, ErrNotFound
	}
	return out, nil
}

// extractTldrSummary pulls the brief description from tldr output.
//
// tldr client 3.x (spec 2.x) produces plain indented text:
//
//	  command-name
//	  Description sentence.  See also: `x`.  More information: URL.
//	  - Example:    command
//
// We skip the first non-blank line (the command name), then return the first
// sentence of the next non-blank line, stripping trailing "  See also: ..." and
// "  More information: ..." clauses that are separated by double-spaces.
func extractTldrSummary(text string) string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	first := true
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if first {
			first = false
			continue // command name header — skip it
		}
		return tldrFirstSentence(line)
	}
	return ""
}

// tldrFirstSentence returns the first sentence of a tldr description line by
// cutting at the first double-space boundary (which separates clauses like
// "See also: ..." and "More information: ...").
func tldrFirstSentence(line string) string {
	if i := strings.Index(line, "  "); i > 0 {
		line = line[:i]
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	if !strings.HasSuffix(line, ".") {
		line += "."
	}
	return line
}

// cleanTldrPage returns the tldr page body adjusted for the preview pane.
//
// tldr client 3.x (spec 2.x) format:
//
//	  command-name
//	  Description.  See also: `x`.  More information: URL.
//	  - Example:    command
//
// Adjustments made:
//   - The first non-blank line (the command name) is dropped — it's already
//     shown in the COMMAND field above.
//   - On the description line, "  See also: ..." and "  More information: ..."
//     trailing clauses are stripped (URLs aren't clickable in the pane).
//   - Leading and trailing blank lines are trimmed.
func cleanTldrPage(text string) string {
	lines := strings.Split(text, "\n")

	// Drop the first non-blank line (command name header).
	for i, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		lines = append(lines[:i], lines[i+1:]...)
		break
	}

	// On the description line (now the first non-blank line), strip the
	// "  See also: ..." and "  More information: ..." trailing clauses.
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if idx := strings.Index(t, "  More information:"); idx >= 0 {
			t = strings.TrimSpace(t[:idx])
		}
		if idx := strings.Index(t, "  See also:"); idx >= 0 {
			t = strings.TrimSpace(t[:idx])
		}
		// Re-apply the original leading whitespace.
		indent := len(l) - len(strings.TrimLeft(l, " \t"))
		lines[i] = l[:indent] + t
		break
	}

	out := strings.Join(lines, "\n")
	out = strings.Trim(out, "\n")
	return out
}
