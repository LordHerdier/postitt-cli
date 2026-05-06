package shellhist

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Bash reads from $HISTFILE or ~/.bash_history.
//
// Bash by default flattens multi-line commands into single lines on save
// (joined with `; `), so we don't try to reconstruct them. If HISTTIMEFORMAT
// is set, history is interleaved with `#<unix-ts>` lines which we strip.
//
// Caveat: there is no portable way to capture truly multi-line bash history
// without `shopt -s lithist`, which most users don't enable. This is a known
// limitation of the bash implementation.
type Bash struct{}

func (Bash) Name() string { return "bash" }

func (b *Bash) Recent(n int) ([]string, error) {
	path, err := bashHistPath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoHistory
		}
		return nil, fmt.Errorf("open bash history: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var entries []string
	for scanner.Scan() {
		line := scanner.Text()
		// Skip HISTTIMEFORMAT timestamp lines.
		if strings.HasPrefix(line, "#") && len(line) > 1 && isAllDigits(line[1:]) {
			continue
		}
		if line == "" {
			continue
		}
		entries = append(entries, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan bash history: %w", err)
	}

	if len(entries) == 0 {
		return nil, ErrNoHistory
	}
	if n <= 0 || n > len(entries) {
		n = len(entries)
	}
	return entries[len(entries)-n:], nil
}

func bashHistPath() (string, error) {
	if h := os.Getenv("HISTFILE"); h != "" {
		return h, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".bash_history"), nil
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
