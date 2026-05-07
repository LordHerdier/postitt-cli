// Package picker integrates fzf as the interactive selection UI. It does not
// implement fuzzy matching itself; it shells out to fzf, passes a rendered
// list on stdin, and parses the selection (plus any "expect" key) on stdout.
package picker

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/LordHerdier/postitt-cli/internal/pickerstate"
	"github.com/LordHerdier/postitt-cli/internal/store"
)

// Action is the post-selection action determined by which key the user
// pressed to confirm the picker.
type Action int

const (
	// ActionNone means the user dismissed the picker without selecting.
	ActionNone Action = iota
	// ActionCopy: Enter — copy command to clipboard.
	ActionCopy
	// ActionExec: Ctrl-E — run command via $SHELL -c.
	ActionExec
	// ActionPrint: Ctrl-P — write command to stdout.
	ActionPrint
)

// Result is what the picker returns to the caller after the user makes
// a terminal choice (Enter, Ctrl-E, Ctrl-P). In-place actions like bookmark
// and delete are handled internally by fzf bindings and never surface here.
type Result struct {
	Action  Action
	Command *store.Command
}

// ErrNoFzf is returned when the fzf binary is not on $PATH.
var ErrNoFzf = errors.New("fzf not found on $PATH (install fzf to use the picker)")

// fieldSep is the separator used in the rendered picker line. We use a tab
// because it's never present in normal command text and fzf's --delimiter
// handles it cleanly.
const fieldSep = "\t"

// Run opens fzf with the given list of commands. The returned Result describes
// the action chosen; selfExec is the path to the postitt binary itself, used
// for the preview subprocess and for in-place bind actions (bookmark, delete).
func Run(cmds []*store.Command, selfExec string) (*Result, error) {
	if _, err := exec.LookPath("fzf"); err != nil {
		return nil, ErrNoFzf
	}
	if len(cmds) == 0 {
		return &Result{Action: ActionNone}, nil
	}

	// Create a per-process session file used by --bind callbacks to track
	// the active tag filter. Removed on exit regardless of how the picker
	// terminates.
	sessionPath, err := pickerstate.New()
	if err != nil {
		return nil, err
	}
	defer pickerstate.Cleanup(sessionPath)

	input := renderList(cmds)

	args := buildFzfArgs(selfExec)
	cmd := exec.Command("fzf", args...)
	cmd.Stdin = strings.NewReader(input)
	cmd.Stderr = os.Stderr // fzf draws its UI on stderr; pass it through.
	// Children spawned by fzf bindings inherit our environment plus the
	// session pointer. We don't use the package-level helper (Path()) here
	// because we want to be explicit about what we pass to the child.
	cmd.Env = append(os.Environ(), pickerstate.EnvVar()+"="+sessionPath)

	var out bytes.Buffer
	cmd.Stdout = &out

	err = cmd.Run()
	if err != nil {
		// fzf exits 130 when the user dismisses with Esc/Ctrl-C, and 1 when
		// the input is empty or no match. Both mean "no selection," not a
		// real error.
		if ee, ok := err.(*exec.ExitError); ok {
			code := ee.ExitCode()
			if code == 130 || code == 1 {
				return &Result{Action: ActionNone}, nil
			}
		}
		return nil, fmt.Errorf("fzf: %w", err)
	}

	return parseFzfOutput(out.String(), cmds)
}

// renderList builds fzf's stdin: one line per command, tab-separated fields:
//   ID \t star \t one-line-command \t [tags]
//
// We render multi-line commands as their first line + " … [N lines]" so the
// picker stays one-row-per-entry. The full multi-line text is shown in the
// preview pane.
//
// Bookmarked commands are prefixed with "★" (and sorted to the top by the
// store query already, but the marker makes that visible).
func renderList(cmds []*store.Command) string {
	var b strings.Builder
	for _, c := range cmds {
		star := " "
		if c.Bookmarked {
			star = "★"
		}
		tagStr := ""
		if len(c.Tags) > 0 {
			tagStr = "[" + strings.Join(c.Tags, ",") + "]"
		}
		fmt.Fprintf(&b, "%d%s%s%s%s%s%s\n",
			c.ID, fieldSep,
			star, fieldSep,
			oneLine(c.Command), fieldSep,
			tagStr,
		)
	}
	return b.String()
}

// RenderListExternal is the exported version used by fzf's reload action
// (via the hidden `postitt _list` subcommand). The output format must match
// renderList exactly so that fzf's internal state stays consistent across
// reloads.
func RenderListExternal(cmds []*store.Command) string {
	return renderList(cmds)
}

