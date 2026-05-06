# cheatshh

A fast personal command reference. Picker over commands you've saved, with
tags, auto-generated descriptions from tldr/man, and shell history capture.

A rewrite of the bash original, in Go.

## Install

```sh
nix build
./result/bin/cheatshh --help
```

Or with Go directly:

```sh
go install ./cmd/cheatshh
```

## Dependencies

- **fzf** — required for the picker
- **tldr** *(optional)* — used to auto-fill descriptions and populate the
  preview pane
- **xclip / wl-copy / pbcopy** — for the default copy action; auto-detected.
  Override with `$CHEATSHH_COPY_CMD` (a shell command that reads from stdin).

## Quick reference

```
cheatshh                    open picker (Enter copies to clipboard)
cheatshh print              picker, write to stdout — for $(cheatshh print)

cheatshh add CMD            add directly
  -d, --description STR
  -t, --tags TAG1,TAG2

cheatshh save [-N]          save from shell history
                            with no args, saves the most recent command
                            -3 saves the third-most-recent
                            description auto-fills from tldr if blank

cheatshh ls                 list all
  --tag TAG                 filter (repeatable, AND-combined)

cheatshh tags               list all tags with counts
cheatshh tag ID +foo -bar   add/remove tags on a command
cheatshh edit ID            open in $EDITOR (TOML buffer)
cheatshh rm ID [-f]         delete (with confirmation unless -f)
cheatshh pin ID / unpin ID  bookmark; pinned items sort to the top
```

## Picker keybinds

- **Enter** — copy command to clipboard *(default)*
- **Ctrl-E** — execute command via `$SHELL -c`
- **Ctrl-P** — print command to stdout (used internally by `cheatshh print`)
- **Ctrl-B** — toggle bookmark on the highlighted entry
- **Ctrl-X** — delete the highlighted entry (with y/N confirmation)
- **Alt-T** — open a tag picker; selected tag narrows the main list
              (repeat to combine multiple tags as an AND filter)
- **Alt-A** — clear all active tag filters
- **Alt-M** — open the man page for the highlighted command's base program
- **Esc / Ctrl-C** — dismiss without doing anything

The prompt updates to show the active filter, e.g. `cheatshh [git+stash]>`.

## Preview pane

The right-hand pane shows the command, its tags, usage stats, your description
(if any), and reference material from external sources:

1. **tldr** — full page body with usage examples (preferred when available)
2. **man** — `NAME` + `SYNOPSIS` excerpt as a fallback

When you haven't written a description, the reference material fills in.
Press **Alt-M** to open the full man page in your pager.

## Caveats

- **`Ctrl-E` runs in a subshell.** That means `cd`, `export`, and other
  commands that modify shell state won't affect your current shell. For
  those, use the default copy action and paste it yourself.
- **Bash multi-line history** is best-effort — bash flattens multi-line
  commands on save unless you've enabled `shopt -s lithist`. zsh and fish
  preserve multi-line correctly.
- **`cheatshh print` of a multi-line command**: shell `$(...)` substitution
  collapses whitespace; you'll want `"$(cheatshh print)"` with quotes.

## Environment variables

- `CHEATSHH_SHELL` — force shell detection (`zsh` / `bash` / `fish`)
- `CHEATSHH_COPY_CMD` — shell command for clipboard copy, reads stdin
- `XDG_DATA_HOME` — db location, default `~/.local/share/cheatshh/cheatshh.db`
- `HISTFILE` — honored for zsh/bash history reading
- `EDITOR` — used by `cheatshh edit`, falls back to `vi`

## Database

SQLite at `$XDG_DATA_HOME/cheatshh/cheatshh.db`. Three tables: `commands`,
`tags`, `command_tags`. FTS5 virtual table over command + description for
future search features. Migrations are embedded in the binary and applied
on first open.

You can also poke at it with the `sqlite3` CLI if you need to do something
the tool doesn't support yet.
