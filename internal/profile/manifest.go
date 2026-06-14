package profile

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
)

// Kind is the strategy used to save and apply a managed asset.
type Kind string

const (
	KindFile       Kind = "file"        // whole file
	KindDir        Kind = "dir"         // whole directory (mirrored)
	KindJSONKeys   Kind = "json-keys"   // a subset of top-level keys in a shared JSONC file
	KindDBSnapshot Kind = "db-snapshot" // consistent SQLite snapshot, backup-only (never restored)
	KindHistory    Kind = "history"     // opt-in heavy session history backup/restore
)

// Asset is one managed piece of Copilot configuration.
type Asset struct {
	Name     string
	Surface  Surface // where it lives (CLI, VS Code, app, …): the scope axis
	Feature  string  // primary feature it contributes (Cat*/Feat* label)
	Kind     Kind
	Live     string         // absolute live path
	Rel      string         // path under a profile directory
	KeyRE    *regexp.Regexp // KindJSONKeys: matches managed top-level keys
	Optional bool           // only written/saved with --all, never removed from live
}

// copilotKeys are the VS Code settings.json top-level keys this tool manages.
// Everything else in settings.json is the user's editor config and is left alone.
var copilotKeys = regexp.MustCompile(`^(github\.copilot|chat|mcp)`)

