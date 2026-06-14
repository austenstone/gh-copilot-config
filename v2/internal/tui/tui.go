package tui

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/austenstone/copilot-config/internal/profile"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Run launches the interactive TUI against a manager.
func Run(m *profile.Manager) error {
	_, err := tea.NewProgram(newModel(m), tea.WithAltScreen()).Run()
	return err
}

// ---- styles -------------------------------------------------------------

var (
	accent      = lipgloss.Color("212")
	dim         = lipgloss.Color("241")
	green       = lipgloss.Color("42")
	red         = lipgloss.Color("203")
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	subtleStyle = lipgloss.NewStyle().Foreground(dim)
	okStyle     = lipgloss.NewStyle().Foreground(green)
	errStyle    = lipgloss.NewStyle().Foreground(red)
	promptStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent)
)

// ---- key bindings -------------------------------------------------------

type keyMap struct {
	Up, Down, Apply, On, Clean, Save, New, Diff, Delete, Status, Refresh, Help, Quit key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Apply, k.Save, k.New, k.Diff, k.Delete, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Apply, k.On},
		{k.Clean, k.Save, k.New, k.Diff},
		{k.Delete, k.Status, k.Refresh, k.Quit},
	}
}

var keys = keyMap{
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Apply:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
	On:      key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "re-apply last")),
	Clean:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clean")),
	Save:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "save")),
	New:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	Diff:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diff")),
	Delete:  key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "delete")),
	Status:  key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "status")),
	Refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

// ---- model --------------------------------------------------------------

type mode int

const (
	modeList mode = iota
	modeOutput
	modeInput
	modeConfirm
)

type profilesMsg struct {
	profiles []profile.Profile
	err      error
}

type actionMsg struct {
	title string
	out   string
	err   error
}

type model struct {
	mgr   *profile.Manager
	tbl   table.Model
	help  help.Model
	input textinput.Model
	vp    viewport.Model

	mode      mode
	pending   string // action awaiting input/confirm
	target    string // profile the pending action targets
	status    string
	statusErr bool
	width     int
	height    int
	loaded    bool
}

func newModel(mgr *profile.Manager) model {
	ti := textinput.New()
	ti.Placeholder = "profile name"
	ti.CharLimit = 64

	cols := []table.Column{
		{Title: "", Width: 2},
		{Title: "PROFILE", Width: 18},
		{Title: "CREATED", Width: 12},
		{Title: "MODIFIED", Width: 12},
		{Title: "SIZE", Width: 8},
	}
	t := table.New(table.WithColumns(cols), table.WithFocused(true), table.WithHeight(10))
	st := table.DefaultStyles()
	st.Header = st.Header.BorderStyle(lipgloss.NormalBorder()).BorderForeground(dim).BorderBottom(true).Bold(true)
	st.Selected = st.Selected.Foreground(lipgloss.Color("231")).Background(accent).Bold(true)
	t.SetStyles(st)

	return model{mgr: mgr, tbl: t, input: ti, help: help.New(), vp: viewport.New(0, 0)}
}

func (m model) Init() tea.Cmd { return m.loadProfiles }

// ---- commands (side effects) --------------------------------------------

func (m model) loadProfiles() tea.Msg {
	ps, err := m.mgr.Profiles("created", false)
	return profilesMsg{profiles: ps, err: err}
}

// action runs an engine operation off the UI loop, capturing its output.
func (m model) action(title string, fn func(mgr *profile.Manager) error) tea.Cmd {
	mgr := *m.mgr
	return func() tea.Msg {
		var buf bytes.Buffer
		mgr.Out = &buf
		err := fn(&mgr)
		return actionMsg{title: title, out: strings.TrimSpace(buf.String()), err: err}
	}
}

