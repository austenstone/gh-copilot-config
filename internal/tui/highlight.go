package tui

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	glamour "charm.land/glamour/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
)

// render turns file content into preview-ready text: markdown goes through
// glamour (glow's renderer), everything else gets chroma syntax highlighting.
// YAML frontmatter is split off and rendered as its own metadata panel so the
// preview doesn't dump raw --- fences through the markdown renderer.
func render(content, filename string, width int) string {
	if isMarkdown(filename) {
		fm, body, ok := splitFrontmatter(content)
		out, err := renderMarkdown(body, width)
		if err != nil {
			out = highlight(body, filename)
		}
		if ok {
			if panel := renderFrontmatter(fm, width); panel != "" {
				return panel + "\n\n" + out
			}
		}
		return out
	}
	return highlight(content, filename)
}

// splitFrontmatter separates a leading YAML frontmatter block (delimited by ---
// fences) from the markdown body. ok is false when there's no frontmatter, in
// which case body is the original content untouched.
func splitFrontmatter(content string) (frontmatter, body string, ok bool) {
	lines := strings.Split(content, "\n")
	if len(lines) == 0 || strings.TrimRight(lines[0], "\r") != "---" {
		return "", content, false
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimRight(lines[i], "\r") == "---" {
			fm := strings.Join(lines[1:i], "\n")
			rest := strings.Join(lines[i+1:], "\n")
			return fm, strings.TrimLeft(rest, "\n"), true
		}
	}
	return "", content, false
}

// renderFrontmatter formats YAML frontmatter as a two-column metadata panel:
// bold accent keys, wrapped values, set off by a left accent bar. It parses
// only top-level "key: value" lines; nested or list lines fall under the
// previous key as dimmed continuations. Returns "" when there's nothing to show.
func renderFrontmatter(fm string, width int) string {
	type row struct{ key, val string }
	var rows []row
	keyW := 0
	for _, raw := range strings.Split(fm, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		k, v, found := strings.Cut(line, ":")
		indented := line[0] == ' ' || line[0] == '\t' || line[0] == '-'
		if !found || indented {
			rows = append(rows, row{"", strings.TrimSpace(line)})
			continue
		}
		k = strings.TrimSpace(k)
		if w := lipgloss.Width(k); w > keyW {
			keyW = w
		}
		rows = append(rows, row{k, strings.TrimSpace(v)})
	}
	if len(rows) == 0 {
		return ""
	}

	keyStyle := lipgloss.NewStyle().Bold(true).Foreground(accent)
	contStyle := lipgloss.NewStyle().Foreground(dim)
	valW := max(10, width-keyW-6) // 4 border/pad + 2 gap between columns

	var b strings.Builder
	for i, r := range rows {
		if i > 0 {
			b.WriteString("\n")
		}
		if r.key == "" {
			b.WriteString(lipgloss.NewStyle().Width(valW).Render(contStyle.Render(r.val)))
			continue
		}
		keyCol := keyStyle.Width(keyW).Render(r.key)
		valCol := lipgloss.NewStyle().Width(valW).Render(r.val)
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, keyCol, "  ", valCol))
	}

	return lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, false, true).
		BorderForeground(accent).
		PaddingLeft(1).
		Render(b.String())
}

func isMarkdown(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".md", ".markdown", ".mdown", ".mkd":
		return true
	}
	return false
}

// renderMarkdown renders markdown to styled terminal text with glamour,
// wrapping at the viewport width and matching the terminal background and
// color profile. The renderer is cached per width/theme because constructing
// one compiles the style and parser, which is the slow part.
func renderMarkdown(content string, width int) (string, error) {
	mdMu.Lock()
	defer mdMu.Unlock()
	r, err := cachedRenderer(width, lipgloss.HasDarkBackground(os.Stdin, os.Stdout))
	if err != nil {
		return "", err
	}
	out, err := r.Render(content)
	if err != nil {
		return "", err
	}
	return strings.TrimRight(out, "\n"), nil
}

var (
	mdMu       sync.Mutex
	mdRenderer *glamour.TermRenderer
	mdWidth    int
	mdDark     bool
)

// cachedRenderer returns a glamour renderer for the given width/theme, reusing
// the last one when nothing changed. Callers must hold mdMu.
func cachedRenderer(width int, dark bool) (*glamour.TermRenderer, error) {
	if mdRenderer != nil && mdWidth == width && mdDark == dark {
		return mdRenderer, nil
	}
	style := "dark"
	if !dark {
		style = "light"
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle(style),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil, err
	}
	mdRenderer, mdWidth, mdDark = r, width, dark
	return r, nil
}

// highlight returns content with ANSI syntax highlighting based on filename.
// On any failure it returns the original content untouched.
func highlight(content, filename string) string {
	lexer := chroma.Coalesce(lexerFor(filename, content))

	style := styles.Get("github-dark")
	if !lipgloss.HasDarkBackground(os.Stdin, os.Stdout) {
		style = styles.Get("github")
	}
	if style == nil {
		style = styles.Fallback
	}

	it, err := lexer.Tokenise(nil, content)
	if err != nil {
		return content
	}
	var buf bytes.Buffer
	if err := lipglossFormatter.Format(&buf, style, it); err != nil {
		return content
	}
	return buf.String()
}

// lexerFor picks a lexer by extension, falling back to JSON for jsonc files
// (which chroma doesn't register), then content analysis, then a plain lexer
// so every preview gets at least minimal styling.
func lexerFor(filename, content string) chroma.Lexer {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".jsonc", ".json5":
		return lexers.Get("json")
	}
	if l := lexers.Match(filename); l != nil {
		return l
	}
	if l := lexers.Analyse(content); l != nil {
		return l
	}
	return lexers.Fallback
}

// lipglossFormatter renders each chroma token through lipgloss (the pattern
// charm's crush uses) so colors downgrade to the terminal's profile
// automatically and no opaque background is forced onto the preview.
var lipglossFormatter = chroma.FormatterFunc(func(w io.Writer, style *chroma.Style, it chroma.Iterator) error {
	for token := it(); token != chroma.EOF; token = it() {
		entry := style.Get(token.Type)
		if entry.IsZero() {
			if _, err := fmt.Fprint(w, token.Value); err != nil {
				return err
			}
			continue
		}
		s := lipgloss.NewStyle()
		if entry.Bold == chroma.Yes {
			s = s.Bold(true)
		}
		if entry.Italic == chroma.Yes {
			s = s.Italic(true)
		}
		if entry.Underline == chroma.Yes {
			s = s.Underline(true)
		}
		if entry.Colour.IsSet() {
			s = s.Foreground(lipgloss.Color(entry.Colour.String()))
		}
		// Style per line: lipgloss pads multi-line strings into a rectangle,
		// injecting stray spaces. Chroma bundles newlines and indentation into
		// single tokens (e.g. "\n    "), so feeding those to s.Render whole
		// would leak that padding into the preview.
		lines := strings.Split(token.Value, "\n")
		for i, line := range lines {
			if i > 0 {
				if _, err := io.WriteString(w, "\n"); err != nil {
					return err
				}
			}
			if line == "" {
				continue
			}
			if _, err := fmt.Fprint(w, s.Render(line)); err != nil {
				return err
			}
		}
	}
	return nil
})
