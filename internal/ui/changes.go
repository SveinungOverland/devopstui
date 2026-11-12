package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"

	"github.com/sveinungoverland/devopstui/internal/model"
)

// periodKind is how far back the Changes view looks.
type periodKind int

const (
	periodToday periodKind = iota
	// periodYesterday reaches back to the previous working day, so on a
	// Monday it covers Friday: what a stand-up asks about.
	periodYesterday
	periodWeek
	// periodSprint is the selected sprint, from its first day to its last.
	periodSprint
	// periodDays is a custom number of days, set with :changes <n>.
	periodDays
)

// periodCycle is the order h and l step through.
var periodCycle = []periodKind{periodToday, periodYesterday, periodWeek, periodSprint}

type changePeriod struct {
	kind periodKind
	days int // periodDays only
}

// step moves d places along periodCycle, stopping at the ends. A custom
// number of days sits between the fixed periods it falls among.
func (p changePeriod) step(d int) changePeriod {
	i := 0
	switch {
	case p.kind == periodDays && d > 0:
		// The next longer fixed period: a week, unless that is shorter.
		i = 2
		if p.days >= 7 {
			i = 3
		}
		return changePeriod{kind: periodCycle[i]}
	case p.kind == periodDays:
		i = 1
		if p.days > 7 {
			i = 2
		}
		return changePeriod{kind: periodCycle[i]}
	}
	for j, k := range periodCycle {
		if k == p.kind {
			i = j
		}
	}
	return changePeriod{kind: periodCycle[min(max(i+d, 0), len(periodCycle)-1)]}
}

// window is the span of time the period covers at now. A zero until means
// it is still open. Day periods start at local midnight, so the answer
// does not drift while the view is open.
func (p changePeriod) window(now time.Time, sprint model.Iteration) (since, until time.Time) {
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch p.kind {
	case periodToday:
		return midnight, time.Time{}
	case periodYesterday:
		return prevWorkday(midnight), time.Time{}
	case periodWeek:
		return midnight.AddDate(0, 0, -6), time.Time{}
	case periodDays:
		return midnight.AddDate(0, 0, -(max(p.days, 1) - 1)), time.Time{}
	}
	if sprint.Start.IsZero() {
		// "Unscheduled", or a sprint nobody gave dates: a sprint's worth.
		return midnight.AddDate(0, 0, -13), time.Time{}
	}
	since = localDate(sprint.Start, now.Location())
	if !sprint.Finish.IsZero() {
		if end := localDate(sprint.Finish, now.Location()).AddDate(0, 0, 1); end.Before(now) {
			until = end
		}
	}
	return since, until
}

// localDate is midnight in loc on the calendar date t names. Sprint dates
// are dates, sent as midnight UTC; read in a zone west of that they would
// otherwise start the evening before.
func localDate(t time.Time, loc *time.Location) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

// prevWorkday is midnight on the last weekday before the given midnight.
func prevWorkday(midnight time.Time) time.Time {
	d := midnight.AddDate(0, 0, -1)
	for d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
		d = d.AddDate(0, 0, -1)
	}
	return d
}

// label names the period for the header and the empty state.
func (p changePeriod) label(now time.Time) string {
	switch p.kind {
	case periodToday:
		return "today"
	case periodYesterday:
		since, _ := p.window(now, model.Iteration{})
		if now.Sub(since) > 48*time.Hour {
			return "since " + since.Weekday().String()
		}
		return "since yesterday"
	case periodWeek:
		return "last 7 days"
	case periodDays:
		return fmt.Sprintf("last %s", plural(max(p.days, 1), "day"))
	}
	return "this sprint"
}

// changeGroup is what the Changes view's headings are.
type changeGroup int

const (
	groupDay changeGroup = iota
	groupItem
	groupPerson
)

func (g changeGroup) String() string {
	return [...]string{"by day", "by item", "by person"}[g]
}

// changeEntry is one row: a revision and the item it belongs to.
type changeEntry struct {
	c  model.Change
	it *model.WorkItem
}