// ---- update -------------------------------------------------------------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.tbl.SetHeight(max(5, msg.Height-9))
		m.vp.Width, m.vp.Height = msg.Width-2, max(5, msg.Height-7)
		m.help.Width = msg.Width
		return m, nil
	case profilesMsg:
		m.loaded = true
		if msg.err != nil {
			m.status, m.statusErr = msg.err.Error(), true
			return m, nil
		}
		m.tbl.SetRows(rows(msg.profiles))
		return m, nil
	case actionMsg:
		if msg.err != nil {
			m.status, m.statusErr = msg.err.Error(), true
		} else {
			m.status, m.statusErr = msg.title, false
		}
		if msg.out != "" {
			m.mode = modeOutput
			m.vp.SetContent(msg.out)
			m.vp.GotoTop()
		}
		return m, m.loadProfiles
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.mode {
	case modeOutput:
		switch msg.String() {
		case "q", "esc", "enter":
			m.mode = modeList
			return m, nil
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd

	case modeInput:
		switch msg.String() {
		case "esc":
			m.mode = modeList
			m.input.Blur()
			return m, nil
		case "enter":
			name := strings.TrimSpace(m.input.Value())
			m.mode = modeList
			m.input.Blur()
			if name == "" {
				return m, nil
			}
			switch m.pending {
			case "save":
				return m, m.action("saved "+name, func(mgr *profile.Manager) error { return mgr.SaveNamed(name) })
			case "new":
				return m, m.action("created "+name, func(mgr *profile.Manager) error { return mgr.New(name, "") })
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case modeConfirm:
		if msg.String() == "y" || msg.String() == "Y" {
			m.mode = modeList
			return m, m.confirmed()
		}
		m.mode = modeList
		m.status, m.statusErr = "cancelled", false
		return m, nil
	}

	// modeList
	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		return m, nil
	case key.Matches(msg, keys.Refresh):
		m.status = ""
		return m, m.loadProfiles
	case key.Matches(msg, keys.Apply):
		if n := m.selected(); n != "" {
			m.pending, m.target, m.mode = "apply", n, modeConfirm
		}
		return m, nil
	case key.Matches(msg, keys.Clean):
		m.pending, m.target, m.mode = "clean", "clean", modeConfirm
		return m, nil
	case key.Matches(msg, keys.Delete):
		if n := m.selected(); n != "" && n != "clean" {
			m.pending, m.target, m.mode = "delete", n, modeConfirm
		}
		return m, nil
	case key.Matches(msg, keys.On):
		return m, m.action("re-applied last", func(mgr *profile.Manager) error {
			name := mgr.Last()
			if name == "" {
				name = "default"
			}
			return mgr.ApplyNamed(name)
		})
	case key.Matches(msg, keys.Status):
		return m, m.action("status", writeStatus)
	case key.Matches(msg, keys.Diff):
		if n := m.selected(); n != "" {
			return m, m.action("diff "+n, func(mgr *profile.Manager) error { return writeDiff(mgr, n) })
		}
		return m, nil
	case key.Matches(msg, keys.Save):
		return m.startInput("save")
	case key.Matches(msg, keys.New):
		return m.startInput("new")
	}

	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m model) startInput(action string) (tea.Model, tea.Cmd) {
	m.pending = action
	m.input.SetValue("")
	m.input.Focus()
	m.mode = modeInput
	return m, textinput.Blink
}

func (m model) confirmed() tea.Cmd {
	n := m.target
	switch m.pending {
	case "apply":
		return m.action("applied "+n, func(mgr *profile.Manager) error { return mgr.ApplyNamed(n) })
	case "clean":
		return m.action("applied clean", func(mgr *profile.Manager) error { return mgr.ApplyNamed("clean") })
	case "delete":
		return m.action("deleted "+n, func(mgr *profile.Manager) error { return mgr.Remove(n) })
	}
	return nil
}

func (m model) selected() string {
	r := m.tbl.SelectedRow()
	if len(r) < 2 {
		return ""
	}
	return r[1]
}

// ---- view ---------------------------------------------------------------

func (m model) View() string {
	if !m.loaded {
		return "\n  loading profiles…\n"
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("  copilot-config") + subtleStyle.Render("  ·  Copilot customization profiles") + "\n\n")

	switch m.mode {
	case modeOutput:
		b.WriteString(boxStyle.Width(max(1, m.width-2)).Render(m.vp.View()))
		b.WriteString("\n" + subtleStyle.Render("  ↑/↓ scroll · q/esc back"))
		return b.String()
	case modeInput:
		label := "Save current live config as:"
		if m.pending == "new" {
			label = "New empty profile name:"
		}
		b.WriteString("  " + promptStyle.Render(label) + "\n\n  " + m.input.View() + "\n\n")
		b.WriteString(subtleStyle.Render("  enter confirm · esc cancel"))
		return b.String()
	case modeConfirm:
		b.WriteString("  " + promptStyle.Render(m.question()) + "\n\n")
		b.WriteString(subtleStyle.Render("  y confirm · any other key cancel"))
		return b.String()
	}

	b.WriteString(m.tbl.View() + "\n")
	if m.status != "" {
		style := okStyle
		if m.statusErr {
			style = errStyle
		}
		b.WriteString("  " + style.Render("• "+m.status) + "\n")
	} else {
		b.WriteString("\n")
	}
	b.WriteString("  " + m.help.View(keys))
	return b.String()
}

func (m model) question() string {
	switch m.pending {
	case "apply":
		return "Apply profile '" + m.target + "' to your live Copilot config?"
	case "clean":
		return "Reset live Copilot config to vanilla (apply 'clean')?"
	case "delete":
		return "Permanently delete profile '" + m.target + "'?"
	}
	return "Continue?"
}

func rows(ps []profile.Profile) []table.Row {
	out := make([]table.Row, 0, len(ps))
	for _, p := range ps {
		mark := ""
		if p.Active {
			mark = "*"
		}
		out = append(out, table.Row{mark, p.Name, profile.FmtDate(p.Created), profile.FmtDate(p.Modified), profile.HumanSize(p.Size)})
	}
	return out
}

func writeStatus(mgr *profile.Manager) error {
	active, last := mgr.Active(), mgr.Last()
	if active == "" {
		active = "<none>"
	}
	if last == "" {
		last = "<none>"
	}
	fmt.Fprintf(mgr.Out, "active profile : %s\nlast non-clean : %s\nprofiles dir   : %s\n", active, last, mgr.Dir)
	if a := mgr.Active(); a != "" && mgr.Exists(a) {
		if out, err := mgr.Diff(a); err == nil {
			if out == "" {
				fmt.Fprintf(mgr.Out, "live is in sync with %q\n", a)
			} else {
				fmt.Fprintf(mgr.Out, "live has drifted from %q\n", a)
			}
		}
	}
	return nil
}

func writeDiff(mgr *profile.Manager, name string) error {
	out, err := mgr.Diff(name)
	if err != nil {
		return err
	}
	if out == "" {
		fmt.Fprintf(mgr.Out, "no drift: live matches %q\n", name)
	} else {
		fmt.Fprintln(mgr.Out, out)
	}
	return nil
}
