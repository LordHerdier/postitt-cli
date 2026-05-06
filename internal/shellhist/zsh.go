package shellhist

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Zsh reads from $HISTFILE or ~/.zsh_history.
//
// Extended history format looks like:
//   : 1700000000:0;git commit -m "fix"
// where 1700000000 is the start time, 0 is duration, and everything after the
// `;` is the command. Multi-line commands use a trailing backslash on lines
// that aren't the last; we collapse those back into a single entry with
// embedded newlines.
//
// Plain history (no `setopt extendedhistory`) is just one command per line.
type Zsh struct{}

func (Zsh) Name() string { return "zsh" }

func (z *Zsh) Recent(n int) ([]string, error) {
	path, err := zshHistPath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoHistory
		}
		return nil, fmt.Errorf("open zsh history: %w", err)
	}
	defer f.Close()

	// We need to read the whole file because zsh history can have multi-line
	// entries spanning arbitrary lines, and we want the last n complete entries.
	// A reverse scan would be more efficient on huge histories, but zsh
	// history files are typically a few MB at most.
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var entries []string
	var cur strings.Builder
	flush := func() {
		s := strings.TrimRight(cur.String(), "\n")
		if s != "" {
			entries = append(entries, s)
		}
		cur.Reset()
	}

	inMultiline := false
	for scanner.Scan() {
		line := scanner.Text()

		// A new entry starts with ": <ts>:<dur>;" in extended format.
		// In plain format, each line is its own entry, so we still treat
		// the start of a non-continuation line as a new entry.
		if !inMultiline {
			flush()
			// Strip the timestamp prefix if present.
			if strings.HasPrefix(line, ": ") {
				if idx := strings.Index(line, ";"); idx >= 0 {
					line = line[idx+1:]
				}
			}
		}

		// A backslash at end-of-line means the next line continues this entry.
		if strings.HasSuffix(line, "\\") {
			cur.WriteString(strings.TrimSuffix(line, "\\"))
			cur.WriteByte('\n')
			inMultiline = true
		} else {
			cur.WriteString(line)
			inMultiline = false
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan zsh history: %w", err)
	}

	if len(entries) == 0 {
		return nil, ErrNoHistory
	}
	if n <= 0 || n > len(entries) {
		n = len(entries)
	}
	return entries[len(entries)-n:], nil
}

func zshHistPath() (string, error) {
	if h := os.Getenv("HISTFILE"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".zsh_history"), nil
}