// changes is the Changes view: what was done to work items over a period,
// one line per revision, under a heading per day, item or person.
type changes struct {
	period changePeriod
	// allSprints widens the view from the selected sprint's items to the
	// whole project.
	allSprints bool
	group      changeGroup

	set    model.ChangeSet
	items  map[int]*model.WorkItem
	loaded bool
	// key identifies the query the loaded data answers; it differs from
	// the wanted one after a period, sprint or filter change.
	key string

	// entries are the rows shown, in screen order; keys[i] is entry i's
	// group, a heading going wherever it changes.
	entries []changeEntry
	keys    []string
	cursor  int
	whenW   int // width of the widest time shown, so the rows line up

	filter    textinput.Model
	filtering bool

	now func() time.Time
}

func newChanges() *changes {
	in := textinput.New()
	in.Prompt = "/"
	return &changes{period: changePeriod{kind: periodYesterday}, filter: in, items: map[int]*model.WorkItem{}, now: time.Now}
}

// setData replaces the loaded changes, keeping the cursor on the same
// revision when it is still there.
func (t *changes) setData(set model.ChangeSet, key string) {
	cur, had := t.currentEntry()
	t.set, t.key, t.loaded = set, key, true
	t.items = make(map[int]*model.WorkItem, len(set.Items))
	for _, it := range set.Items {
		t.items[it.ID] = it
	}
	t.build()
	if had {
		for i, e := range t.entries {
			if e.c.ItemID == cur.c.ItemID && e.c.At.Equal(cur.c.At) {
				t.cursor = i
				break
			}
		}
	}
	t.clamp()
}

// apply swaps in a newer revision of an item after a write.
func (t *changes) apply(it *model.WorkItem) {
	if _, ok := t.items[it.ID]; !ok {
		return
	}
	t.items[it.ID] = it
	for i, s := range t.set.Items {
		if s.ID == it.ID {
			t.set.Items[i] = it
		}
	}
	for i := range t.entries {
		if t.entries[i].it.ID == it.ID {
			t.entries[i].it = it
		}
	}
}

// build lays the changes out as rows: filtered, then ordered so each
// group's rows sit together.
func (t *changes) build() {
	q := strings.ToLower(strings.TrimSpace(t.filter.Value()))
	t.entries, t.keys = t.entries[:0], t.keys[:0]
	for _, c := range t.set.Changes {
		it, ok := t.items[c.ItemID]
		if !ok {
			continue
		}
		if q != "" && !fuzzy(strings.ToLower(fmt.Sprintf("%d %s %s %s %s", it.ID, it.Title, it.Type, c.By, c.Text())), q) {
			continue
		}
		t.entries = append(t.entries, changeEntry{c: c, it: it})
	}
	// The changes arrive newest first. Groups keep that order inside, and
	// rank among themselves by their newest change (days and items) or by
	// how much they did (people).
	rank := map[string]int{}
	count := map[string]int{}
	for _, e := range t.entries {
		k := t.groupKey(e)
		if _, ok := rank[k]; !ok {
			rank[k] = len(rank)
		}
		count[k]++
	}
	if t.group == groupPerson {
		names := make([]string, 0, len(rank))
		for k := range rank {
			names = append(names, k)
		}
		sort.Slice(names, func(i, j int) bool {
			if count[names[i]] != count[names[j]] {
				return count[names[i]] > count[names[j]]
			}
			return strings.ToLower(names[i]) < strings.ToLower(names[j])
		})
		for i, k := range names {
			rank[k] = i
		}
	}
	sort.SliceStable(t.entries, func(i, j int) bool {
		return rank[t.groupKey(t.entries[i])] < rank[t.groupKey(t.entries[j])]
	})
	t.whenW = 0
	for _, e := range t.entries {
		t.keys = append(t.keys, t.groupKey(e))
		t.whenW = max(t.whenW, lipgloss.Width(t.when(e.c.At)))
	}
	t.clamp()
}

func (t *changes) groupKey(e changeEntry) string {
	switch t.group {
	case groupItem:
		return fmt.Sprint(e.it.ID)
	case groupPerson:
		return e.c.By
	}
	return e.c.At.In(t.now().Location()).Format("2006-01-02")
}

func (t *changes) clamp() {
	t.cursor = min(max(t.cursor, 0), max(len(t.entries)-1, 0))
}

func (t *changes) currentEntry() (changeEntry, bool) {
	if t.cursor < 0 || t.cursor >= len(t.entries) {
		return changeEntry{}, false
	}
	return t.entries[t.cursor], true
}

