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
	out, err := runWithTimeout(2*time.Second, bin, "--color=never", name)
	if err != nil {
		// Any error (including non-zero exit when no page exists) is
		// treated as "no description for this command."
		return nil, ErrNotFound
	}
	return out, nil
}

// extractTldrSummary pulls the first line beginning with "> " from tldr
// output and strips the prefix. tldr pages start with:
//
//   # command-name
//
//   > Brief description.
//   > Possibly continued onto a second line.
//
// We return the first such line, with a period appended if missing.
func extractTldrSummary(text string) string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "> ") {
			continue
		}
		summary := strings.TrimPrefix(line, "> ")
		summary = strings.TrimSpace(summary)
		// Some pages put a "More information:" link in a > line; skip those.
		if strings.HasPrefix(strings.ToLower(summary), "more information") {
			continue
		}
		if summary != "" && !strings.HasSuffix(summary, ".") {
			summary += "."
		}
		return summary
	}
	return ""
}

// cleanTldrPage returns the tldr page body with light cosmetic adjustments
// suitable for embedding inside the preview pane:
//
//   - The leading `# command-name` header is dropped (the command name is
//     already shown above in the COMMAND field).
//   - The "More information:" footer line and its preceding blank line
//     are dropped (it points to a URL the user can't click in the pane).
//   - Leading and trailing blank lines are trimmed.
//
// Otherwise the page is preserved verbatim — example commands stay rendered
// as fenced backticks, descriptions stay in their `> ` form, etc.
func cleanTldrPage(text string) string {
	lines := strings.Split(text, "\n")

	// Drop the first non-blank line if it's a `# header`.
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "# ") {
			lines = append(lines[:i], lines[i+1:]...)
		}
		break
	}

	// Drop "More information:" line and any immediately preceding blanks.
	for i := len(lines) - 1; i >= 0; i-- {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(strings.ToLower(t), "> more information") {
			// Trim this line and any blank lines just before it.
			cut := i
			for cut > 0 && strings.TrimSpace(lines[cut-1]) == "" {
				cut--
			}
			lines = append(lines[:cut], lines[i+1:]...)
			break
		}
	}

	out := strings.Join(lines, "\n")
	out = strings.Trim(out, "\n")
	return out
}
