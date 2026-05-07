package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/LordHerdier/postitt-cli/internal/picker"
	"github.com/LordHerdier/postitt-cli/internal/pickerstate"
	"github.com/LordHerdier/postitt-cli/internal/preview"
	"github.com/LordHerdier/postitt-cli/internal/store"
)

// These four commands are not user-facing. They're invoked by fzf bindings
// via subprocess calls back into the postitt binary. We mark them Hidden so
// they don't show up in `postitt --help`, but they're regular cobra commands
// so they pick up the same --db flag and exit-code conventions.

// newPreviewCmd: `postitt _preview ID` — render the right-hand pane for
// the highlighted command. Called by fzf via --preview on every change in
// the highlighted row.
func newPreviewCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:    "_preview ID",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				// fzf swallows non-zero exit codes silently in --preview;
				// we just print the error so the user sees it in the pane.
				fmt.Fprintf(os.Stdout, "invalid id: %s\n", args[0])
				return nil
			}
			s, err := store.Open(*dbPath)
			if err != nil {
				fmt.Fprintf(os.Stdout, "open db: %v\n", err)
				return nil
			}
			defer s.Close()

			// includeTldr=true: tldr lookup adds <2s on cache hit, less if
			// the user has tldr's local cache (which most do).
			return preview.Render(os.Stdout, s, id, true)
		},
	}
}

// newListInternalCmd: `postitt _list` — emit the picker's input format on
// stdout. Used by fzf's reload action after an in-place mutation, and on
// every prompt change. Honors the active tag filter from picker session
// state if one is set.
//
// The format must match what picker.Run() generates internally on first
// load; the picker package exposes a renderer for exactly this purpose so
// the two paths can't drift.
func newListInternalCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:    "_list",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open(*dbPath)
			if err != nil {
				return err
			}
			defer s.Close()

			tagFilter, err := pickerstate.TagFilter(pickerstate.Path())
			if err != nil {
				// Don't fail the picker on a session-read glitch; just
				// fall back to no filter.
				tagFilter = nil
			}

			cmds, err := s.List(tagFilter)
			if err != nil {
				return err
			}
			fmt.Print(picker.RenderListExternal(cmds))
			return nil
		},
	}
}

// newToggleBookmarkCmd: `postitt _toggle-bookmark ID` — flip the bookmark
// state. Bound to Ctrl-B in the picker; output is suppressed (execute-silent)
// so the picker UI isn't disturbed.
func newToggleBookmarkCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:    "_toggle-bookmark ID",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return err
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
			return s.SetBookmark(id, !c.Bookmarked)
		},
	}
}

// newConfirmDeleteCmd: `postitt _confirm-delete ID` — prompt for y/N then
// delete. Bound to Ctrl-X in the picker. fzf's `execute` action gives us a
// real TTY here so the prompt works. After we exit, fzf reloads the list.
func newConfirmDeleteCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:    "_confirm-delete ID",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return err
			}
			s, err := store.Open(*dbPath)
			if err != nil {
				return err
			}
			defer s.Close()

			c, err := s.Get(id)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return nil
				}
				return err
			}

			// Clear the screen first — fzf's UI is still partially visible
			// otherwise, and that's confusing during a destructive prompt.
			fmt.Print("\033[2J\033[H")
			fmt.Printf("Delete this command?\n\n  %s\n\n",
				oneLineDisplay(c.Command))
			if len(c.Tags) > 0 {
				fmt.Printf("  tags: %s\n", strings.Join(c.Tags, ", "))
			}
			fmt.Print("\n[y/N] ")

			var answer string
			fmt.Scanln(&answer)
			if strings.ToLower(strings.TrimSpace(answer)) != "y" {
				fmt.Println("aborted")
				return nil
			}
			if err := s.Delete(id); err != nil {
				return err
			}
			fmt.Printf("✓ Deleted #%d\n", id)
			return nil
		},
	}
}

