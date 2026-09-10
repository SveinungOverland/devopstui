package markdown

import (
	"strings"
	"testing"
)

func TestFromHTML(t *testing.T) {
	in := `<div><b>Goal</b></div><div>Send a <i>magic link</i>.</div><ul><li>one</li><li>two</li></ul><pre><code>go test</code></pre>`
	got := FromHTML(in)
	for _, want := range []string{"**Goal**", "*magic link*", "- one", "- two", "go test"} {
		if !strings.Contains(got, want) {
			t.Errorf("FromHTML missing %q in:\n%s", want, got)
		}
	}
	if FromHTML("plain text") != "plain text" {
		t.Error("plain text should pass through")
	}
	md := "# Goal\n\nif a < b then **stop**\n\n- [ ] item"
	if FromHTML(md) != md {
		t.Error("markdown with a bare < must pass through untouched")
	}
}

func TestLooksLikeHTML(t *testing.T) {
	cases := map[string]bool{
		"<div>hi</div>":                 true,
		"<p>x</p><br/>":                 true,
		"line<br>break":                 true,
		"# Title\n\ntext":               false,
		"a < b and c > d":               false,
		"generic List<string> in code":  false,
		"":                              false,
		"<a href=\"x\">link</a>":        true,
		"**bold** with <sup>1</sup>":    false, // unknown tag → treated as markdown
	}
	for in, want := range cases {
		if got := LooksLikeHTML(in); got != want {
			t.Errorf("LooksLikeHTML(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestToHTML(t *testing.T) {
	got := ToHTML("# Title\n\nline one\nline two\n\n- a\n- b\n\n| h |\n|---|\n| c |")
	for _, want := range []string{"<h1", "<br", "<li>a</li>", "<table>"} {
		if !strings.Contains(got, want) {
			t.Errorf("ToHTML missing %q in:\n%s", want, got)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	src := "**Goal**\n\nSend a *magic link*.\n\n- one\n- two"
	back := FromHTML(ToHTML(src))
	if back != src {
		t.Errorf("round trip changed text:\n%s\n---\n%s", src, back)
	}
}

func TestRender(t *testing.T) {
	Style = "ascii"
	out := Render("# Head\n\nsome **bold** text\n\n- item", 40)
	if !strings.Contains(out, "Head") || !strings.Contains(out, "item") {
		t.Errorf("render output unexpected:\n%s", out)
	}
	if Render("", 40) != "" {
		t.Error("empty input should render empty")
	}
}
