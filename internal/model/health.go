package model

import (
	"fmt"
	"strings"
	"time"
)

// IsDone reports whether a state closes the item, across the process
// templates' names for it. Removed counts as done: it needs no more work.
func IsDone(state string) bool {
	switch state {
	case "Done", "Closed", "Removed", "Resolved", "Completed":
		return true
	}
	return false
}

// IsActive reports whether state means "currently being worked on",
// across the process templates' differing names for that stage (Scrum's
// task-level "In Progress" vs. its requirement-level "Committed", etc).
func IsActive(state string) bool {
	switch state {
	case "In Progress", "Active", "Committed", "Doing":
		return true
	}
	return false
}

// Signal is one reason an item needs attention. A set of them is a
// bitmask; the constants are in priority order, most actionable first, so
// Top can pick the one to show where there is room for only one glyph.
type Signal uint8

const (
	// SignalOpenTasks: the item is done but some of its tasks are not.
	SignalOpenTasks Signal = 1 << iota
	// SignalReadyToClose: every task is done but the item is not.
	SignalReadyToClose
	// SignalStale: active, yet nothing about it has changed for a while.
	SignalStale
	// SignalMissing: a field the item's state calls for is empty.
	SignalMissing
	// SignalOrphan: a requirement with no parent Feature.
	SignalOrphan

	signalEnd
)

// AllSignals lists every signal in priority order.
func AllSignals() []Signal {
	var out []Signal
	for s := Signal(1); s < signalEnd; s <<= 1 {
		out = append(out, s)
	}
	return out
}

// Name is the short label for a single signal.
func (s Signal) Name() string {
	switch s {
	case SignalOpenTasks:
		return "open tasks"
	case SignalReadyToClose:
		return "ready to close"
	case SignalStale:
		return "stale"
	case SignalMissing:
		return "missing info"
	case SignalOrphan:
		return "orphan"
	}
	return ""
}

// Has reports whether the set contains s.
func (s Signal) Has(o Signal) bool { return s&o != 0 }

// Top is the highest-priority signal in the set, 0 when empty.
func (s Signal) Top() Signal { return s & -s }

// List returns the signals in the set, in priority order.
func (s Signal) List() []Signal {
	var out []Signal
	for _, x := range AllSignals() {
		if s.Has(x) {
			out = append(out, x)
		}
	}
	return out
}

// TaskTally summarises an item's task-level children. Total is 0 when the
// tasks are unknown (the backlog query never fetches them) as well as when
// there are none, and no task-based signal fires then.
type TaskTally struct {
	Done, Total int
	// Latest is the newest ChangedDate among the tasks: a PBI whose tasks
	// move every day is not stale just because the PBI itself sits still.
	Latest time.Time
}

// HealthRules are the tunable parts of Assess.
type HealthRules struct {
	Now time.Time
	// StaleAfter is how long an active item may go unchanged; 0 turns
	// staleness off.
	StaleAfter time.Duration
}

// Health is what Assess found for one item.
type Health struct {
	Signals Signal
	// Missing names what SignalMissing is about, e.g. "no assignee".
	Missing []string
	// Idle is how long since the item (or one of its tasks) last changed,
	// set when SignalStale is.
	Idle time.Duration
}

// Describe lists every signal by name, with what is missing spelled out.
func (h Health) Describe() []string {
	var out []string
	for _, s := range h.Signals.List() {
		switch s {
		case SignalMissing:
			out = append(out, s.Name()+": "+strings.Join(h.Missing, ", "))
		case SignalStale:
			out = append(out, s.Name()+": "+idleText(h.Idle))
		default:
			out = append(out, s.Name())
		}
	}
	return out
}

func idleText(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day without a change"
	}
	return fmt.Sprintf("%d days without a change", days)
}

// Assess works out which signals apply to it. tasks is the tally of its
// task-level children, zero when unknown.
func Assess(it *WorkItem, tasks TaskTally, cfg BacklogConfig, r HealthRules) Health {
	var h Health
	if it == nil || it.State == "Removed" {
		return h
	}
	done := IsDone(it.State)
	active := IsActive(it.State)
	task := cfg.TaskLevel(it)

	if tasks.Total > 0 {
		switch {
		case done && tasks.Done < tasks.Total:
			h.Signals |= SignalOpenTasks
		case !done && tasks.Done == tasks.Total:
			h.Signals |= SignalReadyToClose
		}
	}

	if active && r.StaleAfter > 0 {
		last := it.ChangedDate
		if tasks.Latest.After(last) {
			last = tasks.Latest
		}
		if !last.IsZero() && r.Now.Sub(last) > r.StaleAfter {
			h.Signals |= SignalStale
			h.Idle = r.Now.Sub(last)
		}
	}

	// An unassigned New item is ordinary backlog; one someone is meant to
	// be working on is not.
	if active && it.AssignedTo == "" {
		h.Missing = append(h.Missing, "no assignee")
	}
	if task && done && it.RemainingWork > 0 {
		h.Missing = append(h.Missing, "hours left on a done task")
	}
	if len(h.Missing) > 0 {
		h.Signals |= SignalMissing
	}

	if it.Kind == KindRequirement && !done && it.ParentID == 0 {
		h.Signals |= SignalOrphan
	}
	return h
}
