# DevOps TUI — Implementation Plan

A keyboard-driven terminal UI for Azure DevOps boards, sprints and work-item hierarchies.
Interaction model borrows from **Lazygit** (side-by-side panels, single-key actions, contextual
help footer, confirm popups) and **K9s** (a "context" you drill in and out of with `Enter`/`Esc`,
a `:` command bar for jumping between views, a header showing where you are, auto-refresh).

## 1. Scope (from `tui_intent.md`)

| # | Feature | Plan section |
|---|---------|--------------|
| F1 | Quickly change between boards and sprints | Context switcher (§5.2), Sprint view (§5.4) |
| F2 | See hierarchy of Epic / Feature / PBI | Tree view (§5.4) |
| F3 | Quickly edit Epics / Features / PBIs | Detail + quick-edit (§5.5) |
| F4 | Quickly move Features / PBIs | Move actions (§5.6) |
| F5 | Multi-select for bulk move (e.g. sprint → next sprint) | Selection model (§5.6) |
| F6 | Dashboard of the user's items | Dashboard view (§5.7) |

Out of scope for v1: pipelines, repos/PRs, test plans, creating new projects/teams, custom process
templates beyond Scrum/Agile field names (Epic/Feature/PBI or User Story).

## 2. Tech stack

| Concern | Choice | Version | Why |
|---------|--------|---------|-----|
| Language | Go | 1.27 (per `go.mod`) | — |
| ADO API | `github.com/microsoft/azure-devops-go-api/azuredevops/v7` | v7.1.0 | Required by the brief. Packages used: `core`, `work`, `workitemtracking`, `webapi`. |
| TUI framework | `github.com/charmbracelet/bubbletea` | v1.3.10 | Elm-style model/update/view, same family Lazygit-likes (e.g. `gh dash`) use. Stay on v1; v2 is still pre-release. |
| Widgets | `github.com/charmbracelet/bubbles` | v1.0.0 | `textinput`, `textarea`, `viewport`, `spinner`, `key`, `help`, `table`. |
| Styling | `github.com/charmbracelet/lipgloss` | v1.1.0 | Borders, layout, theming. |
| Config | `github.com/BurntSushi/toml` or `gopkg.in/yaml.v3` | — | `~/.config/devopstui/config.yaml` (K9s-style). |
| Testing | stdlib + `github.com/charmbracelet/x/exp/teatest` | — | Golden-file tests of rendered views. |
| Concurrency | `golang.org/x/sync/errgroup` | — | Bulk updates with bounded parallelism. |

No cobra/viper for v1: a single binary with a handful of flags (`--org`, `--project`, `--team`,
`--config`) is enough.

## 3. Azure DevOps API mapping

The SDK is a thin generated client; all the heavy lifting is WIQL plus JSON-patch updates.

| Need | Call | Notes |
|------|------|-------|
| Auth | `azuredevops.NewPatConnection(orgURL, pat)` | PAT from `AZURE_DEVOPS_EXT_PAT` env, config, or keychain later. Scopes needed: *Work Items (Read & Write)*, *Project & Team (Read)*. |
| Current user | `connection.ConnectionData` / `profile` | Needed for `@Me` resolution and the dashboard. WIQL `@Me` works without it, so this is optional. |
| Projects | `core.Client.GetProjects` | Cached for the session. |
| Teams | `core.Client.GetTeams` | Per project. |
| Sprints | `work.Client.GetTeamIterations(ctx, {Project, Team})` and `Timeframe: "current"` | Gives `Path`, `StartDate`, `FinishDate`. "Next sprint" = the iteration after current by start date. |
| Backlog config | `work.Client.GetBacklogConfigurations` | Tells us which work-item types are Epic/Feature/Requirement level for this process (handles PBI vs User Story). |
| Boards | `work.Client.GetBoards`, `GetBoard` | Board columns for the board view; column of an item is the `System.BoardColumn` field. |
| Hierarchy | `workitemtracking.Client.QueryByWiql` with a **WorkItemLinks** query using `System.LinkTypes.Hierarchy-Forward`, `MODE (Recursive)` | Returns `(Source, Target)` id pairs. Then `GetWorkItemsBatch` in chunks of ≤200 ids with an explicit `Fields` list. |
| Sprint contents | WIQL `WHERE [System.IterationPath] UNDER '<path>'` combined with the link query above so parents outside the sprint still show as tree roots (greyed). | |
| My items | WIQL `[System.AssignedTo] = @Me AND [System.State] <> 'Closed'…` | Dashboard. |
| Edit fields | `workitemtracking.Client.UpdateWorkItem` with `[]webapi.JsonPatchOperation{Op: Add/Replace, Path: "/fields/System.Title", Value: …}` | Fields: `System.Title`, `System.State`, `System.AssignedTo`, `System.IterationPath`, `System.Description`, `Microsoft.VSTS.Common.Priority`, `Microsoft.VSTS.Scheduling.Effort` / `StoryPoints`. Send `System.Rev` in the patch (`/rev` test op) for optimistic concurrency. |
| Move to sprint | Same, `Replace /fields/System.IterationPath` | One request per item; no bulk endpoint in the SDK, so run with errgroup + limit 4. |
| Re-parent | Patch: `Remove /relations/<idx>` of the old `System.LinkTypes.Hierarchy-Reverse` relation, `Add /relations/-` with the new parent URL | Requires fetching the item with `Expand: Relations` first to know the index. |
| Allowed states | `workitemtracking.Client.GetWorkItemTypeStates` | Populate the state picker per type. |