// newPickTagCmd: `postitt _pick-tag` — open a sub-fzf over the tag list
// and append the user's choice to the session's tag filter. Bound to
// Alt-T in the main picker.
//
// We don't return the chosen tag on stdout; instead we write to the
// session file directly. The main picker's reload action picks it up
// immediately, so the user sees the filter applied without further
// interaction.
func newPickTagCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:    "_pick-tag",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionPath := pickerstate.Path()
			if sessionPath == "" {
				return fmt.Errorf("no active picker session")
			}

			s, err := store.Open(*dbPath)
			if err != nil {
				return err
			}
			defer s.Close()

			tags, err := s.AllTags()
			if err != nil {
				return err
			}
			if len(tags) == 0 {
				// No tags exist; nothing to do. Return cleanly so the
				// outer picker just reloads with the same list.
				return nil
			}

			// Build the input: "tag (count)". On selection we strip the
			// count back off to get the tag name.
			var b strings.Builder
			for _, tc := range tags {
				fmt.Fprintf(&b, "%s\t(%d)\n", tc.Name, tc.Count)
			}

			fzf := exec.Command("fzf",
				"--delimiter=\t",
				"--with-nth=1,2",
				"--nth=1",
				"--reverse",
				"--height=40%",
				"--border",
				"--prompt=tag> ",
				"--header=Pick a tag to filter by (Esc to cancel)",
			)
			fzf.Stdin = strings.NewReader(b.String())
			fzf.Stderr = os.Stderr
			var out strings.Builder
			fzf.Stdout = &out

			if err := fzf.Run(); err != nil {
				// 130 = user cancelled; that's fine.
				if ee, ok := err.(*exec.ExitError); ok {
					if ee.ExitCode() == 130 || ee.ExitCode() == 1 {
						return nil
					}
				}
				return fmt.Errorf("tag picker: %w", err)
			}

			selected := strings.TrimSpace(out.String())
			if selected == "" {
				return nil
			}
			// First field before the tab is the tag name.
			tag := strings.SplitN(selected, "\t", 2)[0]

			return pickerstate.AddTagFilter(sessionPath, tag)
		},
	}
}

// newClearFilterCmd: `postitt _clear-filter` — remove all active tag
// filters from the session. Bound to Alt-A in the main picker.
func newClearFilterCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "_clear-filter",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionPath := pickerstate.Path()
			if sessionPath == "" {
				return nil // nothing to clear
			}
			return pickerstate.ClearTagFilter(sessionPath)
		},
	}
}

// newPromptCmd: `postitt _prompt` — emit the prompt string for fzf's
// transform-prompt action. Includes the active tag filter when one is set
// so the user can see what's narrowing the list.
//
// Examples of output:
//   "postitt> "                no filter
//   "postitt [git]> "          single tag
//   "postitt [git+stash]> "    multiple tags (AND)
func newPromptCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "_prompt",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			tags, err := pickerstate.TagFilter(pickerstate.Path())
			if err != nil || len(tags) == 0 {
				fmt.Print("postitt> ")
				return nil
			}
			fmt.Printf("postitt [%s]> ", strings.Join(tags, "+"))
			return nil
		},
	}
}

// newManCmd: `postitt _man ID` — open the man page for the highlighted
// command's base program (e.g. "git" for `git stash pop`). Bound to Alt-M
// in the picker. Runs `man` directly so the user gets their normal pager
// behavior; on quit, fzf redraws and we're back at the picker.
//
// Failures are surfaced briefly to the terminal (the user is in an
// `execute` block, not a silent one, so they see what happened) and then
// we return cleanly so fzf takes over again.
func newManCmd(dbPath *string) *cobra.Command {
	return &cobra.Command{
		Use:    "_man ID",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return nil
			}
			s, err := store.Open(*dbPath)
			if err != nil {
				return nil
			}
			defer s.Close()

			c, err := s.Get(id)
			if err != nil {
				return nil
			}

			name := firstProgramWord(c.Command)
			if name == "" {
				fmt.Println("no recognizable program name in this command")
				fmt.Print("press Enter to return... ")
				fmt.Scanln(new(string))
				return nil
			}

			manBin, err := exec.LookPath("man")
			if err != nil {
				fmt.Println("`man` not found on $PATH")
				fmt.Print("press Enter to return... ")
				fmt.Scanln(new(string))
				return nil
			}

			m := exec.Command(manBin, name)
			m.Stdin, m.Stdout, m.Stderr = os.Stdin, os.Stdout, os.Stderr
			// `man` exits non-zero when no page exists; show a tiny prompt
			// rather than vanishing silently so the user knows what happened.
			if err := m.Run(); err != nil {
				fmt.Printf("\nno man page for %q\n", name)
				fmt.Print("press Enter to return... ")
				fmt.Scanln(new(string))
			}
			return nil
		},
	}
}

// firstProgramWord pulls the program name out of a command line (skipping
// env-var prefixes like FOO=bar and privilege wrappers like sudo). Mirrors
// the helper in descsource and preview.
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