func (t *changes) current() *model.WorkItem {
	if e, ok := t.currentEntry(); ok {
		return e.it
	}
	return nil
}

func (t *changes) move(d int) {
	t.cursor += d
	t.clamp()
}

// moveGroup jumps to the first row of the next (d > 0) or previous group.
func (t *changes) moveGroup(d int) {
	if len(t.entries) == 0 {
		return
	}
	i := t.cursor
	for i > 0 && t.keys[i-1] == t.keys[i] {
		i-- // the first row of this group
	}
	if d > 0 {
		k := t.keys[i]
		for i < len(t.entries) && t.keys[i] == k {
			i++
		}
		if i == len(t.entries) {
			return
		}
	} else {
		if i == 0 {
			t.cursor = 0
			return
		}
		i--
		for i > 0 && t.keys[i-1] == t.keys[i] {
			i--
		}
	}
	t.cursor = i
}

// jumpTo puts the cursor on the item's newest change.
func (t *changes) jumpTo(id int) bool {
	best := -1
	for i, e := range t.entries {
		if e.it.ID == id && (best < 0 || e.c.At.After(t.entries[best].c.At)) {
			best = i
		}
	}
	if best < 0 {
		return false
	}
	t.cursor = best
	return true
}

// itemChanges are the loaded changes to one item, newest first, whether
// or not the filter shows them.
func (t *changes) itemChanges(id int) []model.Change {
	var out []model.Change
	for _, c := range t.set.Changes {
		if c.ItemID == id {
			out = append(out, c)
		}
	}
	return out
}

// shown are the changes on screen, for the tally.
func (t *changes) shown() []model.Change {
	out := make([]model.Change, len(t.entries))
	for i, e := range t.entries {
		out[i] = e.c
	}
	return out
}

// scope names what the view covers, for the header.
func (t *changes) scope() string {
	s := t.period.label(t.now())
	if t.allSprints {
		s += " · all sprints"
	}
	s += " · " + t.group.String()
	if q := strings.TrimSpace(t.filter.Value()); q != "" {
		s += " · /" + q
	}
	return s
}

// tallyLine sums up what is shown, and says when it is not everything.
func (t *changes) tallyLine(width int) string {
	if !t.loaded {
		return pad("", width)
	}
	n := model.TallyChanges(t.shown())
	num := func(n int, noun string) string {
		p := plural(n, noun)
		i := strings.IndexByte(p, ' ')
		return sKey.Render(p[:i]) + p[i:]
	}
	parts := []string{num(n.Changes, "change") + " on " + num(n.Items, "item")}
	comments := "comments"
	if n.Comments == 1 {
		comments = "comment"
	}
	for _, f := range []struct {
		n    int
		text string
	}{
		{n.Created, "created"}, {n.Completed, "completed"}, {n.StateMoves, "moved state"},
		{n.Reassigned, "reassigned"}, {n.Moved, "moved sprint"}, {n.Comments, comments},
	} {
		if f.n > 0 {
			parts = append(parts, sKey.Render(fmt.Sprint(f.n))+" "+f.text)
		}
	}
	s := strings.Join(parts, sMuted.Render(" · "))
	if t.set.Truncated {
		s += sMuted.Render(" · ") + sSelected.Render("only the most recently changed items")
	}
	if t.set.Failed > 0 {
		s += sMuted.Render(" · ") + sErr.Render(fmt.Sprintf("history of %s not read", plural(t.set.Failed, "item")))
	}
	return pad(s, width)
}

// when formats a change's time for a row: the time alone under a day
// heading, otherwise with as much of the date as the period needs.
func (t *changes) when(at time.Time) string {
	now := t.now()
	at = at.In(now.Location())
	if t.group == groupDay {
		return at.Format("15:04")
	}
	y1, m1, d1 := now.Date()
	y2, m2, d2 := at.Date()
	switch {
	case y1 == y2 && m1 == m2 && d1 == d2:
		return at.Format("15:04")
	case now.Sub(at) < 6*24*time.Hour:
		return at.Format("Mon 15:04")
	}
	return at.Format("02 Jan 15:04")
}

