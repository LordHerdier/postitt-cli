package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/charlotte/cheatshh/internal/descsource"
	"github.com/charlotte/cheatshh/internal/shellhist"
	"github.com/charlotte/cheatshh/internal/store"
)

// newAddCmd: `cheatshh add CMD [-d "desc"] [-t tag1,tag2]`
func newAddCmd(dbPath *string) *cobra.Command {
	var desc string
	var tagsRaw string

	cmd := &cobra.Command{
		Use:   "add COMMAND",
		Short: "Add a new command directly",
		Long: `Add a command to the database. Description and tags can be passed via
flags; without flags, the command is added with no description and no tags
(you can edit later with 'cheatshh edit ID').

Example:
  cheatshh add 'git stash pop' -d "restore most recent stash" -t git,stash`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open(*dbPath)
			if err != nil {
				return err
			}
			defer s.Close()

			id, err := s.Add(args[0], desc, store.ParseTags(tagsRaw), false)
			if err != nil {
				if errors.Is(err, store.ErrDuplicate) {
					return fmt.Errorf("command already exists; use 'cheatshh edit' or 'cheatshh rm' first")
				}
				return err
			}
			fmt.Printf("✓ Added #%d: %s\n", id, args[0])
			return nil
		},
	}
	cmd.Flags().StringVarP(&desc, "description", "d", "", "command description")
	cmd.Flags().StringVarP(&tagsRaw, "tags", "t", "", "comma-separated tags")
	return cmd
}

// newSaveCmd: `cheatshh save [N|-N]` — capture from shell history.
//
// Forms:
//   cheatshh save        -> save the most recent NON-cheatshh command
//                           (skips the `cheatshh save` invocation itself,
//                            plus any preceding cheatshh commands)
//   cheatshh save -1     -> save the literal last entry (no filtering;
//                           usually that's `cheatshh save` itself, which
//                           is rarely what you want, but allowed as escape
//                           hatch for testing or unusual workflows)
//   cheatshh save -3     -> save the third-most-recent entry, no filtering
//   cheatshh save 5      -> picker over the last 5 commands [TODO]
func newSaveCmd(dbPath *string) *cobra.Command {
	var desc string
	var tagsRaw string
	var skipPrompt bool

	cmd := &cobra.Command{
		Use:   "save [N|-N]",
		Short: "Save a command from shell history",
		Long: `Capture a recent command from your shell's history. Detects zsh, bash,
and fish automatically (override with $CHEATSHH_SHELL).

If the description is left blank, cheatshh will try to auto-fill it from
tldr (preferred) or man.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			reader, err := shellhist.Detect()
			if err != nil {
				return err
			}

			// Distinguish "user gave an explicit offset" from "no args, find
			// the right thing to save." Explicit -N is taken at face value,
			// no filtering. Implicit (no args) walks back from the most
			// recent and skips cheatshh invocations.
			explicit := false
			offset := 1
			if len(args) == 1 {
				n, err := strconv.Atoi(args[0])
				if err != nil {
					return fmt.Errorf("invalid argument %q: expected an integer", args[0])
				}
				if n < 0 {
					offset = -n
					explicit = true
				} else {
					// TODO: positive N opens a picker over the last N entries.
					return fmt.Errorf("multi-entry picker not implemented yet; pass -N for the Nth most recent")
				}
			}

			cmdText, err := pickFromHistory(reader, offset, explicit)
			if errors.Is(err, shellhist.ErrNoHistory) {
				fmt.Fprintln(os.Stderr,
					"warning: no shell history available; use 'cheatshh add' instead")
				return err
			}
			if err != nil {
				return err
			}

			fmt.Printf("Saving: %s\n", oneLineDisplay(cmdText))

			tags := store.ParseTags(tagsRaw)

			if !skipPrompt {
				if desc == "" {
					desc = promptLine("Description (blank = auto-fill): ")
				}
				if tagsRaw == "" {
					tagsRaw = promptLine("Tags (comma-separated): ")
					tags = store.ParseTags(tagsRaw)
				}
			}

			autoFillable := false
			if desc == "" {
				name := firstWord(cmdText)
				if name != "" {
					ctx := context.Background()
					if d, err := descsource.Default().Lookup(ctx, name); err == nil {
						desc = d
						fmt.Printf("Auto-filled description: %s\n", desc)
					} else {
						autoFillable = true
					}
				} else {
					autoFillable = true
				}
			}

			s, err := store.Open(*dbPath)
			if err != nil {
				return err
			}
			defer s.Close()

			id, err := s.Add(cmdText, desc, tags, autoFillable)
			if err != nil {
				if errors.Is(err, store.ErrDuplicate) {
					existing, gerr := s.GetByText(cmdText)
					if gerr == nil {
						_ = s.RecordUse(existing.ID)
						fmt.Printf("✓ Already saved as #%d (use_count bumped)\n", existing.ID)
						return nil
					}
				}
				return err
			}
			fmt.Printf("✓ Saved #%d\n", id)
			return nil
		},
	}
	cmd.Flags().StringVarP(&desc, "description", "d", "", "command description (blank = auto-fill)")
	cmd.Flags().StringVarP(&tagsRaw, "tags", "t", "", "comma-separated tags")
	cmd.Flags().BoolVarP(&skipPrompt, "no-prompt", "y", false, "skip interactive prompts")
	return cmd
}

// newPrintCmd: `cheatshh print` -> picker -> stdout.
func newPrintCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "print",
		Short: "Pick a command and write it to stdout",
		Long: `Open the picker and write the selected command to stdout, suitable
for shell substitution: eval "$(cheatshh print)" or run "$(cheatshh print)".

Note: multi-line commands include embedded newlines; quote appropriately.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open(*dbPath)
			if err != nil {
				return err
			}
			defer s.Close()
			return runPicker(s, pickActionPrint)
		},
	}
}

