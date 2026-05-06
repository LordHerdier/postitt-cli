// Package descsource looks up a one-line description for a command name from
// external sources (tldr, man). The Composite source tries each in order and
// returns the first non-empty result.
package descsource

import (
	"bufio"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// Source returns a short human-readable description for a command name, or
// ErrNotFound if no description could be produced.
type Source interface {
	Lookup(ctx context.Context, name string) (string, error)
	Name() string
}

// ErrNotFound indicates this source has no description for the command.
var ErrNotFound = errors.New("description not found")

// Composite tries each underlying source in order and returns the first
// successful lookup. Errors other than ErrNotFound short-circuit.
type Composite struct {
	Sources []Source
}

func (c *Composite) Lookup(ctx context.Context, name string) (string, error) {
	for _, s := range c.Sources {
		desc, err := s.Lookup(ctx, name)
		if err == nil && desc != "" {
			return desc, nil
		}
		if err != nil && !errors.Is(err, ErrNotFound) {
			// Tool present but failed for an unexpected reason; keep trying.
			continue
		}
	}
	return "", ErrNotFound
}

func (c *Composite) Name() string { return "composite" }

// Default returns the recommended source: tldr first, man as fallback.
func Default() Source {
	return &Composite{
		Sources: []Source{&Tldr{}, &Man{}},
	}
}

// commandName returns the bare program name from a command line, suitable
// for tldr/man lookup. Examples:
//   "git stash pop"                    -> "git"
//   "find . -name '*.log' -delete"     -> "find"
//   "sudo -E docker system prune"      -> "docker"
//   "DEBUG=1 ./run.sh"                 -> "" (skip)
func commandName(cmdline string) string {
	fields := strings.Fields(cmdline)
	for _, f := range fields {
		// Skip env var assignments like FOO=bar.
		if strings.Contains(f, "=") && !strings.ContainsAny(f, "/.") {
			continue
		}
		// Skip leading "sudo" / "doas" / "env"; recurse to the next field.
		if f == "sudo" || f == "doas" || f == "env" {
			continue
		}
		// Skip flags to those wrappers (e.g. `sudo -E`).
		if strings.HasPrefix(f, "-") {
			continue
		}
		// Skip anything path-like; we don't have descriptions for ./scripts.
		if strings.ContainsAny(f, "/") {
			return ""
		}
		return f
	}
	return ""
}

// runWithTimeout runs cmd with a deadline and returns its stdout, or an error
// if it doesn't exit within the timeout. Used to keep tldr/man from hanging.
func runWithTimeout(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if ctx.Err() == context.DeadlineExceeded {
		return nil, ctx.Err()
	}
	return out, err
}

// firstNonEmptyLine reads up to the first non-empty line and returns it,
// trimmed. Useful for grabbing the summary line from man/tldr output.
func firstNonEmptyLine(text string) string {
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			return line
		}
	}
	return ""
}
