// Package markdown converts Azure DevOps HTML descriptions to and from
// Markdown, and renders Markdown for the terminal.
//
// Azure DevOps stores descriptions as HTML. Editing HTML in a terminal is
// miserable, so the TUI works in Markdown throughout: HTML is converted on
// read, Markdown is converted back on write.
package markdown

import (
	"bytes"
	"regexp"
	"strings"
	"sync"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2"
	"github.com/charmbracelet/glamour"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

// Style overrides Glamour's auto-detected style ("ascii", "dark", "light",
// "notty"). Empty means auto. Tests set it to "ascii".
var Style string

// htmlTag matches the tags Azure DevOps' HTML editor emits. Markdown with a
// stray "<" (a comparison, a generic) does not match.
var htmlTag = regexp.MustCompile(`(?i)</?(div|p|br|span|b|i|u|strong|em|ul|ol|li|a|h[1-6]|table|thead|tbody|tr|td|th|pre|code|img|blockquote|hr)\b[^>]*>`)

// LooksLikeHTML reports whether a field value is HTML rather than Markdown.
// Azure DevOps returns raw Markdown for fields in Markdown mode and HTML
// for the rest; the SDK drops the format flag so we sniff the content.
func LooksLikeHTML(s string) bool {
	return htmlTag.MatchString(s)
}

// FromHTML converts an HTML description to Markdown. Markdown and plain
// text pass through unchanged.
func FromHTML(h string) string {
	h = strings.TrimSpace(h)
	if h == "" || !LooksLikeHTML(h) {
		return h
	}
	md, err := htmltomd.ConvertString(h)
	if err != nil {
		return h
	}
	return strings.TrimSpace(md)
}

var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
	goldmark.WithRendererOptions(html.WithHardWraps(), html.WithUnsafe()),
)

// ToHTML converts Markdown to the HTML Azure DevOps expects. Hard wraps are
// kept as <br> so single newlines survive the round trip.
func ToHTML(src string) string {
	src = strings.TrimSpace(src)
	if src == "" {
		return ""
	}
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		return src
	}
	return strings.TrimSpace(buf.String())
}

var (
	mu        sync.Mutex
	renderers = map[int]*glamour.TermRenderer{}
)

// Render draws Markdown for a terminal of the given width. Renderers are
// cached per width because building one is expensive.
func Render(src string, width int) string {
	src = strings.TrimSpace(src)
	if src == "" {
		return ""
	}
	if width < 20 {
		width = 20
	}
	mu.Lock()
	r, ok := renderers[width]
	if !ok {
		opts := []glamour.TermRendererOption{glamour.WithWordWrap(width), glamour.WithEmoji()}
		if Style != "" {
			opts = append(opts, glamour.WithStandardStyle(Style))
		} else {
			opts = append(opts, glamour.WithAutoStyle())
		}
		var err error
		r, err = glamour.NewTermRenderer(opts...)
		if err != nil {
			mu.Unlock()
			return src
		}
		renderers[width] = r
	}
	mu.Unlock()
	out, err := r.Render(src)
	if err != nil {
		return src
	}
	// Glamour pads every line with a leading margin and adds blank lines
	// around the document; trim the vertical slack, keep the margin.
	return strings.Trim(out, "\n")
}
