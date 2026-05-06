package shellhist

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Fish reads from ~/.local/share/fish/fish_history.
//
// The format is YAML-ish (fish doesn't use a real YAML library; it writes
// its own subset). Entries look like:
//
//   - cmd: git status
//     when: 1700000000
//     paths:
//       - .
//   - cmd: |-
//       for f in *.log
//           gzip $f
//       end
//     when: 1700000100
//
// Multi-line commands use `|-` block scalars with consistent indentation.
// We hand-parse rather than depending on a YAML library for one shell.
type Fish struct{}

func (Fish) Name() string { return "fish" }

func (fi *Fish) Recent(n int) ([]string, error) {
	path, err := fishHistPath()
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoHistory
		}
		return nil, fmt.Errorf("open fish history: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var (
		entries  []string
		cur      strings.Builder
		inBlock  bool
		hasEntry bool
	)
	flush := func() {
		if hasEntry {
			s := strings.TrimRight(cur.String(), "\n")
			if s != "" {
				entries = append(entries, s)
			}
		}
		cur.Reset()
		hasEntry = false
		inBlock = false
	}

	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "- cmd: |-"):
			flush()
			hasEntry = true
			inBlock = true
			// Block scalar; subsequent indented lines belong to this entry.

		case strings.HasPrefix(line, "- cmd: "):
			flush()
			hasEntry = true
			inBlock = false
			cmd := strings.TrimPrefix(line, "- cmd: ")
			cur.WriteString(unescapeFishCmd(cmd))

		case inBlock && strings.HasPrefix(line, "    "):
			// Indented continuation of a block scalar (4-space indent).
			if cur.Len() > 0 {
				cur.WriteByte('\n')
			}
			cur.WriteString(strings.TrimPrefix(line, "    "))

		case strings.HasPrefix(line, "  when:") ||
			strings.HasPrefix(line, "  paths:") ||
			strings.HasPrefix(line, "    - "):
			// Metadata for the current entry; close the block scalar.
			inBlock = false

		default:
			// Unknown / blank line; ignore.
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan fish history: %w", err)
	}

	if len(entries) == 0 {
		return nil, ErrNoHistory
	}
	if n <= 0 || n > len(entries) {
		n = len(entries)
	}
	return entries[len(entries)-n:], nil
}

// unescapeFishCmd reverses the simple escapes fish writes for inline cmd
// values: \\ -> \, \n -> newline. Block-scalar entries don't go through this.
func unescapeFishCmd(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			case '\\':
				b.WriteByte('\\')
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func fishHistPath() (string, error) {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "fish", "fish_history"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "fish", "fish_history"), nil
}
