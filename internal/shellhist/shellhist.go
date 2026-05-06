// Package shellhist reads recent command history from the user's interactive
// shell. Three implementations are provided (zsh, bash, fish), and Detect
// picks the right one at runtime based on $CHEATSHH_SHELL, $SHELL, or by
// probing for known history files.
package shellhist

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Reader returns recent shell history entries, most recent last (i.e.,
// Recent(1) returns just the very last command).
type Reader interface {
	// Recent returns up to n recent history entries. Implementations should
	// return them in chronological order (oldest first), matching how a user
	// would read `history` output. Multi-line entries are returned as a single
	// string with embedded newlines where the shell preserves them.
	Recent(n int) ([]string, error)

	// Name returns a short identifier for diagnostics ("zsh", "bash", "fish").
	Name() string
}

// ErrNoHistory indicates that the detected shell has no readable history,
// either because the file doesn't exist or it's empty. Callers should
// surface this as a warning, not a fatal error.
var ErrNoHistory = errors.New("no shell history available")

// Detect picks a Reader based on, in order:
//   1. $CHEATSHH_SHELL (escape hatch: "zsh", "bash", "fish")
//   2. basename of $SHELL
//   3. probing for known history files in $HOME
//
// Returns an error only if no shell could be identified at all.
func Detect() (Reader, error) {
	if forced := os.Getenv("CHEATSHH_SHELL"); forced != "" {
		r, err := byName(forced)
		if err != nil {
			return nil, fmt.Errorf("CHEATSHH_SHELL=%q: %w", forced, err)
		}
		return r, nil
	}

	if sh := os.Getenv("SHELL"); sh != "" {
		base := filepath.Base(sh)
		if r, err := byName(base); err == nil {
			return r, nil
		}
		// Fall through to probing on unrecognized $SHELL.
	}

	if r := probe(); r != nil {
		return r, nil
	}
	return nil, errors.New("could not detect shell (set $CHEATSHH_SHELL to zsh, bash, or fish)")
}

func byName(name string) (Reader, error) {
	switch strings.ToLower(name) {
	case "zsh":
		return &Zsh{}, nil
	case "bash":
		return &Bash{}, nil
	case "fish":
		return &Fish{}, nil
	default:
		return nil, fmt.Errorf("unsupported shell %q", name)
	}
}

// probe checks for the presence of each shell's default history file in
// $HOME and returns the first match. Order: zsh, fish, bash. zsh is checked
// first because its history format is the richest.
func probe() Reader {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	candidates := []struct {
		path string
		r    Reader
	}{
		{filepath.Join(home, ".zsh_history"), &Zsh{}},
		{filepath.Join(home, ".local", "share", "fish", "fish_history"), &Fish{}},
		{filepath.Join(home, ".bash_history"), &Bash{}},
	}

	for _, c := range candidates {
		if _, err := os.Stat(c.path); err == nil {
			return c.r
		}
	}
	return nil
}
