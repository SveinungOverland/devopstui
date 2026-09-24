package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/sveinungoverland/devopstui/internal/ado"
	"github.com/sveinungoverland/devopstui/internal/model"
)

// activity is the Activity tab: a flat feed of recently changed work items,
// newest change first, with a marker line where the entries changed since
// the last visit end.
type activity struct {
	all      []*model.WorkItem // as loaded, newest change first
	rows     []*model.WorkItem // all after include
	loaded   bool              // a fetch has landed, even an empty one
	cursor   int
	offset   int // first display line on screen; the marker takes a line too
	selected map[int]bool
	// include narrows the feed (team, sprint, involved); nil shows all.
	include func(*model.WorkItem) bool
	// since is where the marker goes: rows changed after it are new. Zero
	// means no marker (the first visit ever).
	since time.Time
}

func newActivity() *activity { return &activity{selected: map[int]bool{}} }

// setItems replaces the feed and re-applies the filter, keeping the cursor
// on the same item when it is still there.
func (f *activity) setItems(items []*model.WorkItem) {
	f.all = items
	f.loaded = true
	f.rebuild()
}

// rebuild re-applies include over all, keeping the cursor on the same item.
func (f *activity) rebuild() {
	cur := f.current()
	var rows []*model.WorkItem
	for _, it := range f.all {
		if f.include == nil || f.include(it) {
			rows = append(rows, it)
		}
	}
	f.rows = rows
	for id := range f.selected {
		if !f.has(id) {
			delete(f.selected, id)
		}
	}
	if cur == nil || !f.jumpTo(cur.ID) {
		f.clamp()
	}
}

func (f *activity) has(id int) bool {
	for _, it := range f.rows {
		if it.ID == id {
			return true
		}
	}
	return false
}

func (f *activity) clamp() {
	f.cursor = max(min(f.cursor, len(f.rows)-1), 0)
}

func (f *activity) current() *model.WorkItem {
	if f.cursor < 0 || f.cursor >= len(f.rows) {
		return nil
	}
	return f.rows[f.cursor]
}

func (f *activity) move(delta int) {
	f.cursor += delta
	f.clamp()
}

// jumpTo puts the cursor on id and reports whether it is in the feed.
func (f *activity) jumpTo(id int) bool {
	for i, it := range f.rows {
		if it.ID == id {
			f.cursor = i
			return true
		}
	}
	return false
}

func (f *activity) toggleSelect() {
	if it := f.current(); it != nil {
		if f.selected[it.ID] {
			delete(f.selected, it.ID)
		} else {
			f.selected[it.ID] = true
		}
		f.move(1)
	}
}

// targetItems is the selection, in feed order, or the highlighted item.
func (f *activity) targetItems() []*model.WorkItem {
	if len(f.selected) > 0 {
		var out []*model.WorkItem
		for _, it := range f.rows {
			if f.selected[it.ID] {
				out = append(out, it)
			}
		}
		return out
	}
	if it := f.current(); it != nil {
		return []*model.WorkItem{it}
	}
	return nil
}

// apply swaps in an updated item after a write. Its change is now the
// newest, so the feed is re-sorted and the cursor follows it.
func (f *activity) apply(updated *model.WorkItem) {
	found := false
	for i, it := range f.all {
		if it.ID == updated.ID {
			f.all[i] = updated
			found = true
		}
	}
	if !found {
		return
	}
	sort.SliceStable(f.all, func(i, j int) bool { return f.all[i].ChangedDate.After(f.all[j].ChangedDate) })
	f.rebuild()
}

// newCount is how many rows changed since the last visit. The rows are
// newest first, so they are a prefix.
func (f *activity) newCount() int {
	if f.since.IsZero() {
		return 0
	}
	n := 0
	for _, it := range f.rows {
		if !it.ChangedDate.After(f.since) {
			break
		}
		n++
	}
	return n
}

// hasMarker reports whether the marker line is drawn: whenever something is
// new. When everything is, the line closes the feed.
func (f *activity) hasMarker() bool { return f.newCount() > 0 }

// line is the display line of row i, counting the marker line.
func (f *activity) line(i int) int {
	if f.hasMarker() && i >= f.newCount() {
		return i + 1
	}
	return i
}

// The feed's columns past these widths: the changer's full name instead of
// initials, then the assignee as well.
const (
	activityWideW  = 90
	activityWiderW = 140
)

