package tui

import (
	"strings"
	"testing"
)

func TestSplitFrontmatter(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		wantFM   string
		wantBody string
		wantOK   bool
	}{
		{
			name:     "instructions block",
			in:       "---\nname: Coding\ndescription: rules\napplyTo: \"**\"\n---\n\n# Heading\n\nbody text",
			wantFM:   "name: Coding\ndescription: rules\napplyTo: \"**\"",
			wantBody: "# Heading\n\nbody text",
			wantOK:   true,
		},
		{
			name:     "no frontmatter",
			in:       "# Heading\n\nbody",
			wantBody: "# Heading\n\nbody",
			wantOK:   false,
		},
		{
			name:   "unterminated fence",
			in:     "---\nname: x\n# Heading",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fm, body, ok := splitFrontmatter(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && fm != tc.wantFM {
				t.Errorf("fm = %q, want %q", fm, tc.wantFM)
			}
			if tc.wantBody != "" && body != tc.wantBody {
				t.Errorf("body = %q, want %q", body, tc.wantBody)
			}
		})
	}
}

func TestRenderFrontmatterPanel(t *testing.T) {
	out := renderFrontmatter("name: Coding\ndescription: some rules", 80)
	if out == "" {
		t.Fatal("expected a panel, got empty")
	}
	if !strings.Contains(out, "name") || !strings.Contains(out, "Coding") {
		t.Errorf("panel missing key/value content:\n%s", out)
	}
}

func TestRenderSplitsFrontmatterFromBody(t *testing.T) {
	content := "---\nname: Coding\n---\n# Heading\n\nbody"
	out := render(content, "x.md", 80)
	if strings.Contains(out, "---") {
		t.Errorf("raw frontmatter fence leaked into preview:\n%s", out)
	}
	if !strings.Contains(out, "name") {
		t.Errorf("frontmatter key missing from preview:\n%s", out)
	}
}
