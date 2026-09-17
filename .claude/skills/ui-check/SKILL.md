---
name: ui-check
description: See what the devopstui TUI actually looks like by building it, driving it with real keystrokes in a pty, and capturing the screen. Use when changing rendering, layout, colours, key bindings or popups, when reproducing a visual bug, or when a pull request needs a before/after of the UI.
---

# Seeing the TUI

`go test` proves the model is right. It does not prove the screen looks right.
Capture the screen.

## The standard tour

```bash
make shots          # builds, then writes shots/*.txt for nine scenes
```

Scenes: sprint tree, dashboard, board, backlog, details pane, state popup,
filter, description editor, help. Read the `.txt` files — each is the exact
terminal grid, 140×40.

## One specific thing

```bash
scripts/tui-shot.sh --name <name> --keys "<keys>"
```

- Keys are comma-separated and sent one at a time to the real binary over a
  tmux pty: `3,j,j,l` presses those five keys in order.
- `type:trace` types literal text — use it after `/` for the filter, or in the
  description editor.
- `wait:1.5` pauses without pressing anything, for something that loads.
- `--size 80x24` checks a narrow terminal, which is where layout breaks first.
- `--svg` also writes a colour image next to the text.

Bindings live in `internal/ui/keymap.go`. Useful ones: `1`–`4` switch views,
`j`/`k` move, `l`/`h` expand and collapse, `D` details, `s` state, `a` assign,
`d` description, `/` filter, `?` help, `esc` back.

```bash
scripts/tui-shot.sh --name narrow --keys "3" --size 80x24
scripts/tui-shot.sh --name filtering --keys "/,type:trace,Enter"
scripts/tui-shot.sh --name editor --keys "j,j,d,i,type:hello,Escape"
```

## Before and after

For a change to existing layout, capture the same scene on both sides:

```bash
git stash && make build && scripts/tui-shot.sh --name before --keys "3"
git stash pop && make build && scripts/tui-shot.sh --name after --keys "3"
diff shots/before.txt shots/after.txt
```

Put both in the pull request inside fenced code blocks. A reviewer should not
have to build the program to see what changed.

## When it fails

The script exits non-zero if the program died before the capture and prints
what was on screen — usually a panic, and usually the fastest way to read one.
If a key seems to do nothing, raise `--delay` (default 0.5s) or `--settle`
(default 2.5s); the data loads asynchronously and a capture can land early.