// newLsCmd: `cheatshh ls [--tag X]` -> plain text listing.
func newLsCmd(dbPath *string) *cobra.Command {
	var tagFilter []string

	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List saved commands",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open(*dbPath)
			if err != nil {
				return err
			}
			defer s.Close()

			cmds, err := s.List(tagFilter)
			if err != nil {
				return err
			}
			for _, c := range cmds {
				star := " "
				if c.Bookmarked {
					star = "*"
				}
				tagStr := ""
				if len(c.Tags) > 0 {
					tagStr = "[" + strings.Join(c.Tags, ",") + "]"
				}
				fmt.Printf("%s %4d  %-50s %s\n",
					star, c.ID, oneLineDisplay(c.Command), tagStr)
			}
			return nil
		},
	}
	// StringArrayVar (not StringSliceVar) so each --tag value is taken
	// verbatim and not comma-split. Tag names shouldn't contain commas, but
	// we want repeated --tag flags to be the only way to specify multiple
	// tags, for clarity.
	cmd.Flags().StringArrayVar(&tagFilter, "tag", nil,
		"filter by tag (repeat for AND filter: --tag git --tag stash)")
	return cmd
}

// newTagsCmd: `cheatshh tags` -> tag listing with counts.
func newTagsCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "tags",
		Short: "List all tags with command counts",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open(*dbPath)
			if err != nil {
				return err
			}
			defer s.Close()
			tags, err := s.AllTags()
			if err != nil {
				return err
			}
			for _, t := range tags {
				fmt.Printf("%-20s %d\n", t.Name, t.Count)
			}
			return nil
		},
	}
}

