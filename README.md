# devopstui

> This is a vibe coded project I have created to make devops bearable for me. The code will probably be bad, the commit messages unintelligible and features barely working.

A keyboard-driven terminal UI for Azure DevOps boards and sprints, in the spirit of
Lazygit and K9s. See [PLAN.md](PLAN.md) for the design and roadmap.

```
devopstui  contoso › Platform › Team Blue › Sprint 42 Sep 7 – Sep 20          ⟳ just now
 1 Dashboard   2 Sprint   3 Board   4 Backlog                                       3/12
╭──────────────────────────────────────────────────────────────╮╭─────────────────────╮
│  ▾ EPIC  1001 Self-service onboarding      In Progress  AK   ││PBI  #1003           │
│    ▾ FEAT  1002 Invite flow                In Progress  SØ   ││Send invite email…   │
│ ●      PBI   1003 Send invite email with…  Committed    SØ  5││                     │
│ ●      PBI   1004 Accept invite and crea…  Approved     PN  8││State     Committed  │
│  ▾ EPIC  1010 Observability                In Progress  PN   ││Assigned  Sveinung   │
╰──────────────────────────────────────────────────────────────╯╰─────────────────────╯
l expand  space select  e edit  s state  a assign  m move to iteration  p parent  / filter
```

## Install

```bash
go install github.com/sveinungoverland/devopstui/cmd/devopstui@latest
```

