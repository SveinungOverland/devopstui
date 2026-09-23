package ui

import (
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestSpreadDropsRightPiecesBeforeTouching(t *testing.T) {
	plain := lipgloss.NewStyle()
	cases := []struct {
		w    int
		want string
	}{
		{20, "PBI 1005        3 SØ"},
		{13, "PBI 1005 3 SØ"},
		{12, "PBI 1005  SØ"}, // no room for the effort: drop it, keep who
		{10, "PBI 1005"},     // no room for either
	}
	for _, c := range cases {
		got := spread(plain, "PBI 1005", c.w, "3", "SØ")
		if got != pad(c.want, c.w) {
			t.Errorf("w=%d: got %q, want %q", c.w, got, pad(c.want, c.w))
		}
	}
}

func TestTitleLines(t *testing.T) {
	cases := []struct {
		s    string
		w, n int
		want []string
	}{
		{"Rotate signing keys quarterly", 26, 2, []string{"Rotate signing keys", "quarterly"}},
		{"Short", 26, 2, []string{"Short", ""}},
		{"Retry budget also drops the parent span id", 20, 2, []string{"Retry budget also", "drops the parent sp…"}},
		{"Supercalifragilistic", 10, 2, []string{"Supercalif", "ragilistic"}},
		{"Rotate signing keys quarterly", 12, 1, []string{"Rotate sign…"}},
	}
	for _, c := range cases {
		if got := titleLines(c.s, c.w, c.n); !reflect.DeepEqual(got, c.want) {
			t.Errorf("titleLines(%q, %d, %d) = %q, want %q", c.s, c.w, c.n, got, c.want)
		}
	}
}

func TestPlural(t *testing.T) {
	for n, want := range map[int]string{0: "0 lanes", 1: "1 lane", 2: "2 lanes"} {
		if got := plural(n, "lane"); got != want {
			t.Errorf("plural(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestFooterHintsKeepsWholeHints(t *testing.T) {
	bindings := footerTree
	all := footerHints(bindings, 1000)
	if strings.Contains(all, "more") {
		t.Fatalf("everything fits, got %q", all)
	}
	for _, w := range []int{40, 80, 120} {
		got := ansi.Strip(footerHints(bindings, w))
		if lipgloss.Width(got) > w {
			t.Errorf("w=%d: %d wide: %q", w, lipgloss.Width(got), got)
		}
		if !strings.HasSuffix(got, "? more") || strings.Contains(got, "…") {
			t.Errorf("w=%d: want whole hints ending in \"? more\", got %q", w, got)
		}
	}
}