// newTagCmd: `cheatshh tag ID +foo -bar` adjusts tags on a single command.
func newTagCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "tag ID [+tag|-tag ...]",
		Short: "Add or remove tags on a command",
		Long: `Modify a command's tags using +name to add and -name to remove. Tags not
listed are left alone.

Example:
  cheatshh tag 47 +production -experimental`,
		Args: cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid id %q", args[0])
			}
			var add, remove []string
			for _, a := range args[1:] {
				if strings.HasPrefix(a, "+") && len(a) > 1 {
					add = append(add, a[1:])
				} else if strings.HasPrefix(a, "-") && len(a) > 1 {
					remove = append(remove, a[1:])
				} else {
					return fmt.Errorf("tag args must start with + or -, got %q", a)
				}
			}

			s, err := store.Open(*dbPath)
			if err != nil {
				return err
			}
			defer s.Close()

			if err := s.AdjustTags(id, add, remove); err != nil {
				return err
			}
			c, err := s.Get(id)
			if err != nil {
				return err
			}
			fmt.Printf("✓ Updated #%d tags: %s\n", id, strings.Join(c.Tags, ", "))
			return nil
		},
	}
}

// newEditCmd: `cheatshh edit ID` opens $EDITOR with a small TOML buffer.
func newEditCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:   "edit ID",
		Short: "Edit a command in $EDITOR",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid id %q", args[0])
			}
			s, err := store.Open(*dbPath)
			if err != nil {
				return err
			}
			defer s.Close()

			c, err := s.Get(id)
			if err != nil {
				return err
			}

			edited, err := editInEditor(c)
			if err != nil {
				return err
			}

			if err := s.Update(id, edited.Command, edited.Description, false); err != nil {
				return err
			}
			if err := s.SetTags(id, edited.Tags); err != nil {
				return err
			}
			if err := s.SetBookmark(id, edited.Bookmarked); err != nil {
				return err
			}
			fmt.Printf("✓ Updated #%d\n", id)
			return nil
		},
	}
}

// newRmCmd: `cheatshh rm ID [-f]` deletes a command.
func newRmCmd(dbPath *string) *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "rm ID",
		Short: "Delete a command",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid id %q", args[0])
			}
			s, err := store.Open(*dbPath)
			if err != nil {
				return err
			}
			defer s.Close()

			c, err := s.Get(id)
			if err != nil {
				return err
			}

			if !force {
				fmt.Printf("Delete: %s ? [y/N] ", oneLineDisplay(c.Command))
				answer := promptLine("")
				if strings.ToLower(strings.TrimSpace(answer)) != "y" {
					fmt.Println("aborted")
					return nil
				}
			}

			if err := s.Delete(id); err != nil {
				return err
			}
			fmt.Printf("✓ Deleted #%d\n", id)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&force, "force", "f", false, "skip confirmation")
	return cmd
}

// newPinCmd: pin/unpin both get the same code path with a flipped flag.
func newPinCmd(dbPath *string, set bool) *cobra.Command {
	use := "pin ID"
	short := "Bookmark a command (sorts to top of picker)"
	if !set {
		use = "unpin ID"
		short = "Remove a bookmark"
	}
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid id %q", args[0])
			}
			s, err := store.Open(*dbPath)
			if err != nil {
				return err
			}
			defer s.Close()
			if err := s.SetBookmark(id, set); err != nil {
				return err
			}
			verb := "Pinned"
			if !set {
				verb = "Unpinned"
			}
			fmt.Printf("✓ %s #%d\n", verb, id)
			return nil
		},
	}
}

