# devopstui — context for automated agents

Read this first in any automated run. It describes the project, how to verify a
change, and the label contract the automation runs on.

## What this is

A keyboard-driven terminal UI for Azure DevOps boards and sprints, in the
spirit of Lazygit and K9s. Go, [Bubble Tea](https://github.com/charmbracelet/bubbletea)
for the event loop, [Lipgloss](https://github.com/charmbracelet/lipgloss) for
layout, [Glamour](https://github.com/charmbracelet/glamour) for Markdown.

`PLAN.md` holds the design, the milestone history and the decisions already
made — check section 9 ("Decisions and assumptions") before proposing something
that contradicts one. `README.md` is the user-facing documentation and is part
of the product: a change to keys, config or behaviour is not finished until the
README matches it.

## Package layout

| Package            | Holds                                                                  |
| ------------------ | ---------------------------------------------------------------------- |
| `cmd/devopstui`    | Flag parsing and wiring. Deliberately thin.                            |
| `internal/model`   | Work items, the tree, pure logic. No I/O, no terminal.                 |
| `internal/ado`     | Azure DevOps client, plus `Fake` — the in-memory backend for `--demo`. |
| `internal/ui`      | Bubble Tea models: views, popups, keymap, theme, the editor.           |
| `internal/config`  | Config file, flags and environment precedence.                         |
| `internal/markdown`| HTML↔Markdown conversion and rendering.                                |

`internal/ui/keymap.go` is the single source of truth for key bindings — add
bindings there, never inline in an update function.

## Verifying a change

```bash
make build     # go build -o bin/devopstui ./cmd/devopstui
make test      # go test ./...   (internal/ui takes ~1 minute, that is normal)
make vet
make check     # vet + test + gofmt check — run this before pushing
```

`internal/ui/app_test.go` has a harness that drives the whole Bubble Tea model
synchronously against `ado.Fake`: `newHarness(t, w, h)` then `h.keys("j", "s",
"enter")` then assert on `h.app.View()` or on `h.fake.Updates` (the writes the
backend received). Prefer adding a case there over a unit test that pokes at an
unexported field, because it tests the flow the user actually performs.

## Seeing the UI

Two levels, both available in CI:

```bash
make dumps     # View() snapshots from the test harness, into dumps/
make shots     # the real binary driven in a tmux pty, into shots/
```

`make shots` runs `scripts/shots.sh`, which takes the standard tour of the app
(sprint tree, dashboard, board, backlog, details, state popup, filter,
description editor, help). Each scene lands in `shots/<name>.txt` as the exact
terminal grid. Capture one scene yourself with:

```bash
scripts/tui-shot.sh --name my-change --keys "3,j,j,l"
scripts/tui-shot.sh --name filtering --keys "/,type:trace,Enter" --size 160x48
```

Keys are sent one at a time to the real program over a pty, so this catches
what `View()` snapshots cannot: colour, cursor position, wrapping and anything
that only goes wrong at terminal level. `type:foo` types literal text and
`wait:1.5` pauses. Add `--svg` for a colour image next to the text.

**Show the UI in the pull request.** When a change is visible, paste the
relevant `shots/*.txt` capture into the PR description or a comment inside a
```` ``` ```` fence, before and after if the change alters existing layout. A
reviewer should not have to run the program to see what changed.

## Style

Match the code already in the file. Specifically:

- Comments explain *why*, and sit above the thing they explain. The codebase is
  lightly commented; do not narrate obvious code.
- Errors surface in the UI through the existing status/flash mechanism rather
  than `log.Fatal`.
- No new dependencies without saying so explicitly in the PR description — this
  is a small tool and the dependency list is deliberately short.
- Every write to Azure DevOps goes through `internal/ado`, is confirmable, and
  keeps working against `ado.Fake` so `--demo` and the tests stay honest.

## Label contract

Work is driven from labels on the issue. An issue carries at most one `agent:`
status at a time, and that status is the whole control surface:

| Issue state                | Meaning                                                                     |
| -------------------------- | --------------------------------------------------------------------------- |
| open, no `agent:` status   | The backlog. A plan comment is posted here; nothing is built yet.           |
| `agent:ready`              | **The human's go-ahead.** Adding it starts an agent: branch, draft PR, build. |
| `agent:in-progress`        | An agent is implementing. The PR exists and is a draft.                     |
| `agent:in-review`          | Implementation finished, PR marked ready, the review agent runs.            |
| closed                     | Done, by a merged PR.                                                       |

Three more labels are independent of the status: `agent:planned` (a plan
comment exists), `agent:needs-decision` (the plan is waiting on an answer) and
`agent:blocked` (a run failed).

Never add `agent:ready` yourself — that one belongs to the human, and only
someone with write access can apply it, which is what makes it a safe trigger.
The rest are set by the workflows in `.github/workflows/` through
`.github/scripts/status.sh`:

```bash
.github/scripts/status.sh set 42 in-progress   # exactly one status label
.github/scripts/status.sh get 42
.github/scripts/status.sh flag 42 add blocked
```

`docs/automation.md` explains the whole pipeline.
