package ui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sveinungoverland/devopstui/internal/model"
)

// form is the full edit view for one item. It lists fields; enter opens the
// matching editor for the highlighted field, ctrl+s saves all changes.
type form struct {
	app     *App
	item    *model.WorkItem
	fields  []formField
	cursor  int
	values  map[string]any // pending changes by field ref
	child   popup
	onSave  func(patches []model.Patch) tea.Cmd
	states  []string
	members []string
	iters   []model.Iteration
}

type formField struct {
	label string
	ref   string
}

var formFields = []formField{
	{"Title", model.FieldTitle},
	{"State", model.FieldState},
	{"Assigned", model.FieldAssignedTo},
	{"Iteration", model.FieldIterationPath},
	{"Effort", model.FieldEffort},
	{"Priority", model.FieldPriority},
	{"Description", model.FieldDescription},
}

// openForm loads picker data then shows the form.
func (a *App) openForm(it *model.WorkItem) tea.Cmd {
	project, team := a.ctx.Project, a.ctx.Team
	iters := a.iterations
	return func() tea.Msg {
		ctx := context.Background()
		states, err := a.client.States(ctx, project, it.Type)
		if err != nil {
			return errMsg{err}
		}
		members, err := a.client.Members(ctx, project, team)
		if err != nil {
			return errMsg{err}
		}
		f := &form{app: a, item: it, fields: formFields, values: map[string]any{}, states: states, members: members, iters: iters}
		f.onSave = func(patches []model.Patch) tea.Cmd {
			if len(patches) == 0 {
				return a.setFlash("no changes", false)
			}
			return a.update(it, patches...)
		}
		return popupMsg{f}
	}
}

func (f *form) currentValue(ref string) string {
	if v, ok := f.values[ref]; ok {
		return fmt.Sprint(v)
	}
	it := f.item
	switch ref {
	case model.FieldTitle:
		return it.Title
	case model.FieldState:
		return it.State
	case model.FieldAssignedTo:
		return it.AssignedTo
	case model.FieldIterationPath:
		return it.IterationPath
	case model.FieldEffort:
		return fmtEffort(it.Effort)
	case model.FieldPriority:
		if it.Priority == 0 {
			return ""
		}
		return strconv.Itoa(it.Priority)
	case model.FieldDescription:
		return it.Description
	}
	return ""
}

func (f *form) Update(msg tea.Msg) (popup, tea.Cmd) {
	if f.child != nil {
		var cmd tea.Cmd
		f.child, cmd = f.child.Update(msg)
		return f, cmd
	}
	km, ok := msg.(tea.KeyMsg)
	if !ok {
		return f, nil
	}
	switch km.String() {
	case "esc", "q":
		return nil, nil
	case "ctrl+s":
		var patches []model.Patch
		for _, fld := range f.fields {
			if v, ok := f.values[fld.ref]; ok {
				patches = append(patches, model.Patch{Field: fld.ref, Value: v})
			}
		}
		return nil, f.onSave(patches)
	case "j", "down", "tab":
		if f.cursor < len(f.fields)-1 {
			f.cursor++
		}
	case "k", "up", "shift+tab":
		if f.cursor > 0 {
			f.cursor--
		}
	case "enter", "l", "e":
		return f, f.openEditor()
	}
	return f, nil
}

func (f *form) openEditor() (cmd tea.Cmd) {
	fld := f.fields[f.cursor]
	cur := f.currentValue(fld.ref)
	set := func(ref string, v any) tea.Cmd {
		f.values[ref] = v
		return nil
	}
	switch fld.ref {
	case model.FieldTitle:
		f.child = newPrompt("Title", cur, func(v string) tea.Cmd {
			if v != "" {
				return set(fld.ref, v)
			}
			return nil
		})
	case model.FieldState:
		var items []pickItem
		for _, s := range f.states {
			items = append(items, pickItem{Label: s, Value: s})
		}
		f.child = newPicker("State", items, func(pi pickItem) tea.Cmd { return set(fld.ref, pi.Value) })
	case model.FieldAssignedTo:
		items := []pickItem{{Label: "Unassigned", Value: ""}}
		for _, m := range f.members {
			items = append(items, pickItem{Label: m, Value: m})
		}
		f.child = newPicker("Assign to", items, func(pi pickItem) tea.Cmd { return set(fld.ref, pi.Value) })
	case model.FieldIterationPath:
		var items []pickItem
		for _, it := range f.iters {
			items = append(items, pickItem{Label: it.Name, Desc: iterDesc(it), Value: it.Path})
		}
		f.child = newPicker("Iteration", items, func(pi pickItem) tea.Cmd { return set(fld.ref, pi.Value) })
	case model.FieldEffort, model.FieldPriority:
		f.child = newPrompt(fld.label, cur, func(v string) tea.Cmd {
			if v == "" {
				return nil
			}
			n, err := strconv.ParseFloat(v, 64)
			if err != nil {
				return nil
			}
			if fld.ref == model.FieldPriority {
				return set(fld.ref, int(n))
			}
			return set(fld.ref, n)
		})
	case model.FieldDescription:
		// Uses the same path as the `d` key: external editor when configured,
		// built-in editor otherwise. The result lands in the pending values.
		item := *f.item
		item.Description = cur
		cmd = f.app.editDescription(&item, func(v string) tea.Cmd { return set(fld.ref, v) })
		if p := f.app.popup; p != f {
			// editDescription opened the built-in editor as the top-level
			// popup; re-home it as our child so the form stays visible.
			f.app.popup = f
			f.child = p
		}
	}
	return cmd
}

func (f *form) View(w, h int) string {
	width := min(max(w-10, 40), 90)
	var b strings.Builder
	b.WriteString(kindStyle(f.item.Kind).Render(f.item.Type) + sMuted.Render(fmt.Sprintf("  #%d", f.item.ID)) + "\n\n")
	for i, fld := range f.fields {
		val := f.currentValue(fld.ref)
		if fld.ref == model.FieldDescription {
			lines := strings.Count(val, "\n") + 1
			val = strings.ReplaceAll(val, "\n", " ")
			if val != "" {
				val = fmt.Sprintf("%s  %s", trunc(val, width-28), sMuted.Render(fmt.Sprintf("(%d lines)", lines)))
			}
		}
		cur := i == f.cursor
		st := rowStyler(cur)
		plain := st(lipgloss.NewStyle())
		val = trunc(val, width-16)
		valStyle := plain
		if _, changed := f.values[fld.ref]; changed {
			val += " *"
			valStyle = st(sSelected)
		}
		line := st(sLabel).Render(fld.label) + valStyle.Render(val)
		line += fill(plain, width-4-lipgloss.Width(line))
		if cur {
			line = st(sKey).Render(cursorMark+" ") + line
		} else {
			line = "  " + line
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n" + sKey.Render("enter") + sMuted.Render(" edit field  ") + sKey.Render("ctrl+s") + sMuted.Render(" save  ") + sKey.Render("esc") + sMuted.Render(" cancel"))
	out := sPopup.Width(width).Render(b.String())
	if f.child != nil {
		out = overlay(out, f.child.View(w, h), width+2, strings.Count(out, "\n")+1)
	}
	return out
}
