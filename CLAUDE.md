# CLAUDE.md

Notes for future agents working on postitt. Read this first; it'll save you
backtracking from wrong assumptions.

## What this is

A personal command reference. The user (Charlotte) has 30+ sticky notes of
shell commands above her desk and wants to replace them with something that
behaves like fzf-over-her-saved-commands. The full read-me is in `README.md`;
this file is for orienting on the *codebase*.

The original was a ~600-line bash script with two unsynced JSON files
(`commands.json`, `groups.json`) and a wizard-driven whiptail UX. We threw
that out and rewrote in Go with SQLite + tags + fzf. The "groups" concept
became "tags" (a command can have many; AND-filterable). The whiptail
wizard became flag-driven CLI subcommands plus a single fzf picker for
interactive use.

## Architecture in one paragraph

`cmd/postitt/` has cobra wiring; `internal/store/` is SQLite with embedded
migrations; `internal/picker/` shells out to `fzf` and parses its output;
`internal/preview/` formats the right-hand fzf pane; `internal/descsource/`
looks up command descriptions from `tldr` and `man`; `internal/shellhist/`
reads the user's shell history (zsh/bash/fish, runtime-detected);
`internal/clipboard/` writes to the system clipboard via xclip/wl-copy/pbcopy;
`internal/pickerstate/` is a tiny per-process scratch file for sharing tag-
filter state between the running picker and the helper subprocesses fzf
spawns via its bindings.

## The fzf-callback architecture (most important thing to understand)

This is the part most likely to confuse you. The picker runs fzf as a
subprocess. fzf has two relevant features:

- `--expect=ctrl-e,ctrl-p`: these keys exit fzf and the keypress is echoed
  on stdout. Used for terminal actions (copy, exec, print) where the picker
  should close.
- `--bind 'key:execute(...)+reload(...)'`: runs an arbitrary command, then
  reloads the list. Used for in-place actions (bookmark, delete, tag filter,
  open man page) where the picker should stay open.

For the bind callbacks, we pass `os.Executable()` into the bind string and
fzf invokes postitt recursively with hidden `_subcommand` arguments. So
when you press Ctrl-B, fzf forks `postitt _toggle-bookmark <id>`, then
`postitt _list` to refresh.

**Hidden subcommands you'll find in `cmd/postitt/internal_cmds.go`:**

| Subcommand              | Bound to     | What it does                              |
| ----------------------- | ------------ | ----------------------------------------- |
| `_preview ID`           | `--preview`  | Render the right-hand pane                |
| `_list`                 | `reload(...)`| Emit picker input (honors tag filter)     |
| `_toggle-bookmark ID`   | Ctrl-B       | Flip bookmarked flag                      |
| `_confirm-delete ID`    | Ctrl-X       | Prompt y/N then delete                    |
| `_pick-tag`             | Alt-T        | Sub-fzf over tags; appends to filter      |
| `_clear-filter`         | Alt-A        | Empty the tag filter                      |
| `_prompt`               | `transform-prompt` | Emit prompt with active filter      |
| `_man ID`               | Alt-M        | Open `man <base-cmd>` in pager            |

These are marked `Hidden: true` in cobra so they don't show in `--help`.
**Don't promote them to user-facing commands** — they assume an active
picker session and aren't safe to call directly.

## The pickerstate session file

When the picker starts, it creates `$TMPDIR/postitt/session-<pid>` and sets
`$CHEATSHH_SESSION=<path>` on the fzf subprocess. fzf inherits that env;
when fzf forks a binding callback, the callback inherits it too. The
callback reads/writes the file via `pickerstate.{Get,Set,Add,Clear}TagFilter`.

