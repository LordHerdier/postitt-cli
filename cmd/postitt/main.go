// Command postitt is a personal command reference: a fast picker for
// commands you've saved, replacing sticky notes above your desk.
//
// Subcommand overview:
//   postitt                 picker -> copy to clipboard (default action)
//   postitt print           picker -> stdout (for $(postitt print))
//   postitt add CMD         add directly with -d/-t flags
//   postitt save [N|-N]     capture from shell history, auto-fill desc
//   postitt ls [--tag X]    list, optionally filtered
//   postitt tags            list all tags with counts
//   postitt tag ID +x -y    add/remove tags
//   postitt edit ID         open in $EDITOR
//   postitt rm ID [-f]      delete
//   postitt pin/unpin ID    bookmark
package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/spf13/cobra"

	"github.com/LordHerdier/postitt-cli/internal/clipboard"
	"github.com/LordHerdier/postitt-cli/internal/picker"
	"github.com/LordHerdier/postitt-cli/internal/store"
)

// version is overridden at build time via -ldflags "-X main.version=...".
var version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		// cobra already prints a friendly message for most errors; we just
		// need to exit non-zero.
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	var dbPath string

	root := &cobra.Command{
		Use:   "postitt",
		Short: "Personal command reference and picker",
		Long: `postitt is a fast picker for commands you've saved, with tags,
auto-generated descriptions from tldr/man, and shell history capture.

Run with no arguments to open the picker; selected commands are copied to
the clipboard by default. Use Ctrl-E in the picker to execute, Ctrl-P to
print, Ctrl-B to bookmark.`,
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
		// Default action: open the picker.
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open(dbPath)
			if err != nil {
				return err
			}
			defer s.Close()
			return runPicker(s, pickActionCopy)
		},
	}

	root.PersistentFlags().StringVar(&dbPath, "db", "",
		"path to postitt.db (default: $XDG_DATA_HOME/postitt/postitt.db)")

	root.AddCommand(
		newAddCmd(&dbPath),
		newSaveCmd(&dbPath),
		newPrintCmd(&dbPath),
		newLsCmd(&dbPath),
		newTagsCmd(&dbPath),
		newTagCmd(&dbPath),
		newEditCmd(&dbPath),
		newRmCmd(&dbPath),
		newPinCmd(&dbPath, true),
		newPinCmd(&dbPath, false),
		// Hidden helpers invoked by fzf bindings — not part of the user UX.
		newPreviewCmd(&dbPath),
		newListInternalCmd(&dbPath),
		newToggleBookmarkCmd(&dbPath),
		newConfirmDeleteCmd(&dbPath),
		newPickTagCmd(&dbPath),
		newClearFilterCmd(),
		newPromptCmd(),
		newManCmd(&dbPath),
	)

	return root
}

// pickAction enumerates what to do once the user has picked a command.
type pickAction int

const (
	pickActionCopy pickAction = iota
	pickActionPrint
	pickActionExec
)

// runPicker is the shared entry point for the picker. It opens fzf via the
// picker package, then dispatches to the right action based on which key
// the user pressed to confirm. In-place actions (bookmark, delete) are
// handled by fzf bindings via hidden subcommands and never reach this code.
func runPicker(s *store.Store, action pickAction) error {
	cmds, err := s.List(nil)
	if err != nil {
		return err
	}
	if len(cmds) == 0 {
		fmt.Fprintln(os.Stderr,
			"no commands saved yet — try 'postitt add' or 'postitt save'")
		return nil
	}

	selfExec, err := os.Executable()
	if err != nil {
		// Fall back to "postitt" on $PATH; this is fine as long as the user
		// installed it normally. Only matters for the bind callbacks.
		selfExec = "postitt"
	}

	res, err := picker.Run(cmds, selfExec)
	if err != nil {
		return err
	}
	if res.Action == picker.ActionNone || res.Command == nil {
		return nil
	}

	// If the caller forced a specific action (e.g. `postitt print`), respect
	// that over whatever key was pressed. The picker still allows ctrl-e/p
	// override though, so we only override when the caller asked for print.
	chosen := res.Action
	if action == pickActionPrint {
		chosen = picker.ActionPrint
	}

	// Record the use BEFORE acting, so even if the action fails (e.g.
	// clipboard not configured) we still capture the intent. Failures here
	// are non-fatal — we don't want to skip the user's actual goal because
	// of a stats update.
	if err := s.RecordUse(res.Command.ID); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not record use: %v\n", err)
	}

	switch chosen {
	case picker.ActionCopy:
		if err := clipboard.Copy(res.Command.Command); err != nil {
			return fmt.Errorf("copy: %w", err)
		}
		fmt.Fprintf(os.Stderr, "✓ Copied: %s\n", oneLineDisplay(res.Command.Command))
	case picker.ActionPrint:
		// Important: print to stdout so it can be captured via $(postitt ...).
		fmt.Print(res.Command.Command)
	case picker.ActionExec:
		return execCommand(res.Command.Command)
	}
	return nil
}

// execCommand runs the user's shell with -c and the picked command. We use
// $SHELL when available so functions/aliases the user has defined in their
// shell rc are available; falling back to /bin/sh otherwise. The subshell
// caveat (cd doesn't persist, etc.) is documented in the README.
func execCommand(cmdText string) error {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	c := exec.Command(shell, "-c", cmdText)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	// Print the command before running so the user sees what executed —
	// fzf's full-screen UI hides scrollback up to this point, so the line
	// is otherwise invisible.
	fmt.Fprintf(os.Stderr, "$ %s\n", cmdText)
	return c.Run()
}
