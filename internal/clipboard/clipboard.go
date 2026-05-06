// Package clipboard writes text to the system clipboard, auto-detecting the
// right tool for the platform. The user can override with $CHEATSHH_COPY_CMD
// (a shell command that reads from stdin).
package clipboard

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ErrNoBackend means no clipboard tool was found on the system.
var ErrNoBackend = errors.New("no clipboard tool found (install xclip, wl-clipboard, or pbcopy)")

// Copy writes text to the clipboard. The text is passed as-is on stdin to
// the chosen backend, including any embedded newlines.
func Copy(text string) error {
	cmd, err := backend()
	if err != nil {
		return err
	}
	cmd.Stdin = strings.NewReader(text)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("clipboard copy failed: %w", err)
	}
	return nil
}

// backend constructs the *exec.Cmd for the right clipboard tool. Detection
// order: $CHEATSHH_COPY_CMD, then platform defaults.
func backend() (*exec.Cmd, error) {
	// User override: a shell command read via `sh -c`.
	if override := os.Getenv("CHEATSHH_COPY_CMD"); override != "" {
		return exec.Command("sh", "-c", override), nil
	}

	switch runtime.GOOS {
	case "darwin":
		if _, err := exec.LookPath("pbcopy"); err == nil {
			return exec.Command("pbcopy"), nil
		}
	case "linux":
		// Wayland first, since X11 fallback often exists alongside.
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			if _, err := exec.LookPath("wl-copy"); err == nil {
				return exec.Command("wl-copy"), nil
			}
		}
		if _, err := exec.LookPath("xclip"); err == nil {
			return exec.Command("xclip", "-selection", "clipboard"), nil
		}
		if _, err := exec.LookPath("xsel"); err == nil {
			return exec.Command("xsel", "--clipboard", "--input"), nil
		}
	case "windows":
		if _, err := exec.LookPath("clip.exe"); err == nil {
			return exec.Command("clip.exe"), nil
		}
	}
	return nil, ErrNoBackend
}