func (t *changes) view(width, height int, focused bool, loading bool) string {
	style := sPanel
	if focused {
		style = sPanelFocus
	}
	w := max(width-4, 20)
	innerH := max(height-2, 3)
	var lines []string
	curLine, headLine := 0, 0
	for i, e := range t.entries {
		if i == 0 || t.keys[i] != t.keys[i-1] {
			if len(lines) > 0 {
				lines = append(lines, "")
			}
			if i <= t.cursor {
				headLine = len(lines)
			}
			lines = append(lines, t.heading(i, w))
		}
		if i == t.cursor {
			curLine = len(lines)
		}
		lines = append(lines, t.renderEntry(e, w, i == t.cursor && focused))
	}
	if len(lines) == 0 {
		lines = t.emptyLines(loading)
	}
	// Scrolled, the cursor's group starts at the top when it fits, so each
	// day or person comes into view whole.
	start := 0
	if len(lines) > innerH {
		start = curLine - innerH + 1
		if curLine-headLine < innerH {
			start = headLine
		}
		start = max(min(start, len(lines)-innerH), 0)
	}
	end := min(start+innerH, len(lines))
	return style.Width(width - 2).Height(innerH).Render(strings.Join(lines[start:end], "\n"))
}

func (t *changes) emptyLines(loading bool) []string {
	switch {
	case !t.loaded && loading:
		return []string{sMuted.Render("reading what changed…")}
	case !t.loaded:
		return []string{sMuted.Render("changes not loaded; r loads them")}
	case strings.TrimSpace(t.filter.Value()) != "":
		return []string{sMuted.Render("no change matches the filter")}
	}
	hint := "l looks further back"
	if !t.allSprints {
		hint += " · A includes every sprint"
	}
	return []string{sMuted.Render("nothing changed " + t.period.label(t.now())), "", sMuted.Render(hint)}
}

// heading heads the group that starts at entry i.
func (t *changes) heading(i, w int) string {
	n := 0
	items := map[int]bool{}
	for j := i; j < len(t.entries) && t.keys[j] == t.keys[i]; j++ {
		n++
		items[t.entries[j].it.ID] = true
	}
	e := t.entries[i]
	bold := lipgloss.NewStyle().Bold(true)
	if t.cursor >= i && t.cursor < i+n {
		bold = sHeader
	}
	switch t.group {
	case groupItem:
		it := e.it
		head := kindStyle(it.Kind).Render(fmt.Sprintf("%-4s", it.Kind.Tag())) + sMuted.Render(fmt.Sprintf(" %d ", it.ID))
		tail := "  " + stateStyle(it.State).Render(it.State) + sMuted.Render("  "+it.Assignee()+"  "+plural(n, "change"))
		return head + bold.Render(trunc(it.Title, max(w-lipgloss.Width(head)-lipgloss.Width(tail), 8))) + tail
	case groupPerson:
		name := e.c.By
		if name == "" {
			name = "Unknown"
		}
		return trunc(bold.Render(name)+sMuted.Render(fmt.Sprintf("  %s · %s", plural(n, "change"), plural(len(items), "item"))), w)
	}
	return trunc(bold.Render(t.dayName(e.c.At))+sMuted.Render(fmt.Sprintf("  %s · %s", plural(n, "change"), plural(len(items), "item"))), w)
}

// dayName is a day heading: Today and Yesterday by name, then the date.
func (t *changes) dayName(at time.Time) string {
	now := t.now()
	at = at.In(now.Location())
	day := func(x time.Time) time.Time { return time.Date(x.Year(), x.Month(), x.Day(), 0, 0, 0, 0, x.Location()) }
	date := at.Format("Mon 2 Jan")
	switch day(now).Sub(day(at)).Round(24*time.Hour) / (24 * time.Hour) {
	case 0:
		return "Today · " + date
	case 1:
		return "Yesterday · " + date
	}
	return date
}