// editInEditor writes a TOML-ish buffer for the user to edit and parses it
// back. We avoid pulling in a real TOML library for this — the format is
// trivial and we control both writer and reader.
//
// String fields use TOML's two quoting forms:
//   - `"..."`     for single-line values (with standard \-escapes)
//   - `"""..."""` for multi-line values (preserved verbatim, no escapes)
//
// We pick the form per-field based on whether the value contains a newline.
func editInEditor(c *store.Command) (*store.Command, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	tmpf, err := os.CreateTemp("", "cheatshh-edit-*.toml")
	if err != nil {
		return nil, err
	}
	tmpPath := tmpf.Name()
	defer os.Remove(tmpPath)

	buf := fmt.Sprintf(
		"# Edit the fields below.\n"+
			"# Multi-line strings use \"\"\"triple quotes\"\"\".\n"+
			"# Tags is a TOML array; bookmarked is a bool.\n"+
			"#\n"+
			"command = %s\n"+
			"description = %s\n"+
			"tags = [%s]\n"+
			"bookmarked = %t\n",
		tomlString(c.Command),
		tomlString(c.Description),
		quoteList(c.Tags),
		c.Bookmarked,
	)
	if _, err := tmpf.WriteString(buf); err != nil {
		tmpf.Close()
		return nil, err
	}
	tmpf.Close()

	ed := exec.Command(editor, tmpPath)
	ed.Stdin, ed.Stdout, ed.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := ed.Run(); err != nil {
		return nil, fmt.Errorf("editor exited with error: %w", err)
	}

	body, err := os.ReadFile(tmpPath)
	if err != nil {
		return nil, err
	}
	return parseEditBuffer(string(body))
}

// tomlString picks the right quoting form for a string value. Single-line
// strings get `"..."` with Go-style escaping (which happens to match TOML's
// basic-string escapes for the characters we care about). Multi-line strings
// get `"""..."""` literal blocks: TOML's "multi-line basic string" form,
// where the only thing we need to worry about is the value itself containing
// `"""`. We don't try to escape that — if the user really has triple-quotes
// in their command, they need to edit by hand. (Vanishingly rare in shell.)
func tomlString(s string) string {
	if !strings.Contains(s, "\n") {
		return strconv.Quote(s)
	}
	// TOML's """...""" preserves contents literally. A leading newline
	// directly after the opening """ is trimmed by spec, which is exactly
	// what we want for readable formatting:
	//
	//   command = """
	//   for f in *.log; do
	//     gzip "$f"
	//   done
	//   """
	return "\"\"\"\n" + s + "\n\"\"\""
}

// parseEditBuffer parses the TOML-ish edit buffer back into a Command.
//
// Recognized lines:
//   key = "value"                    single-line basic string
//   key = """\n...lines...\n"""      multi-line basic string (any field)
//   key = "value"\nkey2 = ...        single-line basic string with trailing
//                                    content (rare; we still handle it)
//   tags = ["a", "b"]                inline array
//   bookmarked = true / false        bool
//   # comment                        ignored
//   <blank>                          ignored
//
// We intentionally don't try to be a full TOML parser; we recognize the
// shapes our writer produces and fail loudly on anything else, with a hint
// to use --force or to edit by hand.
func parseEditBuffer(body string) (*store.Command, error) {
	out := &store.Command{}
	lines := strings.Split(body, "\n")

	i := 0
	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		// Skip comments and blank lines at the top level. (Inside a
		// triple-quoted block we don't get here — the multi-line reader
		// below consumes everything verbatim until the closing fence.)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			i++
			continue
		}

		eq := strings.Index(trimmed, "=")
		if eq < 0 {
			return nil, fmt.Errorf("line %d: expected `key = value`, got %q", i+1, trimmed)
		}
		key := strings.TrimSpace(trimmed[:eq])
		val := strings.TrimSpace(trimmed[eq+1:])

		// Detect triple-quoted block: the value on this line is either
		// `"""` alone (block starts on next line) or starts with `"""`
		// followed by content on the same line.
		if strings.HasPrefix(val, `"""`) {
			block, consumed, err := readTripleQuoted(lines, i, eq+1)
			if err != nil {
				return nil, err
			}
			if err := assignField(out, key, block, true); err != nil {
				return nil, err
			}
			i += consumed
			continue
		}

		// Single-line value. Dispatch by key.
		if err := assignField(out, key, val, false); err != nil {
			return nil, err
		}
		i++
	}

	if out.Command == "" {
		return nil, fmt.Errorf("command field is empty")
	}
	return out, nil
}

