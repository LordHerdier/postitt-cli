// Package pickerstate manages a tiny scratch file used to share state
// between a running picker (fzf) and the postitt helper subprocesses it
// invokes via --bind callbacks.
//
// The state lives at a temp path keyed by the parent picker process ID,
// which keeps concurrent picker instances isolated. The picker creates
// the file on startup and removes it on exit; helpers read/write it.
//
// The only state we currently track is the active tag filter (one tag
// per line). The format is plain text so it's debuggable with cat and
// trivially extensible.
package pickerstate

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// envVar is the name of the environment variable postitt sets when
// launching fzf, so subprocess helpers know where to find session state.
// Using an env var rather than a fixed path means concurrent pickers
// don't collide and stale files don't get picked up by new sessions.
const envVar = "CHEATSHH_SESSION"

// pathLock guards the file from concurrent writes within a single process.
// Cross-process races are extremely unlikely given the single-picker
// invariant, but the lock costs nothing and removes one class of bug.
var pathLock sync.Mutex

// New creates a fresh session file for the current process and returns
// the path. The caller (the picker) is responsible for setting the env
// var on the fzf subprocess and for cleanup with Cleanup().
func New() (string, error) {
	dir := filepath.Join(os.TempDir(), "postitt")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create session dir: %w", err)
	}
	path := filepath.Join(dir, fmt.Sprintf("session-%d", os.Getpid()))
	// Truncate any stale file from a previous picker that crashed without
	// cleanup.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return "", fmt.Errorf("create session file: %w", err)
	}
	f.Close()
	return path, nil
}

// Cleanup removes the session file. Safe to call even if the file is
// already gone.
func Cleanup(path string) {
	if path == "" {
		return
	}
	_ = os.Remove(path)
}

// Path returns the active session file path from the environment, or
// the empty string if no session is active. Helper subprocesses use this
// to locate the file.
func Path() string {
	return os.Getenv(envVar)
}

// EnvVar returns the name of the env var postitt sets to communicate
// the session path to subprocesses. Exposed so the picker can set it
// when launching fzf.
func EnvVar() string {
	return envVar
}

// SetTagFilter writes the given tags (one per line) to the session file,
// replacing any previous filter. An empty slice clears the filter.
func SetTagFilter(path string, tags []string) error {
	if path == "" {
		return errors.New("no session path")
	}
	pathLock.Lock()
	defer pathLock.Unlock()

	var b strings.Builder
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" {
			continue
		}
		b.WriteString(t)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o600)
}

// AddTagFilter appends a tag to the session's existing filter set,
// deduplicating. Used when the user picks a second tag to narrow further.
func AddTagFilter(path, tag string) error {
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return nil
	}
	existing, err := TagFilter(path)
	if err != nil {
		return err
	}
	for _, t := range existing {
		if t == tag {
			return nil // already present
		}
	}
	return SetTagFilter(path, append(existing, tag))
}

// ClearTagFilter empties the session's filter file.
func ClearTagFilter(path string) error {
	return SetTagFilter(path, nil)
}

// TagFilter reads the current tag filter from the session file. Returns
// nil (no filter) if the file is empty or doesn't exist; an error only
// for genuine I/O failures.
func TagFilter(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		out = append(out, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