// Manifest returns every managed asset with live paths resolved for the current
// user and OS.
func Manifest() []Asset {
	home, _ := os.UserHomeDir()
	cop := filepath.Join(home, ".copilot")
	agents := filepath.Join(home, ".agents")
	code, ins := vscodeDirs(home)

	return []Asset{
		// Copilot CLI (~/.copilot)
		{Name: "cli-instructions", Surface: SurfaceCLI, Feature: CatInstructions, Kind: KindFile, Live: filepath.Join(cop, "copilot-instructions.md"), Rel: "cli/copilot-instructions.md"},
		{Name: "cli-instructions-dir", Surface: SurfaceCLI, Feature: CatInstructions, Kind: KindDir, Live: filepath.Join(cop, "instructions"), Rel: "cli/instructions"},
		{Name: "cli-skills", Surface: SurfaceCLI, Feature: CatSkills, Kind: KindDir, Live: filepath.Join(cop, "skills"), Rel: "cli/skills"},
		{Name: "cli-extensions", Surface: SurfaceCLI, Feature: CatExtensions, Kind: KindDir, Live: filepath.Join(cop, "extensions"), Rel: "cli/extensions"},
		{Name: "cli-hooks", Surface: SurfaceCLI, Feature: CatHooks, Kind: KindDir, Live: filepath.Join(cop, "hooks"), Rel: "cli/hooks"},
		{Name: "cli-installed-plugins", Surface: SurfaceCLI, Feature: CatPlugins, Kind: KindDir, Live: filepath.Join(cop, "installed-plugins"), Rel: "cli/installed-plugins"},
		{Name: "cli-mcp", Surface: SurfaceCLI, Feature: CatMCP, Kind: KindFile, Live: filepath.Join(cop, "mcp-config.json"), Rel: "cli/mcp-config.json"},
		{Name: "cli-settings", Surface: SurfaceCLI, Feature: FeatSettings, Kind: KindFile, Live: filepath.Join(cop, "settings.json"), Rel: "cli/settings.json"},
		{Name: "cli-permissions", Surface: SurfaceCLI, Feature: FeatSettings, Kind: KindFile, Live: filepath.Join(cop, "permissions-config.json"), Rel: "cli/permissions-config.json"},
		{Name: "cli-agents", Surface: SurfaceCLI, Feature: CatAgents, Kind: KindDir, Live: filepath.Join(cop, "agents"), Rel: "cli/agents"},

		// Cross-tool personal agent config (~/.agents)
		{Name: "agents-skills", Surface: SurfaceAgents, Feature: CatSkills, Kind: KindDir, Live: filepath.Join(agents, "skills"), Rel: "agents/skills"},
		{Name: "agents-skill-lock", Surface: SurfaceAgents, Feature: CatSkills, Kind: KindFile, Live: filepath.Join(agents, ".skill-lock.json"), Rel: "agents/.skill-lock.json"},

		// GitHub Copilot app (Tauri)
		{Name: "app-settings", Surface: SurfaceApp, Feature: FeatSettings, Kind: KindFile, Live: filepath.Join(cop, "m-settings.json"), Rel: "app/m-settings.json"},
		{Name: "app-mcp", Surface: SurfaceApp, Feature: CatMCP, Kind: KindFile, Live: filepath.Join(cop, "m-mcp-servers.json"), Rel: "app/m-mcp-servers.json"},
		{Name: "app-data-db", Surface: SurfaceApp, Feature: FeatDB, Kind: KindDBSnapshot, Live: filepath.Join(cop, "data.db"), Rel: "app/data.db"},

		// VS Code (stable)
		{Name: "code-settings", Surface: SurfaceVSCode, Feature: FeatSettings, Kind: KindJSONKeys, Live: filepath.Join(code, "settings.json"), Rel: "vscode/code/settings.copilot.json", KeyRE: copilotKeys},
		{Name: "code-mcp", Surface: SurfaceVSCode, Feature: CatMCP, Kind: KindFile, Live: filepath.Join(code, "mcp.json"), Rel: "vscode/code/mcp.json"},
		{Name: "code-prompts", Surface: SurfaceVSCode, Feature: CatPrompts, Kind: KindDir, Live: filepath.Join(code, "prompts"), Rel: "vscode/code/prompts"},
		{Name: "code-keybindings", Surface: SurfaceVSCode, Feature: FeatSettings, Kind: KindFile, Live: filepath.Join(code, "keybindings.json"), Rel: "vscode/code/keybindings.json", Optional: true},

		// VS Code (Insiders)
		{Name: "ins-settings", Surface: SurfaceInsiders, Feature: FeatSettings, Kind: KindJSONKeys, Live: filepath.Join(ins, "settings.json"), Rel: "vscode/code-insiders/settings.copilot.json", KeyRE: copilotKeys},
		{Name: "ins-mcp", Surface: SurfaceInsiders, Feature: CatMCP, Kind: KindFile, Live: filepath.Join(ins, "mcp.json"), Rel: "vscode/code-insiders/mcp.json"},
		{Name: "ins-prompts", Surface: SurfaceInsiders, Feature: CatPrompts, Kind: KindDir, Live: filepath.Join(ins, "prompts"), Rel: "vscode/code-insiders/prompts"},
		{Name: "ins-keybindings", Surface: SurfaceInsiders, Feature: FeatSettings, Kind: KindFile, Live: filepath.Join(ins, "keybindings.json"), Rel: "vscode/code-insiders/keybindings.json", Optional: true},

		// Session history (opt-in: --with-history only; heavy)
		{Name: "hist-data-db", Surface: SurfaceHistory, Feature: FeatHistory, Kind: KindHistory, Live: filepath.Join(cop, "data.db"), Rel: "history/data.db"},
		{Name: "hist-session-store", Surface: SurfaceHistory, Feature: FeatHistory, Kind: KindHistory, Live: filepath.Join(cop, "session-store.db"), Rel: "history/session-store.db"},
		{Name: "hist-session-state", Surface: SurfaceHistory, Feature: FeatHistory, Kind: KindHistory, Live: filepath.Join(cop, "session-state"), Rel: "history/session-state"},
		{Name: "hist-chats", Surface: SurfaceHistory, Feature: FeatHistory, Kind: KindHistory, Live: filepath.Join(cop, "chats"), Rel: "history/chats"},
	}
}

func vscodeDirs(home string) (code, insiders string) {
	switch runtime.GOOS {
	case "darwin":
		base := filepath.Join(home, "Library", "Application Support")
		return filepath.Join(base, "Code", "User"), filepath.Join(base, "Code - Insiders", "User")
	case "windows":
		base := os.Getenv("APPDATA")
		return filepath.Join(base, "Code", "User"), filepath.Join(base, "Code - Insiders", "User")
	default:
		base := filepath.Join(home, ".config")
		return filepath.Join(base, "Code", "User"), filepath.Join(base, "Code - Insiders", "User")
	}
}