Rate limits: ADO throttles around 200 TSTUs/5 min. Bulk moves of a whole sprint (30–50 items) are
fine; just bound concurrency and surface 429s in the status bar.

## 4. Package layout

```
cmd/devopstui/main.go         flag parsing, config load, wire client → tea.Program
internal/
  config/                     load/save config.yaml, env overrides, last-used context
  ado/                        ADO abstraction layer
    client.go                 interface `Client` (Projects, Teams, Iterations, Boards, Query, Get, Update, Move, Reparent)
    sdk.go                    real implementation on azure-devops-go-api
    fake.go                   in-memory implementation for tests + `--demo` mode
    wiql.go                   query builders
    patch.go                  JSON-patch builders
    cache.go                  TTL cache for projects/teams/iterations/states
  model/                      pure domain types, no SDK types leak past here
    workitem.go               WorkItem{ID, Type, Title, State, AssignedTo, IterationPath, BoardColumn, Effort, Priority, ParentID, Rev…}
    tree.go                   Build(items, links) → []*Node; flatten with expand/collapse; stable ordering by backlog priority
    context.go                Context{Org, Project, Team, Iteration, Board}
  ui/
    app.go                    root tea.Model: context stack, active view, global keys, status bar, popups
    keymap.go                 all key bindings in one place (bubbles/key) so help + docs stay in sync
    theme.go                  lipgloss styles, colours by work-item type/state
    layout.go                 responsive split (left list / right detail), min-size guard
    components/
      statusbar.go            K9s-style header: org › project › team › sprint, hint line, spinner, last error
      cmdbar.go               `:` command bar with completion (`:sprint`, `:board`, `:dash`, `:proj`)
      picker.go               generic filterable list popup (sprints, teams, parents, states, users)
      confirm.go              y/n popup used before any write
      toast.go                transient messages
    views/
      dashboard.go            F6
      tree.go                 F2, F4, F5 — hierarchy list with expand/collapse + multi-select
      board.go                F1 — kanban columns
      detail.go               F3 — read-only detail pane
      edit.go                 F3 — form with textinput/textarea per field
```

Rule: `ui` never imports the SDK; it talks to `ado.Client` and receives `model.*` types. This keeps
views testable with `ado.Fake` and lets `--demo` run without credentials.

## 5. UI design

### 5.1 Global chrome

```
┌ devopstui ─ contoso › Platform › Team Blue › Sprint 42 (Sep 1 – Sep 14) ──── ⟳ 12s ago ┐
│ [1]Dashboard [2]Sprint [3]Board                                             ? help  : cmd │
├────────────────────────────────────┬──────────────────────────────────────────────────────┤
│  left: list / tree                 │  right: detail of highlighted item                   │
│                                    │                                                      │
├────────────────────────────────────┴──────────────────────────────────────────────────────┤
│ j/k move  enter open  space select  m move  e edit  p parent  s state  r refresh  q quit  │
└───────────────────────────────────────────────────────────────────────────────────────────┘
```