func (f *activity) view(width, height int) string {
	if len(f.rows) == 0 {
		msg := fmt.Sprintf("nothing changed in the last %d days", ado.ActivityDays)
		if len(f.all) > 0 {
			msg = "nothing in the feed matches the filters (I involved, A sprint, T team)"
		}
		if !f.loaded {
			msg = "loading…"
		}
		return sMuted.Render(msg)
	}
	nNew := f.newCount()
	marker := f.hasMarker()
	total := len(f.rows)
	if marker {
		total++
	}
	cl := f.line(f.cursor)
	if cl < f.offset {
		f.offset = cl
	}
	if cl >= f.offset+height {
		f.offset = cl - height + 1
	}
	// Keep the marker on screen with the first row below it: scrolling up
	// to row nNew should show why it is where it is.
	if marker && f.cursor == nNew && f.offset == cl {
		f.offset = max(cl-1, 0)
	}
	f.offset = max(min(f.offset, total-height), 0)

	// Columns: marker(2) tag(5) id(6) title(*) state(12) by(whoW) [→ assignee] ago(9)
	stateW, whoW, agoW, assignW := 12, 2, 9, 0
	if width >= activityWideW {
		whoW = 18
	}
	if width >= activityWiderW {
		assignW = 20
	}
	var lines []string
	for ln := f.offset; ln < f.offset+height && ln < total; ln++ {
		i := ln
		if marker && ln == nNew {
			lines = append(lines, f.markerLine(nNew, width))
			continue
		}
		if marker && ln > nNew {
			i = ln - 1
		}
		it := f.rows[i]
		cur := i == f.cursor
		st := rowStyler(cur)
		plain := st(lipgloss.NewStyle())
		muted := st(sMuted)

		mark := "  "
		switch {
		case cur && f.selected[it.ID]:
			mark = st(sKey).Render(cursorMark) + st(sSelected).Render("●")
		case cur:
			mark = st(sKey).Render(cursorMark) + plain.Render(" ")
		case f.selected[it.ID]:
			mark = sSelected.Render("● ")
		}
		tag := st(kindStyle(it.Kind)).Render(pad(it.Kind.Tag(), 4))
		id := muted.Render(fmt.Sprintf("%5d", it.ID))
		titleW := width - 2 - 5 - 6 - stateW - 1 - whoW - 1 - agoW - 1
		if assignW > 0 {
			titleW -= assignW + 1
		}
		titleW = min(titleW, listTitleMaxW)
		badge := ""
		if it.Rev == 1 {
			badge = " " + st(sOK).Render("created")
		}
		title := plain.Render(trunc(it.Title, titleW-lipgloss.Width(badge))) + badge
		title += fill(plain, titleW-lipgloss.Width(title))
		state := st(stateStyle(it.State)).Render(pad(trunc(it.State, stateW), stateW))
		by := muted.Render(initials(it.ChangedBy))
		if whoW > 2 {
			by = muted.Render(pad(trunc(it.ChangedBy, whoW), whoW))
		}
		when := muted.Render(padLeft(ago(it.ChangedDate), agoW))
		if i < nNew {
			when = st(sKey).Render(padLeft(ago(it.ChangedDate), agoW))
		}
		sp := plain.Render(" ")
		line := mark + tag + sp + id + sp + title + sp + state + sp + by
		if assignW > 0 {
			line += sp + muted.Render(pad(trunc("→ "+it.Assignee(), assignW), assignW))
		}
		line += sp + when
		line += fill(plain, width-lipgloss.Width(line))
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

// markerLine separates what changed since the last visit from the rest.
func (f *activity) markerLine(nNew, width int) string {
	label := fmt.Sprintf(" ▲ %s since you last looked, %s ", plural(nNew, "change"), f.since.Local().Format("Mon Jan 2 15:04"))
	rule := max(width-lipgloss.Width(label)-2, 0)
	return sSelected.Render("──" + label + strings.Repeat("─", rule))
}

// summary is the header's right-hand status: position, new count, scope.
func (f *activity) summary(scope string) string {
	s := fmt.Sprintf("%d/%d", min(f.cursor+1, len(f.rows)), len(f.rows))
	if n := f.newCount(); n > 0 {
		s += sSelected.Render(fmt.Sprintf(" · %d new", n))
	}
	if len(f.selected) > 0 {
		s += sSelected.Render(fmt.Sprintf(" · %d selected", len(f.selected)))
	}
	if scope != "" {
		s += sMuted.Render(" · " + scope)
	}
	return s
}
