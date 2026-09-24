package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// trunc cuts s to width cells, adding an ellipsis when cut.
func trunc(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return ansi.Truncate(s, width, "…")
}

// oneLine flattens s onto a single line: tabs and line breaks become
// spaces. A row that measures one width but renders as several lines, or
// wider (lipgloss expands tabs), would break a fixed-height layout.
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// clip cuts a rendered line to width cells without an ellipsis, as a last
// guard against a row wrapping inside its panel and growing it.
func clip(s string, width int) string {
	if ansi.StringWidth(s) <= width {
		return s
	}
	return ansi.Truncate(s, max(width, 0), "")
}

// pad right-pads s to width cells.
func pad(s string, width int) string {
	w := ansi.StringWidth(s)
	if w >= width {
		return trunc(s, width)
	}
	return s + strings.Repeat(" ", width-w)
}

// padLeft left-pads s to width cells.
func padLeft(s string, width int) string {
	w := ansi.StringWidth(s)
	if w >= width {
		return trunc(s, width)
	}
	return strings.Repeat(" ", width-w) + s
}

func fmtEffort(e float64) string {
	if e == 0 {
		return ""
	}
	if e == float64(int(e)) {
		return fmt.Sprintf("%d", int(e))
	}
	return fmt.Sprintf("%.1f", e)
}

func ago(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 14*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

func initials(name string) string {
	if name == "" {
		return "  "
	}
	parts := strings.Fields(name)
	if len(parts) == 1 {
		r := []rune(parts[0])
		if len(r) > 1 {
			return strings.ToUpper(string(r[:2]))
		}
		return strings.ToUpper(string(r)) + " "
	}
	a, b := []rune(parts[0]), []rune(parts[len(parts)-1])
	return strings.ToUpper(string(a[0]) + string(b[0]))
}

// wrap word-wraps text to width.
// titleLines word-wraps s into exactly n lines of at most width cells,
// ending the last with "…" when there's more. Short titles are padded with
// empty lines so cards stay the same height.
func titleLines(s string, width, n int) []string {
	if n <= 1 || width < 4 {
		return []string{trunc(s, width)}
	}
	var out []string
	rest := strings.TrimSpace(s)
	for len(out) < n-1 && rest != "" {
		if ansi.StringWidth(rest) <= width {
			out = append(out, rest)
			rest = ""
			break
		}
		cut := ansi.Truncate(rest, width+1, "")
		i := strings.LastIndex(cut, " ")
		if i <= 0 {
			cut = ansi.Truncate(rest, width, "")
			i = len(cut)
		}
		out = append(out, strings.TrimRight(rest[:i], " "))
		rest = strings.TrimLeft(rest[i:], " ")
	}
	out = append(out, trunc(rest, width))
	for len(out) < n {
		out = append(out, "")
	}
	return out[:n]
}

func wrap(s string, width int) string {
	if width < 4 {
		return s
	}
	return lipgloss.NewStyle().Width(width).Render(s)
}

// plural returns "1 lane", "3 lanes": count and noun, with an s unless n is 1.
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// overlay centers popup on top of bg by replacing the covered cells.
func overlay(bg, popup string, w, h int) string {
	bgLines := strings.Split(bg, "\n")
	for len(bgLines) < h {
		bgLines = append(bgLines, "")
	}
	pLines := strings.Split(popup, "\n")
	pw := lipgloss.Width(popup)
	top := max((h-len(pLines))/2, 0)
	left := max((w-pw)/2, 0)
	for i, pl := range pLines {
		row := top + i
		if row >= len(bgLines) {
			break
		}
		bgLine := bgLines[row]
		leftPart := pad(ansi.Truncate(bgLine, left, ""), left)
		rightPart := ""
		if ansi.StringWidth(bgLine) > left+pw {
			rightPart = ansi.TruncateLeft(bgLine, left+pw, "")
		}
		bgLines[row] = leftPart + pad(pl, pw) + rightPart
	}
	return strings.Join(bgLines, "\n")
}