// renderEntry is one revision on one line: when, which item (unless the
// heading says), what changed, and who did it (unless the heading says).
func (t *changes) renderEntry(e changeEntry, w int, cur bool) string {
	st := rowStyler(cur)
	plain := st(lipgloss.NewStyle())
	muted := st(sMuted)
	mark := plain.Render(" ")
	if cur {
		mark = st(sKey).Render(cursorMark)
	}
	left := mark + muted.Render(padLeft(t.when(e.c.At), t.whenW)) + plain.Render("  ")
	if t.group != groupItem {
		itemW := min(max(w*2/5, 22), 52)
		head := st(kindStyle(e.it.Kind)).Render(fmt.Sprintf("%-4s", e.it.Kind.Tag())) + muted.Render(fmt.Sprintf(" %d ", e.it.ID))
		head += plain.Render(trunc(e.it.Title, max(itemW-lipgloss.Width(head), 4)))
		left += head + fill(plain, itemW-lipgloss.Width(head)) + plain.Render("  ")
	}
	var right []string
	rw := 0
	if t.group != groupPerson {
		right = append(right, muted.Render(initials(e.c.By)))
		rw = 3
	}
	parts := make([]string, len(e.c.Fields))
	for i, f := range e.c.Fields {
		parts[i] = renderFieldChange(f, st)
	}
	text := strings.Join(parts, muted.Render(" · "))
	left += trunc(text, max(w-lipgloss.Width(left)-rw, 4))
	return spread(plain, left, w, right...)
}

// renderFieldChange styles one field's change. st adds the cursor row's
// background to every piece.
func renderFieldChange(f model.FieldChange, st func(lipgloss.Style) lipgloss.Style) string {
	plain, muted := st(lipgloss.NewStyle()), st(sMuted)
	arrow := muted.Render(" → ")
	switch {
	case f.Kind == model.ChangeCreated:
		return st(sOK).Render("created")
	case f.Kind == model.ChangeComment:
		return muted.Render("comment ") + plain.Render("“"+f.New+"”")
	case f.Kind == model.ChangeLink && f.Old != "" && f.New != "":
		// a re-parent: falls through to old → new
	case f.Kind == model.ChangeLink && f.Removed:
		return plain.Render(f.Label) + muted.Render(" removed") + plain.Render(lead(f.Old))
	case f.Kind == model.ChangeLink:
		return plain.Render(f.Label) + muted.Render(" added") + plain.Render(lead(f.New))
	case f.Old == "" && f.New == "":
		return plain.Render(f.Label) + muted.Render(" edited")
	case f.Kind == model.ChangeState:
		return muted.Render(f.Label+" ") + st(stateStyle(f.Old)).Render(f.OldText()) + arrow + st(stateStyle(f.New)).Render(f.NewText())
	case f.Kind == model.ChangeIteration, f.Label == "Area":
		return muted.Render(f.Label+" ") + plain.Render(orDashSeg(f.Old)) + arrow + plain.Render(orDashSeg(f.New))
	}
	return muted.Render(f.Label+" ") + plain.Render(f.OldText()) + arrow + plain.Render(f.NewText())
}

// lead puts a space in front of a non-empty string.
func lead(s string) string {
	if s == "" {
		return ""
	}
	return " " + s
}

// orDashSeg is the last segment of a path, or a dash for none.
func orDashSeg(path string) string {
	if path == "" {
		return "—"
	}
	return lastSeg(path)
}

// changesDetail is the head of the preview pane in the Changes view:
// everything that happened to the highlighted item in the period, in full.
func (t *changes) changesDetail(it *model.WorkItem, width int) string {
	list := t.itemChanges(it.ID)
	if len(list) == 0 {
		return ""
	}
	now := t.now()
	var b strings.Builder
	b.WriteString(sTitle.Render("Changed "+t.period.label(now)) + sMuted.Render(fmt.Sprintf("  %d", len(list))) + "\n")
	id := func(s lipgloss.Style) lipgloss.Style { return s }
	for _, c := range list {
		at := c.At.In(now.Location())
		by := c.By
		if by == "" {
			by = "Unknown"
		}
		b.WriteString("\n" + sMuted.Render(at.Format("Mon 2 Jan 15:04")+"  ") + by + "\n")
		for _, f := range c.Fields {
			b.WriteString(indent(wrap(renderFieldChange(f, id), max(width-2, 10)), "  ") + "\n")
		}
	}
	b.WriteString("\n" + sMuted.Render(strings.Repeat("─", width)) + "\n")
	return b.String()
}

func indent(s, prefix string) string {
	return prefix + strings.ReplaceAll(s, "\n", "\n"+prefix)
}
