package cmd

import (
	"fmt"
	"os"

	"github.com/austenstone/gh-copilot-config/internal/profile"
	"github.com/austenstone/gh-copilot-config/internal/tui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	flagDryRun  bool
	flagAll     bool
	flagHistory bool
	flagDB      bool
	flagForce   bool
)

var rootCmd = &cobra.Command{
	Use:   "copilot-config",
	Short: "Save, restore, and toggle GitHub Copilot customization profiles",
	Long: "Manage named profiles of your GitHub Copilot customizations across the\n" +
		"Copilot CLI, the Copilot app, and VS Code. The empty 'clean' profile resets\n" +
		"to a vanilla setup. Run with no command in a terminal for the interactive TUI.",
	SilenceUsage:  true,
	SilenceErrors: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !interactive() {
			return cmd.Help()
		}
		m, err := newManager()
		if err != nil {
			return err
		}
		return tui.Run(m)
	},
}

// Execute runs the root command.
func Execute(version string) error {
	rootCmd.Version = version
	return rootCmd.Execute()
}

func init() {
	pf := rootCmd.PersistentFlags()
	pf.BoolVar(&flagDryRun, "dry-run", false, "print planned changes without touching disk")
	pf.BoolVar(&flagAll, "all", false, "include assets flagged optional (e.g. keybindings)")
	pf.BoolVar(&flagHistory, "with-history", false, "also back up / restore / clear session history (heavy)")
	pf.BoolVar(&flagDB, "with-db", false, "also snapshot Copilot databases (backup-only, heavy)")
	pf.BoolVarP(&flagForce, "force", "y", false, "skip confirmation prompts")

	rootCmd.AddCommand(listCmd, statusCmd, saveCmd, applyCmd, cleanCmd, onCmd, newCmd, rmCmd, diffCmd, tuiCmd)
}

func newManager() (*profile.Manager, error) {
	m, err := profile.Open()
	if err != nil {
		return nil, err
	}
	m.Out = os.Stdout
	m.DryRun = flagDryRun
	m.Optional = flagAll
	m.History = flagHistory
	m.DBSnapshot = flagDB
	return m, nil
}

// interactive reports whether a bare invocation should launch the TUI.
func interactive() bool {
	if os.Getenv("CC_NO_TUI") == "1" {
		return false
	}
	return term.IsTerminal(int(os.Stdout.Fd())) && term.IsTerminal(int(os.Stdin.Fd()))
}

// confirm prompts for y/N approval, bypassed by --force and --dry-run. In a
// non-interactive shell it refuses rather than guess.
func confirm(prompt string) bool {
	if flagForce || flagDryRun {
		return true
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, prompt, "Refusing without confirmation (use --force / -y).")
		return false
	}
	fmt.Fprintf(os.Stderr, "%s [y/N] ", prompt)
	var ans string
	fmt.Scanln(&ans)
	return ans == "y" || ans == "Y" || ans == "yes" || ans == "Yes"
}
