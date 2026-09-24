package model

import (
	"reflect"
	"testing"
	"time"
)

func TestAssess(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	rules := HealthRules{Now: now, StaleAfter: 5 * 24 * time.Hour}
	cfg := BacklogConfig{BugsBehavior: "asRequirements"}
	fresh := now.Add(-time.Hour)
	old := now.AddDate(0, 0, -9)

	// pbi is a healthy, estimated, parented requirement to vary from.
	pbi := func(mod func(*WorkItem)) *WorkItem {
		w := &WorkItem{ID: 1, Kind: KindRequirement, State: "Committed", AssignedTo: "Ann", Effort: 3, ParentID: 9, ChangedDate: fresh}
		if mod != nil {
			mod(w)
		}
		return w
	}
	task := func(mod func(*WorkItem)) *WorkItem {
		w := &WorkItem{ID: 2, Kind: KindTask, State: "In Progress", AssignedTo: "Ann", RemainingWork: 4, ParentID: 1, ChangedDate: fresh}
		if mod != nil {
			mod(w)
		}
		return w
	}

	cases := []struct {
		name    string
		it      *WorkItem
		tasks   TaskTally
		want    Signal
		missing []string
	}{
		{"healthy", pbi(nil), TaskTally{Done: 1, Total: 2}, 0, nil},
		{"ready to close", pbi(nil), TaskTally{Done: 2, Total: 2}, SignalReadyToClose, nil},
		{"done with open tasks", pbi(func(w *WorkItem) { w.State = "Done" }), TaskTally{Done: 1, Total: 2}, SignalOpenTasks, nil},
		{"done with done tasks", pbi(func(w *WorkItem) { w.State = "Done" }), TaskTally{Done: 2, Total: 2}, 0, nil},
		{"tasks unknown", pbi(nil), TaskTally{}, 0, nil},
		{"stale", pbi(func(w *WorkItem) { w.ChangedDate = old }), TaskTally{}, SignalStale, nil},
		{"stale but tasks moving", pbi(func(w *WorkItem) { w.ChangedDate = old }), TaskTally{Done: 0, Total: 1, Latest: fresh}, 0, nil},
		{"old but not active", pbi(func(w *WorkItem) { w.State = "New"; w.ChangedDate = old }), TaskTally{}, 0, nil},
		{"old but removed", pbi(func(w *WorkItem) { w.State = "Removed"; w.ChangedDate = old; w.ParentID = 0 }), TaskTally{}, 0, nil},
		{"no effort", pbi(func(w *WorkItem) { w.Effort = 0 }), TaskTally{}, SignalMissing, []string{"no effort"}},
		{"done without effort", pbi(func(w *WorkItem) { w.State = "Done"; w.Effort = 0 }), TaskTally{}, 0, nil},
		{"active and unassigned", pbi(func(w *WorkItem) { w.AssignedTo = "" }), TaskTally{}, SignalMissing, []string{"no assignee"}},
		{"new and unassigned", pbi(func(w *WorkItem) { w.State = "New"; w.AssignedTo = "" }), TaskTally{}, 0, nil},
		{"bug as requirement without effort", pbi(func(w *WorkItem) { w.Kind = KindBug; w.Effort = 0 }), TaskTally{}, SignalMissing, []string{"no effort"}},
		{"task healthy", task(nil), TaskTally{}, 0, nil},
		{"task in progress with 0h", task(func(w *WorkItem) { w.RemainingWork = 0 }), TaskTally{}, SignalMissing, []string{"no remaining work"}},
		{"task to do with 0h", task(func(w *WorkItem) { w.State = "To Do"; w.RemainingWork = 0 }), TaskTally{}, 0, nil},
		{"done task with hours", task(func(w *WorkItem) { w.State = "Done" }), TaskTally{}, SignalMissing, []string{"hours left on a done task"}},
		{"task needs no effort", task(func(w *WorkItem) { w.Effort = 0; w.ParentID = 0 }), TaskTally{}, 0, nil},
		{"orphan", pbi(func(w *WorkItem) { w.ParentID = 0 }), TaskTally{}, SignalOrphan, nil},
		{"done orphan", pbi(func(w *WorkItem) { w.ParentID = 0; w.State = "Done" }), TaskTally{}, 0, nil},
		{"parentless epic", &WorkItem{Kind: KindEpic, State: "New"}, TaskTally{}, 0, nil},
		{"parentless bug", pbi(func(w *WorkItem) { w.Kind = KindBug; w.ParentID = 0 }), TaskTally{}, 0, nil},
		{"several", pbi(func(w *WorkItem) { w.ParentID = 0; w.Effort = 0; w.ChangedDate = old }), TaskTally{Done: 1, Total: 1},
			SignalReadyToClose | SignalStale | SignalMissing | SignalOrphan, []string{"no effort"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := Assess(c.it, c.tasks, cfg, rules)
			if h.Signals != c.want {
				t.Errorf("signals = %v, want %v", h.Signals.List(), c.want.List())
			}
			if !reflect.DeepEqual(h.Missing, c.missing) {
				t.Errorf("missing = %q, want %q", h.Missing, c.missing)
			}
		})
	}
}

func TestAssessBugAsTask(t *testing.T) {
	cfg := BacklogConfig{BugsBehavior: "asTasks"}
	bug := &WorkItem{Kind: KindBug, State: "Active", AssignedTo: "Ann", RemainingWork: 0}
	h := Assess(bug, TaskTally{}, cfg, HealthRules{})
	if want := []string{"no remaining work"}; !reflect.DeepEqual(h.Missing, want) {
		t.Errorf("missing = %q, want %q", h.Missing, want)
	}
}

func TestAssessStaleOff(t *testing.T) {
	now := time.Now()
	it := &WorkItem{Kind: KindTask, State: "In Progress", AssignedTo: "Ann", RemainingWork: 1, ChangedDate: now.AddDate(-1, 0, 0)}
	if h := Assess(it, TaskTally{}, BacklogConfig{}, HealthRules{Now: now}); h.Signals.Has(SignalStale) {
		t.Error("stale with StaleAfter 0")
	}
}

func TestSignalTop(t *testing.T) {
	s := SignalOrphan | SignalMissing | SignalStale
	if s.Top() != SignalStale {
		t.Errorf("top = %v", s.Top().Name())
	}
	if Signal(0).Top() != 0 {
		t.Error("empty set has a top")
	}
	h := Health{Signals: SignalStale | SignalMissing, Missing: []string{"no effort", "no assignee"}, Idle: 9 * 24 * time.Hour}
	want := []string{"stale: 9 days without a change", "missing info: no effort, no assignee"}
	if got := h.Describe(); !reflect.DeepEqual(got, want) {
		t.Errorf("describe = %q", got)
	}
}
