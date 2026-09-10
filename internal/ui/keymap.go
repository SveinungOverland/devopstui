package ui

import "github.com/charmbracelet/bubbles/key"

// keymap is the single source of truth for bindings. Letters are chosen as
// vim-style mnemonics: the key is the first letter of the action word.
type keymap struct {
	// navigation
	Up, Down, Top, Bottom, PageUp, PageDown key.Binding
	Collapse, Expand, CollapseAll, ExpandAll key.Binding
	Focus, Preview, Details                 key.Binding
	// views
	Dashboard, Sprint, Board, Backlog key.Binding
	PrevSprint, NextSprint, CurSprint key.Binding
	TeamFilter                        key.Binding
	Command, Filter, Help, Refresh    key.Binding
	Back, Quit                        key.Binding
	// selection
	Select, Visual, SelectAll, ClearSel key.Binding
	// actions
	Edit, Title, Desc, State, Assign, Iteration, Effort, Priority, New key.Binding
	Move, MoveNext, MoveBacklog, Parent                    key.Binding
	Open, Yank, Flat, Closed                               key.Binding
	// board
	Left, Right, ColLeft, ColRight key.Binding
}

func b(help string, keys ...string) key.Binding {
	return key.NewBinding(key.WithKeys(keys...), key.WithHelp(keys[0], help))
}

var keys = keymap{
	Up:          b("up", "k", "up"),
	Down:        b("down", "j", "down"),
	Top:         b("top", "g", "home"),
	Bottom:      b("bottom", "G", "end"),
	PageUp:      b("page up", "ctrl+u", "pgup"),
	PageDown:    b("page down", "ctrl+d", "pgdown"),
	Collapse:    b("collapse", "h", "left"),
	Expand:      b("expand", "l", "right", "enter"),
	CollapseAll: b("collapse all", "H"),
	ExpandAll:   b("expand all", "L"),
	Focus:       b("focus detail", "tab"),
	Preview:     b("toggle preview", "z"),
	Details:     b("details", "D"),

	Dashboard:  b("dashboard", "1"),
	Sprint:     b("sprint", "2"),
	Board:      b("board", "3"),
	Backlog:    b("backlog", "4"),
	PrevSprint: b("prev sprint", "["),
	NextSprint: b("next sprint", "]"),
	CurSprint:  b("current sprint", "S"),
	TeamFilter: b("team filter", "T"),
	Command:    b("command", ":"),
	Filter:     b("filter", "/"),
	Help:       b("help", "?"),
	Refresh:    b("refresh", "r"),
	Back:       b("back", "esc"),
	Quit:       b("quit", "q", "ctrl+c"),

	Select:    key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "select")),
	Visual:    b("visual select", "v"),
	SelectAll: b("select all", "ctrl+a"),
	ClearSel:  b("clear selection", "esc"),

	Edit:      b("edit", "e"),
	Title:     b("title", "t"),
	Desc:      b("description", "d"),
	State:     b("state", "s"),
	Assign:    b("assign", "a"),
	Iteration: b("iteration", "i"),
	Effort:    b("effort", "E"),
	Priority:  b("priority", "P"),
	New:       b("new child", "n"),

	Move:        b("move to sprint", "m"),
	MoveNext:    b("move to next sprint", "M"),
	MoveBacklog: b("move to backlog", "B"),
	Parent:      b("parent", "p"),

	Open:   b("open in browser", "o"),
	Yank:   b("yank id", "y"),
	Flat:   b("flat/tree", "f"),
	Closed: b("show closed", "c"),

	Left:     b("left", "h", "left"),
	Right:    b("right", "l", "right"),
	ColLeft:  b("move column left", "H"),
	ColRight: b("move column right", "L"),
}

// helpGroups drive both the footer hints and the ? overlay.
var helpGroups = [][]key.Binding{
	{keys.Up, keys.Down, keys.Top, keys.Bottom, keys.Expand, keys.Collapse, keys.ExpandAll, keys.CollapseAll, keys.Focus, keys.Preview, keys.Details},
	{keys.Dashboard, keys.Sprint, keys.Board, keys.Backlog, keys.PrevSprint, keys.NextSprint, keys.CurSprint, keys.TeamFilter, keys.Command, keys.Filter, keys.Refresh},
	{keys.Select, keys.Visual, keys.SelectAll, keys.ClearSel},
	{keys.New, keys.Edit, keys.Title, keys.Desc, keys.State, keys.Assign, keys.Iteration, keys.Effort, keys.Priority},
	{keys.Move, keys.MoveNext, keys.MoveBacklog, keys.Parent},
	{keys.Open, keys.Yank, keys.Flat, keys.Closed, keys.Help, keys.Quit},
}

var footerTree = []key.Binding{keys.Expand, keys.Select, keys.Details, keys.New, keys.Edit, keys.Desc, keys.State, keys.Assign, keys.Move, keys.Parent, keys.Filter, keys.Help}
var footerBoard = []key.Binding{keys.Left, keys.Right, keys.ColLeft, keys.ColRight, keys.Select, keys.Details, keys.Edit, keys.State, keys.Move, keys.Preview, keys.Help}

// footerItemDesc and footerItemKanban are the two focus modes of the
// drill-down view.
var footerItemDesc = []key.Binding{keys.Focus, keys.Desc, keys.New, keys.Edit, keys.Title, keys.State, keys.Assign, keys.Preview, keys.Back, keys.Help}
var footerItemKanban = []key.Binding{keys.Left, keys.Right, keys.Up, keys.Down, keys.ColLeft, keys.ColRight, keys.New, keys.Details, keys.Edit, keys.State, keys.Focus, keys.Back}
