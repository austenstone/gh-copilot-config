package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/austenstone/gh-copilot-config/internal/profile"
	"github.com/austenstone/gh-copilot-config/internal/tui"
	"github.com/spf13/cobra"
)

var (
	listSort    string
	listReverse bool
	newFrom     string

	saveSurface, saveFeature   string
	applySurface, applyFeature string
	diffSurface, diffFeature   string
	diffSummaryFlag            bool
)

const (
	surfaceFlagHelp = "limit to surfaces (comma-separated: cli,vscode,insiders,app,agents,history)"
	featureFlagHelp = "limit to features (comma-separated: instructions,prompts,agents,skills,hooks,mcp,extensions,plugins,settings,db,history)"
)

func init() {
	listCmd.Flags().StringVar(&listSort, "sort", "created", "sort key: created|modified|name")
	listCmd.Flags().BoolVarP(&listReverse, "reverse", "r", false, "reverse the list order")
	newCmd.Flags().StringVar(&newFrom, "from", "", "base profile to copy from")

	saveCmd.Flags().StringVar(&saveSurface, "surface", "", surfaceFlagHelp)
	saveCmd.Flags().StringVar(&saveFeature, "feature", "", featureFlagHelp)
	applyCmd.Flags().StringVar(&applySurface, "surface", "", surfaceFlagHelp)
	applyCmd.Flags().StringVar(&applyFeature, "feature", "", featureFlagHelp)
	diffCmd.Flags().StringVar(&diffSurface, "surface", "", surfaceFlagHelp)
	diffCmd.Flags().StringVar(&diffFeature, "feature", "", featureFlagHelp)
	diffCmd.Flags().BoolVar(&diffSummaryFlag, "summary", false, "list changed paths only, not the full patch")
}

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "List profiles (* = active)",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := newManager()
		if err != nil {
			return err
		}
		ps, err := m.Profiles(listSort, listReverse)
		if err != nil {
			return err
		}
		if flagJSON {
			return emitJSON(newListJSON(m, ps))
		}
		if len(ps) == 0 {
			fmt.Println("no profiles yet — create one with: copilot-config save <name>")
			return nil
		}
		tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(tw, "  PROFILE\tCREATED\tMODIFIED\tSIZE")
		for _, p := range ps {
			mark := "  "
			if p.Active {
				mark = "* "
			}
			fmt.Fprintf(tw, "%s%s\t%s\t%s\t%s\n", mark, p.Name, profile.FmtDate(p.Created), profile.FmtAgo(p.Modified), profile.HumanSize(p.Size))
		}
		tw.Flush()
		if last := m.Last(); last != "" {
			fmt.Printf("last non-clean: %s\n", last)
		}
		return nil
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show active profile and drift vs live",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := newManager()
		if err != nil {
			return err
		}
		active := m.Active()
		exists := active != "" && m.Exists(active)
		if flagJSON {
			out := statusJSON{Active: active, Last: m.Last(), Dir: m.Dir, Exists: exists}
			if exists {
				patch, derr := m.Diff(active)
				if derr == nil {
					drift := patch != ""
					inSync := !drift
					out.Drift = &drift
					out.InSync = &inSync
				}
			}
			return emitJSON(out)
		}
		fmt.Printf("active profile : %s\n", orNone(active))
		fmt.Printf("last non-clean : %s\n", orNone(m.Last()))
		fmt.Printf("profiles dir   : %s\n", m.Dir)
		if exists {
			out, err := m.Diff(active)
			if err == nil {
				if out == "" {
					fmt.Printf("live is in sync with %q\n", active)
				} else {
					fmt.Printf("live has drifted from %q (run: copilot-config diff)\n", active)
				}
			}
		}
		return nil
	},
}

var saveCmd = &cobra.Command{
	Use:   "save <name>",
	Short: "Snapshot current live config into a profile",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if err := validName(name); err != nil {
			return err
		}
		m, err := newManager()
		if err != nil {
			return err
		}
		if err := scope(m, saveSurface, saveFeature); err != nil {
			return err
		}
		if m.Exists(name) && !confirm(fmt.Sprintf("profile %q exists, overwrite it?", name)) {
			return fmt.Errorf("save aborted")
		}
		return m.SaveNamed(name)
	},
}

var applyCmd = &cobra.Command{
	Use:     "apply <name>",
	Aliases: []string{"restore"},
	Short:   "Apply a profile to live",
	Args:    cobra.ExactArgs(1),
	RunE:    func(cmd *cobra.Command, args []string) error { return runApply(args[0], applySurface, applyFeature) },
}

var cleanCmd = &cobra.Command{
	Use:     "clean",
	Aliases: []string{"off"},
	Short:   "Reset live Copilot config to vanilla (apply the empty 'clean' profile)",
	Args:    cobra.NoArgs,
	RunE:    func(cmd *cobra.Command, args []string) error { return runApply("clean", "", "") },
}

var onCmd = &cobra.Command{
	Use:   "on [name]",
	Short: "Re-apply the last non-clean profile (or <name>/default)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := ""
		if len(args) == 1 {
			name = args[0]
		}
		if name == "" {
			m, err := newManager()
			if err != nil {
				return err
			}
			if name = m.Last(); name == "" {
				name = "default"
			}
		}
		return runApply(name, "", "")
	},
}

var newCmd = &cobra.Command{
	Use:   "new <name>",
	Short: "Create a profile (empty, or copied from --from)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		if err := validName(name); err != nil {
			return err
		}
		m, err := newManager()
		if err != nil {
			return err
		}
		return m.New(name, newFrom)
	},
}

