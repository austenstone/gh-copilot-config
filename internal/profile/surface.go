package profile

import (
	"fmt"
	"sort"
	"strings"
)

// Surface is where a customization lives: a tool/IDE/host. It is the primary
// axis (alongside feature) used to lay out and scope backup/restore.
type Surface string

const (
	SurfaceCLI      Surface = "Copilot CLI"
	SurfaceVSCode   Surface = "VS Code"
	SurfaceInsiders Surface = "VS Code Insiders"
	SurfaceApp      Surface = "Copilot app"
	SurfaceDotCom   Surface = "github.com"
	SurfaceAgents   Surface = "Cross-tool agents"
	SurfaceHistory  Surface = "Session history"
)

// AllSurfaces lists every surface in display order.
var AllSurfaces = []Surface{SurfaceCLI, SurfaceVSCode, SurfaceInsiders, SurfaceApp, SurfaceDotCom, SurfaceAgents, SurfaceHistory}

// surfaceTokens maps short flag tokens to surfaces (for --surface).
var surfaceTokens = map[string]Surface{
	"cli":      SurfaceCLI,
	"vscode":   SurfaceVSCode,
	"code":     SurfaceVSCode,
	"insiders": SurfaceInsiders,
	"app":      SurfaceApp,
	"dotcom":   SurfaceDotCom,
	"github":   SurfaceDotCom,
	"agents":   SurfaceAgents,
	"history":  SurfaceHistory,
}

// Token returns the canonical short flag token for a surface.
func (s Surface) Token() string {
	switch s {
	case SurfaceCLI:
		return "cli"
	case SurfaceVSCode:
		return "vscode"
	case SurfaceInsiders:
		return "insiders"
	case SurfaceApp:
		return "app"
	case SurfaceDotCom:
		return "dotcom"
	case SurfaceAgents:
		return "agents"
	case SurfaceHistory:
		return "history"
	}
	return strings.ToLower(string(s))
}

// surfaceForRel derives a surface from a profile-relative path. Insiders must be
// checked before the stable VS Code prefix since one is a prefix of the other.
func surfaceForRel(rel string) Surface {
	switch {
	case strings.HasPrefix(rel, "cli/"):
		return SurfaceCLI
	case strings.HasPrefix(rel, "agents/"):
		return SurfaceAgents
	case strings.HasPrefix(rel, "app/"):
		return SurfaceApp
	case strings.HasPrefix(rel, "dotcom/"):
		return SurfaceDotCom
	case strings.HasPrefix(rel, "vscode/code-insiders/"):
		return SurfaceInsiders
	case strings.HasPrefix(rel, "vscode/code/"):
		return SurfaceVSCode
	case strings.HasPrefix(rel, "history/"):
		return SurfaceHistory
	}
	return SurfaceCLI
}

// featureTokens maps short flag tokens to feature labels (for --feature).
var featureTokens = map[string]string{
	"instructions": CatInstructions,
	"prompts":      CatPrompts,
	"agents":       CatAgents,
	"skills":       CatSkills,
	"hooks":        CatHooks,
	"mcp":          CatMCP,
	"extensions":   CatExtensions,
	"plugins":      CatPlugins,
	"settings":     FeatSettings,
	"db":           FeatDB,
	"history":      FeatHistory,
}

// ParseSurfaces turns a comma-separated token list into a surface set. An empty
// string means "no filter" (nil), so callers can treat nil as "all surfaces".
func ParseSurfaces(csv string) (map[Surface]bool, error) {
	toks := splitCSV(csv)
	if len(toks) == 0 {
		return nil, nil
	}
	out := map[Surface]bool{}
	for _, tok := range toks {
		s, ok := surfaceTokens[tok]
		if !ok {
			return nil, fmt.Errorf("unknown surface %q (valid: %s)", tok, strings.Join(tokenList(surfaceTokens), ", "))
		}
		out[s] = true
	}
	return out, nil
}

// ParseFeatures turns a comma-separated token list into a feature set. An empty
// string means "no filter" (nil), so callers can treat nil as "all features".
func ParseFeatures(csv string) (map[string]bool, error) {
	toks := splitCSV(csv)
	if len(toks) == 0 {
		return nil, nil
	}
	out := map[string]bool{}
	for _, tok := range toks {
		f, ok := featureTokens[tok]
		if !ok {
			return nil, fmt.Errorf("unknown feature %q (valid: %s)", tok, strings.Join(tokenList(featureTokens), ", "))
		}
		out[f] = true
	}
	return out, nil
}

func splitCSV(csv string) []string {
	var out []string
	for _, tok := range strings.Split(csv, ",") {
		if tok = strings.ToLower(strings.TrimSpace(tok)); tok != "" {
			out = append(out, tok)
		}
	}
	return out
}

func tokenList[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