// buildFzfArgs constructs the fzf flag set. The complexity is concentrated
// here so the rest of the picker code stays readable.
//
// Layout choices:
//   --delimiter='\t'           tab-separated input columns
//   --with-nth=2..             hide column 1 (the ID) from display
//   --nth=3..                  fuzzy-match against columns 3 (cmd) and 4 (tags)
//   --preview ...              fzf invokes our _preview subcommand on highlight
//   --preview-window=right,55% two-pane layout, like the original script
//   --header                   show keybind hints
//   --expect                   list of keys that exit fzf and are echoed
//   --bind                     in-place actions (bookmark, delete-with-confirm)
//   --reverse                  prompt at top, list grows down (fzf default
//                              is bottom-up which feels odd for this UX)
//   --ansi                     allow color codes in input (not used yet,
//                              but cheap to enable for future)
func buildFzfArgs(selfExec string) []string {
	preview := fmt.Sprintf("%s _preview {1}", shellQuote(selfExec))

	// Bookmark toggle: run the toggle command, then reload the list.
	// {1} expands to column 1 (the ID).
	bindBookmark := fmt.Sprintf(
		"ctrl-b:execute-silent(%s _toggle-bookmark {1})+reload(%s _list)",
		shellQuote(selfExec), shellQuote(selfExec),
	)

	// Delete with confirmation: fzf's `execute` action runs the binary with a
	// real TTY, so we can prompt y/N inside the spawned process. After the
	// command exits, fzf reloads the list (will reflect the deletion or not
	// based on user's confirmation).
	bindDelete := fmt.Sprintf(
		"ctrl-x:execute(%s _confirm-delete {1})+reload(%s _list)",
		shellQuote(selfExec), shellQuote(selfExec),
	)

	// Tag filter: open a tag picker as a sub-fzf. The chosen tag is appended
	// to the session's filter set; the main picker reloads with that filter
	// applied. The transform-prompt action updates the prompt to reflect the
	// active filter so the user sees what's narrowing the list.
	bindTagFilter := fmt.Sprintf(
		"alt-t:execute(%s _pick-tag)+reload(%s _list)+transform-prompt(%s _prompt)",
		shellQuote(selfExec), shellQuote(selfExec), shellQuote(selfExec),
	)

	// Clear all active tag filters.
	bindClearFilter := fmt.Sprintf(
		"alt-a:execute-silent(%s _clear-filter)+reload(%s _list)+transform-prompt(%s _prompt)",
		shellQuote(selfExec), shellQuote(selfExec), shellQuote(selfExec),
	)

	// Open the man page for the highlighted command's base program. fzf's
	// `execute` action gives us a real TTY, so `man` runs in pager mode
	// like normal; on quit (q) we return to the picker.
	bindMan := fmt.Sprintf(
		"alt-m:execute(%s _man {1})",
		shellQuote(selfExec),
	)

	header := strings.Join([]string{
		"Enter: copy   ^E: exec   ^P: print",
		"^B: bookmark  ^X: delete   M-t: tag filter   M-a: clear   M-m: man page",
	}, "\n")

	return []string{
		"--delimiter=" + fieldSep,
		"--with-nth=2..",
		"--nth=3..",
		"--ansi",
		"--reverse",
		"--height=80%",
		"--border",
		"--header=" + header,
		"--header-lines=0",
		"--preview=" + preview,
		"--preview-window=right,55%,wrap",
		"--expect=ctrl-e,ctrl-p",
		"--bind=" + bindBookmark,
		"--bind=" + bindDelete,
		"--bind=" + bindTagFilter,
		"--bind=" + bindClearFilter,
		"--bind=" + bindMan,
		"--prompt=postitt> ",
	}
}

// parseFzfOutput interprets fzf's stdout when --expect is in use. The format
// is:
//   <key-or-empty>\n
//   <selected-line>\n
//
// An empty first line means Enter was pressed (the default action).
func parseFzfOutput(out string, cmds []*store.Command) (*Result, error) {
	lines := strings.SplitN(out, "\n", 3)
	if len(lines) < 2 {
		return &Result{Action: ActionNone}, nil
	}

	keyPressed := strings.TrimSpace(lines[0])
	selected := lines[1]

	if selected == "" {
		return &Result{Action: ActionNone}, nil
	}

	// Recover the ID from the selected line's first column.
	parts := strings.SplitN(selected, fieldSep, 2)
	if len(parts) == 0 {
		return &Result{Action: ActionNone}, nil
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse selected ID %q: %w", parts[0], err)
	}

	var picked *store.Command
	for _, c := range cmds {
		if c.ID == id {
			picked = c
			break
		}
	}
	if picked == nil {
		// ID not in the list we passed in — could happen if a reload between
		// renders dropped the row (e.g., user deleted it via Ctrl-X then hit
		// Enter on something stale). Treat as no selection.
		return &Result{Action: ActionNone}, nil
	}

	action := ActionCopy
	switch keyPressed {
	case "":
		action = ActionCopy
	case "ctrl-e":
		action = ActionExec
	case "ctrl-p":
		action = ActionPrint
	}

	return &Result{Action: action, Command: picked}, nil
}

// oneLine collapses a multi-line command to a single-line display, with a
// marker indicating how many lines were elided. Mirrors the helper in
// cmd/postitt/commands.go (kept duplicated to keep package boundaries
// clean).
func oneLine(s string) string {
	if !strings.Contains(s, "\n") {
		return s
	}
	first := s[:strings.Index(s, "\n")]
	count := strings.Count(s, "\n") + 1
	return fmt.Sprintf("%s … [%d lines]", first, count)
}

// shellQuote wraps a path in single quotes for safe inclusion in fzf's
// `execute(...)` and `--preview` strings, which are interpreted by /bin/sh.
// Single-quote-escaping a single quote requires closing, escaping, reopening,
// which is what this does.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// CopyToWriter is a small helper used by the print action: write the command
// text to the given writer. Exists in this package only because the print
// path benefits from being symmetric with the rest of the picker code.
func CopyToWriter(w io.Writer, c *store.Command) error {
	_, err := io.WriteString(w, c.Command)
	return err
}
