package profile

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseSurfaces(t *testing.T) {
	got, err := ParseSurfaces("cli, vscode ,CODE")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got[SurfaceCLI] || !got[SurfaceVSCode] {
		t.Fatalf("expected cli+vscode, got %v", got)
	}
	if len(got) != 2 {
		t.Fatalf("code should alias vscode (no dupe surface): %v", got)
	}
	if s, err := ParseSurfaces(""); s != nil || err != nil {
		t.Fatalf("empty should be nil/nil, got %v %v", s, err)
	}
	if _, err := ParseSurfaces("bogus"); err == nil {
		t.Fatal("expected error for unknown surface")
	}
}

func TestParseFeatures(t *testing.T) {
	got, err := ParseFeatures("instructions,mcp")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !got[CatInstructions] || !got[CatMCP] {
		t.Fatalf("expected instructions+mcp, got %v", got)
	}
	if _, err := ParseFeatures("nope"); err == nil {
		t.Fatal("expected error for unknown feature")
	}
}

func TestSurfaceForRel(t *testing.T) {
	cases := map[string]Surface{
		"cli/instructions/a.md":           SurfaceCLI,
		"agents/skills/x/SKILL.md":        SurfaceAgents,
		"app/m-mcp-servers.json":          SurfaceApp,
		"vscode/code/prompts/p.prompt.md": SurfaceVSCode,
		"vscode/code-insiders/mcp.json":   SurfaceInsiders, // insiders before code
		"history/data.db":                 SurfaceHistory,
	}
	for rel, want := range cases {
		if got := surfaceForRel(rel); got != want {
			t.Errorf("surfaceForRel(%q) = %q, want %q", rel, got, want)
		}
	}
}

func TestScopedSkip(t *testing.T) {
	cli := Asset{Surface: SurfaceCLI, Feature: CatInstructions, Kind: KindFile}
	code := Asset{Surface: SurfaceVSCode, Feature: CatMCP, Kind: KindFile}

	m := &Manager{Surfaces: map[Surface]bool{SurfaceCLI: true}}
	if m.skip(cli) {
		t.Error("cli asset should not be skipped when surface=cli")
	}
	if !m.skip(code) {
		t.Error("vscode asset should be skipped when surface=cli")
	}

	m = &Manager{Features: map[string]bool{CatMCP: true}}
	if m.skip(code) {
		t.Error("mcp asset should not be skipped when feature=mcp")
	}
	if !m.skip(cli) {
		t.Error("instructions asset should be skipped when feature=mcp")
	}

	if (&Manager{}).scoped() {
		t.Error("unscoped manager should report scoped()=false")
	}
}

func TestInventoryGroupsBySurface(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("cli/instructions/team.instructions.md")
	write("vscode/code/prompts/review.prompt.md")
	write("vscode/code-insiders/prompts/review.prompt.md")

	inv, err := inventoryOf(root)
	if err != nil {
		t.Fatal(err)
	}
	if inv.Count(SurfaceCLI, CatInstructions) != 1 {
		t.Errorf("expected 1 CLI instruction, got %d", inv.Count(SurfaceCLI, CatInstructions))
	}
	if inv.Count(SurfaceVSCode, CatPrompts) != 1 {
		t.Errorf("expected 1 VS Code prompt, got %d", inv.Count(SurfaceVSCode, CatPrompts))
	}
	if inv.Count(SurfaceInsiders, CatPrompts) != 1 {
		t.Errorf("expected 1 Insiders prompt, got %d", inv.Count(SurfaceInsiders, CatPrompts))
	}
	if got := inv.Surfaces(); len(got) != 3 {
		t.Errorf("expected 3 surfaces present, got %v", got)
	}
}
