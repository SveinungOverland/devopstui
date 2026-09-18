package model

import "testing"

func TestBugChildOf(t *testing.T) {
	cases := []struct {
		parent *WorkItem
		behav  string
		want   bool
	}{
		{nil, "asRequirements", true},
		{nil, "asTasks", false},
		{nil, "off", false},

		{item(1, 0, KindEpic), "asRequirements", false},
		{item(1, 0, KindEpic), "asTasks", false},
		{item(1, 0, KindEpic), "off", false},

		{item(1, 0, KindFeature), "asRequirements", true},
		{item(1, 0, KindFeature), "asTasks", false},
		{item(1, 0, KindFeature), "off", false},

		{item(1, 0, KindRequirement), "asRequirements", false},
		{item(1, 0, KindRequirement), "asTasks", true},
		{item(1, 0, KindRequirement), "off", false},

		{item(1, 0, KindTask), "asRequirements", false},
		{item(1, 0, KindTask), "asTasks", false},
		{item(1, 0, KindTask), "off", false},

		{item(1, 0, KindBug), "asRequirements", false},
		{item(1, 0, KindBug), "asTasks", false},
		{item(1, 0, KindBug), "off", false},
	}
	for _, c := range cases {
		cfg := BacklogConfig{BugsBehavior: c.behav}
		kind := "nil"
		if c.parent != nil {
			kind = c.parent.Kind.Tag()
		}
		if got := cfg.BugChildOf(c.parent); got != c.want {
			t.Errorf("BugChildOf(%s) with BugsBehavior=%q = %v, want %v", kind, c.behav, got, c.want)
		}
	}
}
