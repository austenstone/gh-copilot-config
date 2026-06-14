package profile

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tailscale/hujson"
)

// Customization categories surfaced when inspecting a profile, in display order.
const (
	CatInstructions = "Custom instructions"
	CatPrompts      = "Prompt files"
	CatAgents       = "Custom agents"
	CatSubagents    = "Subagents"
	CatSkills       = "Agent skills"
	CatHooks        = "Hooks"
	CatMCP          = "MCP servers"
)

// Categories is the fixed order categories are presented in.
var Categories = []string{CatInstructions, CatPrompts, CatAgents, CatSubagents, CatSkills, CatHooks, CatMCP}

// InvItem is one customization within a category.
type InvItem struct {
	Name string // display name
	Path string // file to preview or open; for MCP, the config file it lives in
}

// Inventory is a profile's customizations grouped by category, deduped by name.
type Inventory struct {
	Items map[string][]InvItem
}

// Count returns how many items a category holds.
func (inv Inventory) Count(cat string) int { return len(inv.Items[cat]) }

// Inspect classifies every managed asset in a saved profile into categories.
func (m *Manager) Inspect(name string) (Inventory, error) {
	return inventoryOf(m.ProfileDir(name))
}

func inventoryOf(root string) (Inventory, error) {
	seen := map[string]map[string]bool{}
	inv := Inventory{Items: map[string][]InvItem{}}
	add := func(cat, name, p string) {
		if seen[cat] == nil {
			seen[cat] = map[string]bool{}
		}
		if seen[cat][name] {
			return
		}
		seen[cat][name] = true
		inv.Items[cat] = append(inv.Items[cat], InvItem{Name: name, Path: p})
	}

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel := filepath.ToSlash(mustRel(root, p))
		base := d.Name()
		if d.IsDir() {
			switch {
			case base == "node_modules" || base == ".git":
				return filepath.SkipDir
			case base == "hooks":
				listChildren(p, func(name, child string) { add(CatHooks, name, child) })
				return filepath.SkipDir
			case rel == "cli/agents":
				listChildren(p, func(name, child string) { add(CatSubagents, trimMD(name), child) })
				return filepath.SkipDir
			}
			return nil
		}

		parent := path.Base(path.Dir(rel))
		switch {
		case base == "SKILL.md":
			add(CatSkills, path.Base(path.Dir(rel)), p)
		case base == "copilot-instructions.md" || strings.HasSuffix(base, ".instructions.md") || (parent == "instructions" && strings.HasSuffix(base, ".md")):
			add(CatInstructions, base, p)
		case strings.HasSuffix(base, ".prompt.md"):
			add(CatPrompts, base, p)
		case strings.HasSuffix(base, ".agent.md") || strings.HasSuffix(base, ".chatmode.md"):
			add(CatAgents, base, p)
		case isMCPFile(base):
			for _, s := range mcpServers(p) {
				add(CatMCP, s, p)
			}
		}
		return nil
	})
	if err != nil {
		return inv, err
	}
	for _, items := range inv.Items {
		sort.Slice(items, func(i, j int) bool { return strings.ToLower(items[i].Name) < strings.ToLower(items[j].Name) })
	}
	return inv, nil
}

func mustRel(root, p string) string {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return rel
}

func trimMD(name string) string { return strings.TrimSuffix(name, ".md") }

func listChildren(dir string, fn func(name, path string)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		fn(e.Name(), filepath.Join(dir, e.Name()))
	}
}

func isMCPFile(base string) bool {
	switch base {
	case "mcp-config.json", "m-mcp-servers.json", "mcp.json":
		return true
	default:
		// VS Code settings carry MCP servers under a top-level "mcp" key.
		return base == "settings.copilot.json" || base == "settings.json"
	}
}

// mcpServers returns the server names declared in an MCP config file, tolerating
// the several shapes these files take (wrapped under servers/mcpServers, nested
// under an mcp key, or a bare name→config map).
func mcpServers(p string) []string {
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	std, err := hujson.Standardize(raw)
	if err != nil {
		return nil
	}
	var doc map[string]json.RawMessage
	if json.Unmarshal(std, &doc) != nil {
		return nil
	}
	if names := serverKeys(doc["servers"]); names != nil {
		return names
	}
	if names := serverKeys(doc["mcpServers"]); names != nil {
		return names
	}
	if mcp, ok := doc["mcp"]; ok {
		var inner map[string]json.RawMessage
		if json.Unmarshal(mcp, &inner) == nil {
			if names := serverKeys(inner["servers"]); names != nil {
				return names
			}
			if names := serverKeys(inner["mcpServers"]); names != nil {
				return names
			}
		}
		return nil
	}
	// Bare map of name→{command|url|type}: only treat as servers if it looks like one.
	var names []string
	for k, v := range doc {
		var entry map[string]json.RawMessage
		if json.Unmarshal(v, &entry) != nil {
			return nil
		}
		if _, ok := entry["command"]; ok {
			names = append(names, k)
		} else if _, ok := entry["url"]; ok {
			names = append(names, k)
		} else if _, ok := entry["type"]; ok {
			names = append(names, k)
		}
	}
	return names
}

func serverKeys(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return nil
	}
	names := make([]string, 0, len(obj))
	for k := range obj {
		names = append(names, k)
	}
	return names
}
