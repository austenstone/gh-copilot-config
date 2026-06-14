package cmd

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/austenstone/copilot-config/internal/profile"
	"github.com/austenstone/copilot-config/internal/tui"
	"github.com/spf13/cobra"
)

var (
	listSort    string
	listReverse bool
	newFrom     string
)

func init() {
	listCmd.Flags().StringVar(&listSort, "sort", "created", "sort key: created|modified|name")
	listCmd.Flags().BoolVarP(&listReverse, "reverse", "r", false, "reverse the list order")
	newCmd.Flags().StringVar(&newFrom, "from", "", "base profile to copy from")
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
			fmt.Fprintf(tw, "%s%s\t%s\t%s\t%s\n", mark, p.Name, profile.FmtDate(p.Created), profile.FmtDate(p.Modified), profile.HumanSize(p.Size))
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
		fmt.Printf("active profile : %s\n", orNone(m.Active()))
		fmt.Printf("last non-clean : %s\n", orNone(m.Last()))
		fmt.Printf("profiles dir   : %s\n", m.Dir)
		if a := m.Active(); a != "" && m.Exists(a) {
			out, err := m.Diff(a)
			if err == nil {
				if out == "" {
					fmt.Printf("live is in sync with %q\n", a)
				} else {
					fmt.Printf("live has drifted from %q (run: copilot-config diff)\n", a)
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
	RunE:    func(cmd *cobra.Command, args []string) error { return runApply(args[0]) },
}

var cleanCmd = &cobra.Command{
	Use:     "clean",
	Aliases: []string{"off"},
	Short:   "Reset live Copilot config to vanilla (apply the empty 'clean' profile)",
	Args:    cobra.NoArgs,
	RunE:    func(cmd *cobra.Command, args []string) error { return runApply("clean") },
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
		return runApply(name)
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
		name := m.Active()
		if len(args) == 1 {
			name = args[0]
		}
		if name == "" {
			return fmt.Errorf("no active profile; pass a name")
		}
		out, err := m.Diff(name)
		if err != nil {
			return err
		}
		if out == "" {
			fmt.Printf("no drift: live matches profile %q\n", name)
			return nil
		}
		fmt.Printf("drift vs profile %q (- profile, + live):\n%s\n", name, out)
		return nil
	},
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
func runApply(name string) error {
	m, err := newManager()
	if err != nil {
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

func orNone(s string) string {
	if s == "" {
		return "<none>"
	}
	return s
}