This needs a Go toolchain and puts the `devopstui` binary in `$(go env GOPATH)/bin` (or
`$GOBIN` if you've set it) — make sure that directory is on your `PATH`.

To update, run the same command again; it fetches and builds the latest tagged release. Pin a
specific version instead of `@latest` (e.g. `@v0.3.0`) if you want a reproducible install.

Don't have Go, or just want to try it out? `go run ./cmd/devopstui --demo` (below) needs the
toolchain too but skips the install step entirely.

## Run

```bash
go run ./cmd/devopstui --demo
```

Assigning searches your whole organisation, which needs the optional _Identity (read)_ scope;
without it the picker still covers everyone on the project's teams.

Against a real organisation you need a personal access token with _Work Items (read & write)_
and _Project & Team (read)_ scopes:

```bash
export AZURE_DEVOPS_ORG_URL=https://dev.azure.com/<org>
export AZURE_DEVOPS_EXT_PAT=<pat>
go run ./cmd/devopstui --project MyProject --team "My Team"
```

## Configuration

Settings live in `~/.config/devopstui/config.yaml` (`%AppData%\devopstui\config.yaml` on
Windows). Start from the annotated template:

```bash
mkdir -p ~/.config/devopstui && cp config.example.yaml ~/.config/devopstui/config.yaml
```

Then edit `org`, and either add `pat` or export `AZURE_DEVOPS_EXT_PAT`. Project and team are
optional: leave them out to get a picker on first launch, and devopstui writes your choice back
to the file so the next start lands in the same place.

Precedence, highest first: command-line flags (`--org`, `--project`, `--team`), environment
variables (`AZURE_DEVOPS_ORG_URL`, `AZURE_DEVOPS_EXT_PAT`, `DEVOPSTUI_PAT`), then the file.
Use `--config <path>` or `DEVOPSTUI_CONFIG` to keep several configs, one per organisation:

```bash
devopstui --config ~/.config/devopstui/work.yaml
```

| Key                  | Default    | Meaning                                                              |
| -------------------- | ---------- | -------------------------------------------------------------------- |
| `org`                |            | Organisation URL, e.g. `https://dev.azure.com/contoso`               |
| `pat`                |            | Personal access token. Prefer the environment variable.              |
| `project`, `team`    |            | Starting context, rewritten on change. `team` owns the sprints.      |
| `filter_team`        |            | Show only this team's area paths in every view (`T` at runtime)      |
| `confirm_writes`     | `false`    | Ask before single-item edits too (bulk and re-parent always ask)     |
| `refresh_seconds`    | `0`        | Auto-reload interval, 0 = off (`R` toggles, `:auto 30` sets)         |
| `editor`             |            | Description editor; empty = `$VISUAL`/`$EDITOR`, `inline` = built-in |
| `description_format` | `markdown` | `markdown` (native) or `html` (convert on save)                      |

See [config.example.yaml](config.example.yaml) for the commented version.

## Descriptions are Markdown

Azure DevOps work items support native Markdown for large text fields. The TUI writes
descriptions as Markdown and flags the field as Markdown, so what you type is what is stored.
Fields that are still HTML (older items, or organisations without Markdown enabled) are
converted to Markdown on read; saving such a field switches it to Markdown mode in Azure
DevOps, which is a one-way change. Descriptions are rendered with
[Glamour](https://github.com/charmbracelet/glamour) in the detail pane.

If your organisation has not enabled Markdown on work items, set `description_format: html`
in the config and the TUI converts Markdown to HTML on save instead.

Press `d` on an item to edit its description:

- With `$VISUAL` or `$EDITOR` set (or `editor:` in the config), the description opens in that
  editor on a temporary `.md` file. Save and quit to apply, quit without changes to cancel.
  This is the Lazygit way and gives you your own keybindings, spell check and plugins.
- Otherwise the built-in editor opens: line-numbered text on the left, live rendered preview on
  the right. It is modal like vim and starts in **normal** mode:

  | Normal mode                |                                                                           | Insert mode |                     |
  | -------------------------- | ------------------------------------------------------------------------- | ----------- | ------------------- |
  | `j` `k`                    | line down / up                                                            | `esc`       | back to normal mode |
  | `h` `l` `0` `$`            | move within the line                                                      | `ctrl+s`    | save and close      |
  | `g` `G`                    | top / bottom                                                              |             |                     |
  | `space`                    | toggle `- [ ]` / `- [x]` on the line (a plain list item gains a checkbox) |             |                     |
  | `i` `a` `A`                | insert at cursor / after cursor / end of line                             |             |                     |
  | `o` `O`                    | open a line below / above                                                 |             |                     |
  | `dd` `yy`                  | cut / copy the line                                                       |             |                     |
  | `p` `P`                    | paste the line below / above                                              |             |                     |
  | `ctrl+w`                   | save and keep editing, like vim's `:w`                                    |             |                     |
  | `ctrl+s`                   | save and close                                                            |             |                     |
  | `q`                        | close (press twice to discard unsaved changes)                            |             |                     |
  | `ctrl+p` `ctrl+d` `ctrl+u` | toggle / scroll the preview                                               |             |                     |

  The status line tracks what is on the server: `[+]` unsaved, `[saving…]` in flight,
  `[saved]` written. A failed write leaves the text as unsaved, so `q` still warns before
  discarding it. `ctrl+w` works in normal mode only, which leaves it free to delete the
  previous word while you type.

  Set `editor: inline` to force this editor even when `$EDITOR` is set.

The Description field in the `e` edit form uses the same editor. There `ctrl+w` hands the text
to the form as a pending change; the form's own `ctrl+s` writes it with the other fields.

## Keys

Keys are vim-style mnemonics: the letter is the first letter of the action.

| Navigate          |                       | Views   |                     | Change  |                 | Move |                     |
| ----------------- | --------------------- | ------- | ------------------- | ------- | --------------- | ---- | ------------------- |
| `j` `k`           | down / up             | `1`     | dashboard           | `e`     | edit form       | `m`  | move to iteration…  |
| `g` `G`           | top / bottom          | `2`     | sprint tree         | `t`     | title           | `M`  | move to next sprint |
| `l` `h`           | expand / collapse     | `3`     | board               | `d`     | description     | `B`  | move to backlog     |
| `L` `H`           | expand / collapse all | `4`     | backlog             | `s`     | state           | `p`  | set parent          |
| `tab`             | focus detail pane     | `[` `]` | prev / next sprint  | `a`     | assign          |      |                     |
| `z`               | toggle preview pane   | `T`     | team filter         | `n`     | new child item  |      |                     |
| `ctrl+u` `ctrl+d` | scroll preview pane   |         |                     | `N`     | new bug         |      |                     |
| `D`               | item details view     |         |                     |         |                 |      |                     |
| `C`               | discussion            |         |                     |         |                 |      |                     |
| `/`               | filter                | `S`     | current sprint      | `E`     | effort          |      |                     |
| `space`           | select                | `:`     | command bar         | `P`     | priority        |      |                     |
| `v`               | visual select         | `r`     | refresh             | `o`     | open in browser |      |                     |
| `ctrl+a`          | select all            | `R`     | auto refresh on/off | `y`     | yank id         |      |                     |
|                   |                       | `f`     | flat / tree         |         |                 |      |                     |
| `esc`             | clear selection       | `c`     | show closed         | `?` `q` | help / quit     |      |                     |

Any change key acts on the **selection** when there is one, otherwise on the highlighted
item. Bulk changes and re-parenting ask for confirmation. Moving a Feature or Epic offers
to bring its children along.

The sprint picker (`:sprint`, or `[`/`]` past either end) also offers an **Unscheduled**
entry, for items sitting at the team's backlog root instead of any sprint — the Sprint and
Board tabs switch to those the same way they switch to a sprint.

The `m` move popup and the Iteration field in `e`'s edit form list the whole project's
iteration tree, not just the current team's sprints, so an item can be moved to another
team's sprint or a nested release iteration. Sprint navigation above stays scoped to the
current team.

On the board, `h`/`l` move between columns and `H`/`L` move the card to the neighbouring
column. A preview of the highlighted card sits on the right; `z` hides or shows it, `ctrl+u`/
`ctrl+d` scroll it in place when its description doesn't fit, and `tab` focuses it so the
usual navigation keys scroll it instead.

## Assigning people

`a` opens a people picker that searches as you type. It lists the assignees already on screen
straight away, then replaces them with results from the server: everyone on any of the
project's teams, plus anyone in the organisation matching what you typed. Matches are ranked
with name prefixes first, and each row shows the sign-in address so two people with the same
name are distinguishable. Assignments are written by sign-in address, not display name.

This matters when sprints live on a parent team: the picker is not limited to that team's
members, so you can assign to anyone regardless of which team owns the sprint.

## Item details view

Press `D` on any item (or `enter` on a board card) to open it full screen: its metadata across
the top, the rendered Markdown description on the left, and a kanban of its children on the
right, one column per state.

```
 Product Backlog Item #1013  Propagate trace id through queue workers
 In Progress · Sveinung Øverland · Sprint 42 · 8 pts · P1 · 1/3 tasks · 6h left
 ↑ FEAT 1012 Request tracing                                    2h ago by Alex Kim
╭─────────────────────────────────────╮╭──────────────────────────────────────────╮
│Description                          ││Children (3)                              │
│  Propagate trace id through queue…  ││To Do 2         In Progress 0     Done 1  │
│                                     ││──────────────  ──────────────  ────────  │
│  Acceptance criteria                ││▌TASK 1015 2h SØ                TASK 1016 │
│  ▪ trace id survives the queue      ││ Add trace hea…                  Read tr… │
╰─────────────────────────────────────╯╰──────────────────────────────────────────╯
```

| Key             |                                                                        |
| --------------- | ---------------------------------------------------------------------- |
| `tab`           | switch focus between the description and the kanban                    |
| `j` `k` `h` `l` | scroll the description, or move between cards and columns              |
| `H` `L`         | move the highlighted child to the neighbouring column (sets its state) |
| `n`             | add a child; on a highlighted child it adds a sibling                  |
| `D` `enter`     | drill into the highlighted child                                       |
| `esc` `q`       | walk back out, one level at a time                                     |
| `z`             | give the description the full width                                    |
| `C`             | swap the left pane between the description and the discussion          |
| `r`             | re-fetch the children                                                  |

Every action key works here too and applies to whatever has focus: the item itself while the
description is focused, otherwise the highlighted child. So `s` sets a child's state, `d` edits
the item's description, `a` assigns, and so on. Children are fetched for the item you open, so
the kanban is complete even in views that do not load tasks, such as the backlog.

`C` toggles the left pane to the item's Azure DevOps comments (oldest first) and back; it is
read-only — posting a new comment isn't supported yet. On the Board, Sprint and Backlog views,
where the preview pane already has the full terminal height to work with, the discussion shows
underneath the description instead, with no toggle needed.

## Team filter

Some organisations keep sprints on a parent team and assign work to sub-teams through area
paths. Point `team` at the parent (it owns the sprints and board) and use the **team filter**
for your own team: press `T` or run `:filter <team>`. Every view then shows only items in that
team's area paths, with parents from other teams dimmed so the hierarchy stays readable. New
items created with `n` land in the filtered team's default area. `:filter off` clears it. The
choice is saved as `filter_team` in the config.

## Tasks and other children

- PBIs (and Features, Epics) with task-level children show a **`1/3` badge** in the tree and on
  board cards: tasks done over tasks total. Hidden done tasks still count. Green when complete.
- Task rows show **remaining work** in hours in the effort column instead of effort.
- The detail pane lists an item's children with state, assignee and remaining hours, and sums
  the remaining work.
- The team's **bug behaviour** is read from the backlog configuration. When bugs are tracked as
  tasks they nest under PBIs, count in the badge and stay off the board; when bugs are
  requirements they are cards like PBIs.
- **`n` creates a child** of the highlighted item after a title prompt: a Feature under an Epic,
  a PBI (or User Story, whatever the process uses) under a Feature, a Task under a PBI. On a
  Task it creates a sibling. The new item inherits parent, area and the sprint you are looking
  at, and the cursor lands on it.
- **A new task inherits the assignee of the PBI above it**, since that is nearly always who
  will do it. The prompt shows who it will go to, and `a` changes it afterwards. Nothing is
  inherited above the task level: a Feature does not pick up its Epic's assignee.
- **`N` creates a bug** at whichever level the team's bug behaviour puts it: a requirement-level
  Bug under a Feature when bugs are tracked as requirements, a task-level Bug under a PBI (or a
  sibling of a highlighted task) when they are tracked as tasks. It follows the same
  parent-resolution rules as `n`. Pressing it where the highlighted level doesn't match the
  team's bug behaviour, or where bugs are switched off entirely, is a no-op with a flash
  explaining why.

## Dashboard

The dashboard (`1`) is two rows, both scoped to items assigned to `@Me` and, when a sprint is
selected, further scoped to that sprint (the same one shown in the header and on the Sprint/
Board tabs) — plus an optional preview pane:

- **Top: a kanban** of your PBIs in the selected sprint (and bugs, if the team tracks them as
  requirements), in the columns of the team's board — the same look as the Board tab, just
  filtered to your work. `H`/`L` move the highlighted PBI to the neighbouring column (a state
  change), same as on the Board tab. On a wide enough terminal, a preview pane sits to its
  right (`z` toggles it).
- **Bottom: a swimlane kanban** of those PBIs' children — state as the columns (shared across
  every lane, same as any kanban), the parent PBI as the lane. Only a PBI that is itself being
  worked on (In Progress/Active/Committed/Doing, the same states coloured as "in progress"
  elsewhere) gets a lane at all; one that hasn't been started or is already done gets none, even
  if a stray child lingers. A PBI that qualifies shows every child, not just active ones, so a
  lane reads as real progress: some in To Do, some In Progress, some Done. When a cell holds
  several cards they all stack, full height, rather than collapsing
  behind a "+N" — this row gets the bigger share of the body height for exactly that reason.
  Bugs are left out (they carry requirement-level states, not task states, and already show as
  their own card on the kanban above when they're at that level); a PBI with nothing left after
  that simply has no lane. Children show regardless of who they are assigned to. `j`/`k` scroll
  down/up through a column's cards — within the current lane first, then into the next lane's
  cell for that same column — so moving through several cards in one cell doesn't jump you to
  another lane early. `H`/`L` move the highlighted child to the neighbouring state.

Switching sprints (`[`/`]`/`S`/`:sprint`) updates both rows immediately. With no sprint
selected, the dashboard falls back to every sprint at once.

`tab` toggles focus between the kanban and the lanes (a no-op when there is nothing below); the
usual navigation, selection and edit/state/assign actions work in whichever has focus, and
`D`/`enter` drills into the highlighted card. The preview pane is never itself focusable here —
`ctrl+u`/`ctrl+d` scroll it in place from either the kanban or the lanes, whichever has focus.

`R` toggles auto refresh and saves the choice to your config, so it survives a restart. The
header shows `↻60s` while it is on, and the interval is whatever `refresh_seconds` holds, 60
seconds by default. `:auto 30` sets a different interval. Reloads are skipped while a dialog
is open or a write is in flight, so nothing shifts under you mid-edit.

Commands: `:sprint [name]`, `:team`, `:filter [team|off]`, `:auto [on|off|seconds]`, `:project`, `:board`, `:backlog`, `:dash`, `:refresh`,
`:<id>` to look up a work item, `:q`.

## Develop

```bash
make check       # go vet, go test ./..., gofmt — what CI runs
make test        # unit + rendered-frame tests with the in-memory fake
make dumps       # writes rendered frames to ./dumps for eyeballing
make shots       # drives the real binary in a tmux pty, captures ./shots
```

`make dumps` renders through the test harness; `make shots` runs the actual program in a
terminal and presses keys at it, which is the only way to catch colour, cursor and wrapping
problems. Capture one specific thing with:

```bash
scripts/tui-shot.sh --name board --keys "3,j,j,l"
scripts/tui-shot.sh --name filtering --keys "/,type:trace,Enter" --size 80x24
```

Each scene lands in `shots/<name>.txt` as the exact terminal grid, ready to paste into an
issue or a pull request. `--svg` adds a colour image beside it.

For quick manual testing, `go run ./cmd/devopstui --demo` (or with real org flags/env) is the
fastest inner loop — no build step, just edit and re-run.

To test against the real `devopstui` binary as installed users get it, install from your local
checkout instead of the published module:

```bash
go install ./cmd/devopstui
```

This builds from whatever is on disk, so re-running it after each change refreshes the
installed binary with your edits — same command as end users run, just pointed at the local
source tree instead of `@latest`. `make build` is the equivalent one-off build to `bin/devopstui`
if you'd rather not touch `$GOBIN`.

## Automation

Work is driven from labels on issues. Opening an issue gets it a written implementation plan;
adding `agent:ready` gets it a branch, a pull request and an agent that builds it, screenshots
the result and hands it back for review.

```
issue ──▶ backlog ──▶ agent:ready ──▶ agent:in-progress ──▶ agent:in-review ──▶ closed
            plan         ^ you           branch + PR            reviews          merged
```

Adding the label is the only manual step, and it needs no setup beyond the Claude GitHub App:
labels live in the repository, so the workflows' own token can read and write them, and
`issues: labeled` starts a workflow the moment you click.

[docs/automation.md](docs/automation.md) has the setup, the workflows and what to do when
something gets stuck.