// readTripleQuoted consumes a multi-line basic string starting at lines[start].
// `valueOffset` is the byte offset in lines[start] where the value (after `=`)
// begins, so we can detect content on the opening line.
//
// Returns the unquoted body, the number of source lines consumed (including
// the opening and closing lines), or an error if the block is unterminated.
//
// Per TOML spec, a leading newline immediately after the opening `"""` is
// trimmed, and we honor that. Trailing newline before the closing `"""` is
// likewise stripped because our writer adds one for readability.
func readTripleQuoted(lines []string, start, valueOffset int) (string, int, error) {
	open := strings.TrimSpace(lines[start][valueOffset:])
	if !strings.HasPrefix(open, `"""`) {
		return "", 0, fmt.Errorf("line %d: expected opening \"\"\"", start+1)
	}
	rest := open[3:] // anything after the opening fence on the same line

	// Case 1: block closes on the same line: `key = """foo"""` (rare; we
	// still handle it for completeness).
	if idx := strings.Index(rest, `"""`); idx >= 0 {
		return rest[:idx], 1, nil
	}

	// Case 2: multi-line block. `rest` is the (possibly empty) first line
	// of content. Per TOML, if the first line is blank because the user
	// put `"""` at end-of-line, we drop that leading newline.
	var b strings.Builder
	firstWritten := false
	if rest != "" {
		b.WriteString(rest)
		firstWritten = true
	}

	for j := start + 1; j < len(lines); j++ {
		// Closing fence: a line whose trimmed form is exactly `"""`, or
		// a line ending in `"""` (with content before the fence).
		if idx := strings.Index(lines[j], `"""`); idx >= 0 {
			// Preserve any text before the closing fence on this line.
			pre := lines[j][:idx]
			body := b.String()
			if pre != "" {
				if firstWritten {
					body += "\n"
				}
				body += pre
			}
			// Strip a trailing newline if our writer added one (i.e., the
			// closing `"""` was on its own line). That matches what the
			// writer does in tomlString.
			if pre == "" && firstWritten {
				body = strings.TrimSuffix(body, "\n")
			}
			return body, j - start + 1, nil
		}

		// Continuation line: append with newline separator.
		if firstWritten {
			b.WriteByte('\n')
		}
		b.WriteString(lines[j])
		firstWritten = true
	}

	return "", 0, fmt.Errorf("line %d: unterminated triple-quoted string", start+1)
}

// assignField sets the named field on out from the given raw value. If
// `preParsed` is true, val is the already-extracted string body of a triple-
// quoted block; otherwise val is the raw RHS of a `key = value` line and
// needs unquoting (for strings) or interpretation (for tags / bookmarked).
func assignField(out *store.Command, key, val string, preParsed bool) error {
	switch key {
	case "command":
		s := val
		if !preParsed {
			u, err := strconv.Unquote(val)
			if err != nil {
				return fmt.Errorf("parse command: %w", err)
			}
			s = u
		}
		out.Command = s

	case "description":
		s := val
		if !preParsed {
			u, err := strconv.Unquote(val)
			if err != nil {
				return fmt.Errorf("parse description: %w", err)
			}
			s = u
		}
		out.Description = s

	case "tags":
		// Tags is always a single-line array; preParsed shouldn't happen
		// here under our writer, but if a user manually triple-quoted it
		// we just take the raw form.
		raw := val
		raw = strings.TrimPrefix(raw, "[")
		raw = strings.TrimSuffix(raw, "]")
		for _, t := range strings.Split(raw, ",") {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			if s, err := strconv.Unquote(t); err == nil {
				out.Tags = append(out.Tags, s)
			} else {
				out.Tags = append(out.Tags, t)
			}
		}

	case "bookmarked":
		out.Bookmarked = val == "true"

	default:
		// Unknown keys are ignored rather than erroring — lets us add
		// new fields later without breaking edits of older buffers.
	}
	return nil
}