var rmCmd = &cobra.Command{
	Use:     "rm <name>",
	Aliases: []string{"delete"},
	Short:   "Delete a profile",
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		m, err := newManager()
		if err != nil {
			return err
		}
		if !m.Exists(name) {
			return fmt.Errorf("no such profile %q", name)
		}
		if !confirm(fmt.Sprintf("permanently delete profile %q?", name)) {
			return fmt.Errorf("rm aborted")
		}
		return m.Remove(name)
	},
}

var diffCmd = &cobra.Command{
	Use:   "diff [name]",
	Short: "Diff live config against a profile (default: active)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := newManager()
		if err != nil {
			return err
		}
		if err := scope(m, diffSurface, diffFeature); err != nil {
			return err
		}
		name := m.Active()
		if len(args) == 1 {
			name = args[0]
		}
		if name == "" {
			return fmt.Errorf("no active profile; pass a name")
		}
		if diffSummaryFlag {
			changes, err := m.DiffSummary(name)
			if err != nil {
				return err
			}
			if flagJSON {
				return emitJSON(newDiffSummaryJSON(name, changes))
			}
			if len(changes) == 0 {
				fmt.Printf("no drift: live matches profile %q\n", name)
				return nil
			}
			fmt.Printf("drift vs profile %q (%d changed):\n", name, len(changes))
			for _, c := range changes {
				fmt.Printf("  %-9s %s\n", c.Kind, c.Path)
			}
			return nil
		}
		out, err := m.Diff(name)
		if err != nil {
			return err
		}
		if flagJSON {
			return emitJSON(diffJSON{Name: name, Drift: out != "", Patch: out})
		}
		if out == "" {
			fmt.Printf("no drift: live matches profile %q\n", name)
			return nil
		}
		fmt.Printf("drift vs profile %q (- profile, + live):\n%s\n", name, out)
		return nil
	},
}

var inspectCmd = &cobra.Command{
	Use:     "inspect [name]",
	Aliases: []string{"show"},
	Short:   "Inspect a profile's contents, grouped by surface and feature (default: active)",
	Args:    cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := newManager()
		if err != nil {
			return err
		}
		name := m.Active()
		if len(args) == 1 {
			name = args[0]
		}
		if name == "" {
			return fmt.Errorf("no active profile; pass a name")
		}
		if !m.Exists(name) {
			return fmt.Errorf("no such profile %q", name)
		}
		inv, err := m.Inspect(name)
		if err != nil {
			return err
		}
		if flagJSON {
			return emitJSON(newInspectJSON(name, inv))
		}
		printInspect(name, inv)
		return nil
	},
}

func printInspect(name string, inv profile.Inventory) {
	surfaces := inv.Surfaces()
	if len(surfaces) == 0 {
		fmt.Printf("profile %q is empty\n", name)
		return
	}
	fmt.Printf("profile %q:\n", name)
	for _, s := range surfaces {
		fmt.Printf("  %s (%d)\n", string(s), inv.SurfaceTotal(s))
		for _, cat := range profile.Categories {
			items := inv.Items[s][cat]
			if len(items) == 0 {
				continue
			}
			fmt.Printf("    %s:\n", cat)
			for _, it := range items {
				fmt.Printf("      - %s\n", it.Name)
			}
		}
	}
}

var tuiCmd = &cobra.Command{
	Use:     "tui",
	Aliases: []string{"ui"},
	Short:   "Launch the interactive TUI",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := newManager()
		if err != nil {
			return err
		}
		return tui.Run(m)
	},
}

// runApply handles the apply flow shared by apply/clean/on, including the
// history-write guard and its confirmations.
func runApply(name, surfaceCSV, featureCSV string) error {
	m, err := newManager()
	if err != nil {
		return err
	}
	if err := scope(m, surfaceCSV, featureCSV); err != nil {
		return err
	}
	if !m.Exists(name) {
		return fmt.Errorf("no such profile %q", name)
	}
	if flagHistory && !flagDryRun {
		if m.HistoryLocked() && !flagForce {
			return fmt.Errorf("session DBs are open — quit the GitHub app and Copilot CLI first (or pass --force)")
		}
		switch {
		case m.ProfileHasHistory(name):
			if !confirm(fmt.Sprintf("apply --with-history REPLACES your live session history with %q. Continue?", name)) {
				return fmt.Errorf("apply aborted")
			}
		case name == "clean":
			if !confirm("clean --with-history DELETES your live session history (the app recreates an empty set). Continue?") {
				return fmt.Errorf("clean aborted")
			}
		}
	}
	return m.ApplyNamed(name)
}

func validName(name string) error {
	if strings.HasPrefix(name, "_") {
		return fmt.Errorf("names starting with _ are reserved")
	}
	return nil
}

// scope applies --surface / --feature filters to a manager. Empty strings leave
// the manager unfiltered (all surfaces and features).
func scope(m *profile.Manager, surfaceCSV, featureCSV string) error {
	surfaces, err := profile.ParseSurfaces(surfaceCSV)
	if err != nil {
		return err
	}
	features, err := profile.ParseFeatures(featureCSV)
	if err != nil {
		return err
	}
	m.Surfaces = surfaces
	m.Features = features
	return nil
}

func orNone(s string) string {
	if s == "" {
		return "<none>"
	}
	return s
}
