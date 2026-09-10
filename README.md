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
You can also put `org`, `pat`, `confirm_writes: true` and `refresh_seconds: 60` in that file.

## Keys

Keys are vim-style mnemonics: the letter is the first letter of the action.

| Navigate | | Views | | Change | | Move | |
|---|---|---|---|---|---|---|---|
| `j` `k` | down / up | `1` | dashboard | `e` | edit form | `m` | move to sprint… |
| `g` `G` | top / bottom | `2` | sprint tree | `t` | title | `M` | move to next sprint |
| `l` `h` | expand / collapse | `3` | board | `s` | state | `B` | move to backlog |
| `L` `H` | expand / collapse all | `4` | backlog | `a` | assign | `p` | set parent |
| `tab` | focus detail pane | `[` `]` | prev / next sprint | `E` | effort | | |
| `/` | filter | `S` | current sprint | `P` | priority | | |
| `space` | select | `:` | command bar | `o` | open in browser | | |
| `v` | visual select | `r` | refresh | `y` | yank id | | |
| `ctrl+a` | select all | `f` | flat / tree | `?` | help | | |
| `esc` | clear selection | `c` | show closed | `q` | back / quit | | |

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