// quoteList renders a slice of strings as TOML-ish: "a", "b", "c"
func quoteList(ss []string) string {
	parts := make([]string, len(ss))
	for i, s := range ss {
		parts[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(parts, ", ")
}

// promptLine reads a single line from stdin with a prompt prefix.
func promptLine(prefix string) string {
	if prefix != "" {
		fmt.Print(prefix)
	}
	var line string
	fmt.Scanln(&line)
	return line
}

// oneLineDisplay collapses a (possibly multi-line) command to a single line
// for compact display, with an ellipsis marker for truncation.
func oneLineDisplay(s string) string {
	if !strings.Contains(s, "\n") {
		return s
	}
	first := s[:strings.Index(s, "\n")]
	count := strings.Count(s, "\n") + 1
	return fmt.Sprintf("%s … [%d lines]", first, count)
}

// firstWord returns the first whitespace-separated token, useful for picking
// the program name out of a command line for tldr/man lookup.
func firstWord(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}

// pickFromHistory returns a single command string from the user's shell
// history. There are two modes:
//
//   explicit=true:  return the entry at exactly `offset` from the end.
//                   No filtering — the user asked for a specific position.
//
//   explicit=false: walk backward from the end and return the first entry
//                   that is NOT a cheatshh invocation. This is the default
//                   for `cheatshh save` with no args. We have to filter
//                   because at least one cheatshh invocation (the save
//                   itself) is always present at the very end of history.
//
// We pull a generous batch from the reader (64 entries) and search within
// that. If everything in the batch is a cheatshh invocation we fall back to
// returning the most recent entry as-is rather than refusing to save.
//
// Note: shellhist.Reader.Recent returns entries in chronological order
// (oldest first), so the "most recent" lives at the end of the slice.
func pickFromHistory(reader shellhist.Reader, offset int, explicit bool) (string, error) {
	if explicit {
		entries, err := reader.Recent(offset)
		if err != nil {
			return "", err
		}
		if len(entries) < offset {
			return "", fmt.Errorf("only %d entries in history, can't get -%d",
				len(entries), offset)
		}
		return entries[len(entries)-offset], nil
	}

	const batch = 64
	entries, err := reader.Recent(batch)
	if err != nil {
		return "", err
	}
	if len(entries) == 0 {
		return "", shellhist.ErrNoHistory
	}

	// Walk backward, skipping our own invocations.
	for i := len(entries) - 1; i >= 0; i-- {
		if !isCheatshhInvocation(entries[i]) {
			return entries[i], nil
		}
	}
	// Everything is cheatshh; fall back to the actual last entry rather
	// than failing. This is unusual enough that a friendly warning is
	// better than an error.
	fmt.Fprintln(os.Stderr,
		"warning: only cheatshh invocations in recent history; saving the last entry anyway")
	return entries[len(entries)-1], nil
}

// isCheatshhInvocation returns true if the given history entry looks like
// a cheatshh command. We check the first program-word so things like
// `sudo cheatshh ...` or `CHEATSHH_DB=foo cheatshh ...` are still caught.
func isCheatshhInvocation(entry string) bool {
	for _, f := range strings.Fields(entry) {
		// Skip env var assignments.
		if strings.Contains(f, "=") && !strings.ContainsAny(f, "/.") {
			continue
		}
		// Skip privilege wrappers and their flags.
		if f == "sudo" || f == "doas" || f == "env" {
			continue
		}
		if strings.HasPrefix(f, "-") {
			continue
		}
		// First real token. Match either the bare name or any path ending
		// in /cheatshh (./cheatshh, /usr/local/bin/cheatshh, etc.).
		base := f
		if idx := strings.LastIndex(f, "/"); idx >= 0 {
			base = f[idx+1:]
		}
		return base == "cheatshh"
	}
	return false
}