Currently the only state stored is the active tag filter (one tag per line).
**If you add more state, extend `pickerstate` rather than parallel files** —
single source of truth keeps cleanup correct. The picker's `defer
pickerstate.Cleanup(path)` removes the file on exit; if the picker crashes,
the file leaks at `$TMPDIR/postitt/session-<pid>` until reboot. That's
acceptable; don't add a cleanup-orphans-on-startup pass without a reason.

## Database

SQLite via `modernc.org/sqlite` (pure Go, no CGO — important for cross-
compilation and the user's Nix flake which sets `CGO_ENABLED=0`). Don't
swap to `mattn/go-sqlite3` without a really good reason; it'd break the
flake.

Schema is in `internal/store/migrations/0001_init.sql`. Three tables:
`commands`, `tags`, `command_tags` (many-to-many). FTS5 virtual table
`commands_fts` exists but is **not currently wired** — it's there so a
future `--search` flag on `postitt ls` is a one-liner.

Migrations are embedded via `go:embed` and tracked in a `schema_migrations`
table. To add a migration, drop a new `0002_*.sql` file in the migrations
dir; it'll auto-apply on next open. Don't edit `0001_init.sql` after the
fact — make a new migration that alters.

The user's DB lives at `$XDG_DATA_HOME/postitt/postitt.db` (or
`~/.local/share/postitt/postitt.db`). The `--db` flag overrides it for
tests/dev.

## Conventions worth following

**No emoji or fancy unicode in code paths**, except: the `★` bookmark
marker in the picker, the `✓` success prefix in CLI output, and box-drawing
chars in preview dividers. These are deliberate.

**No bullet points / headers in CLI output.** Keep prose. The user is a
terminal power-user and finds over-formatted output annoying.

**Errors that aren't fatal should be warnings, not errors.** Specifically:
clipboard failures, tldr/man lookup failures, and stat-recording failures
shouldn't abort the user's actual goal. Look at how `runPicker` handles
`RecordUse` failure for the pattern.

**Don't add a TUI library.** fzf is a hard dependency, deliberately.
bubbletea/tview reimplementations of fzf get suggested often and they're
not what the user wants.

**Don't add React/web/anything graphical.** This is a terminal tool.

## Testing

Three test files exist:

- `cmd/postitt/commands_test.go` — `parseEditBuffer` (TOML-ish $EDITOR format)
- `internal/pickerstate/pickerstate_test.go` — session file lifecycle
- `internal/descsource/descsource_test.go` — tldr cleanup, man excerpt extraction

Run them: `go test ./...`

There's no integration test for the picker itself, because that would mean
spawning fzf in CI, which is more trouble than it's worth. The picker's
shape is exercised by hand and via the unit tests on the store/state/preview
pieces it composes.

When adding logic that's even slightly tricky, **write a test**. The user
runs this on her main machine, against her real notes; we don't get a
forgiving review cycle.

## Building and running

```sh
go build ./...
go test ./...
go vet ./...
```

The user is on NixOS. The flake is `flake.nix`; `nix build` produces a
static binary. `vendorHash` in the flake needs updating if `go.sum` changes
materially — the user knows the dance.

For local dev outside Nix, `go install ./cmd/postitt` works.

## Foot-guns and surprises

- **Multi-line commands** are stored verbatim (with embedded `\n`) and
  rendered in two ways: collapsed-with-marker for the fzf list (one row
  per command), full in the preview pane. The `oneLine`/`oneLineDisplay`
  helpers handle the collapse; **don't reach for `strings.ReplaceAll(s,
  "\n", " ")` directly** — use the helper.

- **`postitt save` with no args** intentionally skips postitt invocations
  in history (otherwise it would always save itself). `save -N` is explicit
  and skips the filtering. See `pickFromHistory` in `cmd/postitt/commands.go`.

- **bash multi-line history** is broken-by-design at the shell level. bash
  flattens multi-line input into one line on save unless `shopt -s lithist`
  is set. We don't try to reverse-engineer that. zsh and fish preserve
  multi-line correctly. If a user reports "my heredoc came out wrong on
  save" and they're on bash, that's why.

- **Ctrl-E (exec) runs in a subshell** (`$SHELL -c $cmd`). Means `cd`,
  `export`, etc. don't affect the user's current shell. This is documented
  in the README; don't try to "fix" it with TIOCSTI or similar terminal-
  injection tricks — those are blocked on modern Linux for good reason.

- **The `tldr`/`man` lookup in the preview pane runs on every highlight
  change.** No caching yet. At ~50–500ms per render this is fine in
  practice but is the obvious thing to optimize if the user reports lag.
  The cache approach is sketched in chat history: `~/.cache/postitt/
  preview/<id>.txt`, regenerated lazily on first miss or when `updated_at`
  changes. **Don't add it preemptively** — invalidation logic is a
  liability without a real perf complaint to justify it.

- **Tags are AND-combined when filtering**, not OR. Multiple `--tag`
  flags on `postitt ls`, or multiple Alt-T selections in the picker,
  narrow the result. This was a deliberate design choice; if a user
  asks for OR, push back before implementing — it's almost always
  better expressed as a broader tag.

- **fzf exit codes**: 0 = selection, 1 = no match, 130 = user cancelled.
  Both 1 and 130 should be treated as "no selection," not error. See
  `picker.Run` for the pattern.

- **The picker's `--with-nth=2..` hides the ID column** but it's still in
  the line; we extract it from column 1 on selection. Don't re-render
  without the ID — `_list` and `renderList` must produce the same format.

## Things deliberately not implemented

These have come up; here's the user's stance on each so you don't waste
cycles re-pitching them:

- **Group concept** (from the original bash script): replaced by tags.
  Don't bring it back.

- **Whiptail/dialog-driven add wizard**: the user found this annoying
  in the original. `postitt add` is flag-driven; `postitt save`
  prompts inline.

- **Local web UI / electron / React anything**: terminal only.

- **Encryption at rest**: out of scope. The DB is a SQLite file in the
  user's home directory under standard permissions. If a user asks for
  this, point them at full-disk encryption.

- **Sync across machines**: out of scope. SQLite file + git, or
  Syncthing, or any other tool the user already uses.

## When in doubt

Match the existing style and structure. The codebase is small enough
(~4000 LOC) to read end-to-end before making non-trivial changes; do
that before adding a new package or restructuring an existing one. If
something feels harder than it should be, it's probably because the
existing code already has a helper you missed — `grep -r` first.