* Header shows the current **context** (K9s). Footer shows keys valid for the focused panel (Lazygit).
* `Tab` toggles focus between left and right panels; right panel is a scrollable `viewport`.
* Below ~100 columns the right panel collapses and `Enter` opens detail full-screen instead.

### 5.2 Context switching (F1)

* `:proj`, `:team`, `:sprint`, `:board` open a fuzzy picker; `Enter` sets the context and reloads.
* Shortcuts: `[` / `]` = previous / next sprint without opening a picker. `S` = jump to current sprint.
* Last-used context is persisted to config so start-up lands where you left off.
* Context is a stack: choosing a sprint from inside the board view returns you to the board view.

### 5.3 Global keys

| Key | Action |
|-----|--------|
| `1` `2` `3` | Dashboard / Sprint tree / Board |
| `:` | command bar |
| `/` | filter current list (fuzzy on title, id, assignee) |
| `r` | refresh current view; auto-refresh every 60 s (configurable, off by default when a popup is open) |
| `?` | full help overlay |
| `o` | open highlighted item in browser |
| `y` | yank item id / URL |
| `q` / `ctrl+c` | back / quit |

### 5.4 Sprint tree view (F2)

* Rows: `▸ EPIC  1234  Title…            State   Assignee   Effort` with indentation per level and
  type-coloured tags (Epic purple, Feature orange, PBI blue, Bug red, Task grey).
* `Enter`/`l` expand, `h` collapse, `L`/`H` expand/collapse all, `zz`-style: `g`/`G` top/bottom.
* Items whose parent is outside the sprint show the parent as a dimmed root so the hierarchy is
  always complete; toggle with `t` (tree / flat).
* Backlog mode (`:backlog`) shows the whole product backlog tree with the same widget, which is
  where cross-sprint drag-equivalents happen.

### 5.5 Detail and edit (F3)

* Right panel: all key fields, description (HTML → plain text via a small stripper), parent,
  children count, tags, last updated.
* Quick single-field edits without leaving the list (Lazygit style):
  `s` state picker, `a` assignee picker (team members), `i` iteration picker, `E` effort prompt,
  `P` priority prompt, `T` rename title inline.
* `e` opens the full edit form (title, state, assignee, iteration, effort, priority, description
  textarea). `ctrl+s` saves, `esc` cancels. Save sends one JSON patch and re-fetches the item.
* All writes go through a confirm popup unless `confirm_writes: false` in config.
* Conflict: if the patch fails the `/rev` test, show "item changed on server" and offer reload.

### 5.6 Move and multi-select (F4, F5)

* `space` toggles selection on the highlighted row; `v` starts a visual range; `ctrl+a` selects all
  visible; `esc` clears. Selected rows get a `●` marker and a count in the footer.
* `m` → iteration picker → confirm "Move 7 items to Sprint 43?" → parallel patches with a progress
  bar in the status bar → per-item failures listed in a popup, successes applied to the tree.
* `M` = move directly to next sprint (the F5 headline case), `B` = move to backlog root.
* `p` → parent picker (Epics + Features in the current project, fuzzy) → re-parent selection.
* Selecting a parent with `space` while in tree mode offers "include children" on move.
* Column moves in board view: `←`/`→` with `shift` change `System.BoardColumn` (state mapping
  comes from `GetBoard` column→state map).

### 5.7 Dashboard (F6)

Sections, each a collapsible list reusing the tree row renderer:

1. Assigned to me in the current sprint, grouped by state.
2. Assigned to me, active, in any other sprint or the backlog (things that fell out of view).
3. Recently updated by me (last 7 days).
4. Sprint burn summary line: counts by state + total effort remaining.

`Enter` jumps to the item in the Sprint view with the right context.

### 5.8 Board view (F1)

Kanban columns from `GetBoard`; horizontal scroll with `h`/`l` between columns, `j`/`k` within.
Cards show id, title, assignee initials, effort. Same edit/move keys as the tree.

## 6. State management

* Single root `tea.Model`. Each view is its own `tea.Model` embedded in a map; the root routes
  messages to the active view and to any open popup (popup gets keys first).
* Async work returns typed messages (`itemsLoadedMsg`, `updateDoneMsg{id, err}`, `errMsg`); views
  never call the API synchronously.
* A per-context in-memory store (`map[int]*model.WorkItem` + link table) is the source of truth for
  views; edits are applied optimistically and rolled back on error.
