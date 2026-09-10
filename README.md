# devopstui

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
l expand  space select  e edit  s state  a assign  m move to sprint  p parent  / filter
```

## Run

```bash
go run ./cmd/devopstui --demo
```

Against a real organisation you need a personal access token with *Work Items (read & write)*
and *Project & Team (read)* scopes:

```bash
export AZURE_DEVOPS_ORG_URL=https://dev.azure.com/<org>
export AZURE_DEVOPS_EXT_PAT=<pat>
go run ./cmd/devopstui --project MyProject --team "My Team"
```

Project and team are remembered in `~/.config/devopstui/config.yaml` (or `$DEVOPSTUI_CONFIG`).
You can also put `org`, `pat`, `confirm_writes: true`, `refresh_seconds: 60` and `editor: nvim`
in that file.

## Descriptions are Markdown

Azure DevOps stores descriptions as HTML. The TUI converts them to Markdown on read, renders
them with [Glamour](https://github.com/charmbracelet/glamour) in the detail pane, and converts
back to HTML on save. Headings, lists, task lists, quotes, code blocks, tables and links all
survive the round trip.

Press `d` on an item to edit its description:

- With `$VISUAL` or `$EDITOR` set (or `editor:` in the config), the description opens in that
  editor on a temporary `.md` file. Save and quit to apply, quit without changes to cancel.
  This is the Lazygit way and gives you your own keybindings, spell check and plugins.
- Otherwise the built-in editor opens: line-numbered text on the left, live rendered preview on
  the right. It is modal like vim and starts in **normal** mode:

  | Normal mode | | Insert mode | |
  |---|---|---|---|
  | `j` `k` | line down / up | `esc` | back to normal mode |
  | `h` `l` `0` `$` | move within the line | `ctrl+s` | save |
  | `g` `G` | top / bottom | | |
  | `space` | toggle `- [ ]` / `- [x]` on the line (a plain list item gains a checkbox) | | |
  | `i` `a` `A` | insert at cursor / after cursor / end of line | | |
  | `o` `O` | open a line below / above | | |
  | `dd` `yy` | cut / copy the line | | |
  | `p` `P` | paste the line below / above | | |
  | `ctrl+s` | save | | |
  | `q` | close (press twice to discard unsaved changes) | | |
  | `ctrl+p` `ctrl+d` `ctrl+u` | toggle / scroll the preview | | |

  Set `editor: inline` to force this editor even when `$EDITOR` is set.

The Description field in the `e` edit form uses the same editor.

## Keys

Keys are vim-style mnemonics: the letter is the first letter of the action.

| Navigate | | Views | | Change | | Move | |
|---|---|---|---|---|---|---|---|
| `j` `k` | down / up | `1` | dashboard | `e` | edit form | `m` | move to sprint… |
| `g` `G` | top / bottom | `2` | sprint tree | `t` | title | `M` | move to next sprint |
| `l` `h` | expand / collapse | `3` | board | `d` | description | `B` | move to backlog |
| `L` `H` | expand / collapse all | `4` | backlog | `s` | state | `p` | set parent |
| `tab` | focus detail pane | `[` `]` | prev / next sprint | `a` | assign | | |
| `/` | filter | `S` | current sprint | `E` | effort | | |
| `space` | select | `:` | command bar | `P` | priority | | |
| `v` | visual select | `r` | refresh | `o` | open in browser | | |
| `ctrl+a` | select all | `f` | flat / tree | `y` | yank id | | |
| `esc` | clear selection | `c` | show closed | `?` `q` | help / quit | | |

Any change key acts on the **selection** when there is one, otherwise on the highlighted
item. Bulk changes and re-parenting ask for confirmation. Moving a Feature or Epic offers
to bring its children along.

On the board, `h`/`l` move between columns and `H`/`L` move the card to the neighbouring
column.

Commands: `:sprint [name]`, `:team`, `:project`, `:board`, `:backlog`, `:dash`, `:refresh`,
`:<id>` to look up a work item, `:q`.

## Develop

```bash
make test        # unit + rendered-frame tests with the in-memory fake
make dumps       # writes rendered frames to ./dumps for eyeballing
```