* Loading state per view with a `spinner`; stale data stays visible while refreshing.

## 7. Milestones

Each milestone ends with a runnable binary and tests.

| M | Deliverable | Key tasks |
|---|-------------|-----------|
| **M0 Scaffold** | `devopstui --demo` shows chrome with fake data | `go mod tidy` with deps above; `cmd/`, `internal/ui/app.go`, keymap, theme, statusbar, help; `ado.Client` interface + `fake.go`; config loader; Makefile / `go vet` / `golangci-lint` config. |
| **M1 Connect** | Real org loads, context picker works | `sdk.go` for projects/teams/iterations/boards/backlog config; PAT auth + friendly error on 401/403; `:proj :team :sprint` pickers; persist last context. **F1** |
| **M2 Tree** | Sprint hierarchy view | WIQL link query + batch fetch; `model/tree.go` with unit tests (orphans, cycles, parents outside sprint); tree widget with expand/collapse, filter, detail pane. **F2** |
| **M3 Edit** | Quick edits and full edit form | JSON-patch builders with tests; state/assignee/iteration pickers; edit form; confirm popup; rev conflict handling. **F3** |
| **M4 Move** | Single and bulk moves, re-parent | Selection model; `m`/`M`/`B`/`p`; errgroup runner with progress and failure report; include-children option. **F4, F5** |
| **M5 Dashboard** | `1` view | `@Me` queries, sections, jump-to-item. **F6** |
| **M6 Board** | `3` view | Board columns, card renderer, column moves. |
| **M7 Polish & release** | v0.1.0 | Auto-refresh, responsive layout, colour themes, `--demo` GIF for README, goreleaser (darwin/linux/windows), Homebrew tap. |

Suggested order is M0→M1→M2→M3→M4→M5→M6→M7; M5 and M6 are independent of each other.

### Status (2026-09-10)

An item details view (`D`) drills into one work item: metadata, rendered Markdown description,
and a kanban of its children by state, with child creation and state moves from inside it.

M0–M6 have a first implementation: demo mode, real SDK client, sprint tree with
selection, quick edits and the edit form, bulk moves with the children choice, re-parenting,
dashboard and board views. Remaining from the list above: auto-refresh is wired but off by
default, no colour themes, no release pipeline, and the SDK client has not yet been run against
a real organisation (M7 and the integration test).

## 8. Testing strategy

* `internal/model`: table-driven unit tests for tree building and flattening (the trickiest pure logic).
* `internal/ado`: tests for WIQL builders and patch builders (string/JSON golden files); one opt-in
  integration test gated by `ADO_INTEGRATION=1` that runs read-only against a real org.
* `internal/ui`: `teatest` golden renders of each view at 80×24 and 160×48 using `ado.Fake`;
  key-sequence tests for selection and move flows asserting the patches the fake received.
* CI: `go test ./...`, `go vet`, `golangci-lint`, on push.

## 9. Decisions and assumptions

* **PAT auth only in v1.** Entra/`az` CLI token support is a later addition; the connection type is
  isolated in `ado/sdk.go` so it is a one-file change.
* **Scrum and Agile processes** both supported by reading backlog configuration instead of
  hard-coding "Product Backlog Item"; Basic and CMMI should mostly work but are untested.
* **No drag-and-drop metaphor.** Moves are select → action → picker, which is faster on a keyboard
  and matches Lazygit.
* **Every write is confirmable and logged** to `~/.local/state/devopstui/audit.log` (id, field,
  old → new) so bulk mistakes can be traced and reverted by hand.
* **Single org per run.** Switching org is a restart with a different `--org`; multi-org can come
  from named contexts in config later.

## 10. Open questions (not blocking; defaults chosen)

| Question | Default |
|----------|---------|
| Should moving a Feature also move its child PBIs by default? | Ask via "include children" toggle in the confirm popup, default **on**. |
| Should closed items show in the sprint tree? | Hidden by default, `c` toggles. |
| Description editing: plain text or Markdown→HTML? | **Decided:** Markdown throughout. HTML→Markdown on read (`html-to-markdown`), Glamour rendering in the detail pane, Markdown→HTML on write (`goldmark`). Editing via `$EDITOR` on a temp file, with a built-in split editor + preview as fallback. |
| Should the dashboard span all projects or the current one? | Current project; `:dash all` later. |
