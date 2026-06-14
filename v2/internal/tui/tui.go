package tui

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/austenstone/copilot-config/internal/profile"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
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

// Semantic palette (Catppuccin: Latte on light terminals, Mocha on dark).
var (
	accent = lipgloss.AdaptiveColor{Light: "#8839ef", Dark: "#cba6f7"} // mauve
	dim    = lipgloss.AdaptiveColor{Light: "#8c8fa1", Dark: "#6c7086"} // overlay
	green  = lipgloss.AdaptiveColor{Light: "#40a02b", Dark: "#a6e3a1"}
	red    = lipgloss.AdaptiveColor{Light: "#d20f39", Dark: "#f38ba8"}
	selFg  = lipgloss.AdaptiveColor{Light: "#eff1f5", Dark: "#1e1e2e"} // base, for text on accent

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	subtleStyle = lipgloss.NewStyle().Foreground(dim)
	okStyle     = lipgloss.NewStyle().Foreground(green)
	errStyle    = lipgloss.NewStyle().Foreground(red)
	promptStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent)

	tabStyle       = lipgloss.NewStyle().Foreground(dim).Padding(0, 1)
	activeTabStyle = lipgloss.NewStyle().Bold(true).Foreground(selFg).Background(accent).Padding(0, 1)
)

// catShort maps category labels to compact tab titles.
var catShort = map[string]string{
	profile.CatInstructions: "Instr",
	profile.CatPrompts:      "Prompts",
	profile.CatAgents:       "Agents",
	profile.CatSubagents:    "Sub",
	profile.CatSkills:       "Skills",
	profile.CatHooks:        "Hooks",
	profile.CatMCP:          "MCP",
}

// ---- key bindings -------------------------------------------------------

type keyMap struct {
	Up, Down, Left, Right, Inspect, Apply, On, Clean, Save, New, Diff, Delete, Edit, DB, Status, Refresh, Help, Quit key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Inspect, k.Apply, k.Save, k.Diff, k.Delete, k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Inspect, k.Apply},
		{k.On, k.Clean, k.Save, k.New},
		{k.Diff, k.Delete, k.DB, k.Status},
		{k.Refresh, k.Help, k.Quit},
	}
}

var keys = keyMap{
	Up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Left:    key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "prev tab")),
	Right:   key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "next tab")),
	Inspect: key.NewBinding(key.WithKeys("enter", "i"), key.WithHelp("enter", "inspect")),
	Apply:   key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "apply")),
	On:      key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "re-apply last")),
	Clean:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clean")),
	Save:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "save")),
	New:     key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	Diff:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diff")),
	Delete:  key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "delete")),
	Edit:    key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "open in $EDITOR")),
	DB:      key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "toggle db snapshots")),
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
	modeDetail
	modePreview
)

// action identifies a user-initiated operation awaiting input or confirmation.
type action string

const (
	actApply  action = "apply"
	actClean  action = "clean"
	actDelete action = "delete"
	actSave   action = "save"
	actNew    action = "new"
)

// confirmAction describes a confirmable operation: the prompt shown, the
// in-flight label, the success status, and the engine call to run.
type confirmAction struct {
	prompt  func(target string) string
	working func(target string) string
	done    func(target string) string
	run     func(mgr *profile.Manager, target string) error
}

var confirmActions = map[action]confirmAction{
	actApply: {
		prompt:  func(t string) string { return "Apply profile '" + t + "' to your live Copilot config?" },
		working: func(t string) string { return "applying " + t + "…" },
		done:    func(t string) string { return "applied " + t },
		run:     func(mgr *profile.Manager, t string) error { return mgr.ApplyNamed(t) },
	},
	actClean: {
		prompt:  func(string) string { return "Reset live Copilot config to vanilla (apply 'clean')?" },
		working: func(string) string { return "applying clean…" },
		done:    func(string) string { return "applied clean" },
		run:     func(mgr *profile.Manager, _ string) error { return mgr.ApplyNamed("clean") },
	},
	actDelete: {
		prompt:  func(t string) string { return "Permanently delete profile '" + t + "'?" },
		working: func(t string) string { return "deleting " + t + "…" },
		done:    func(t string) string { return "deleted " + t },
		run:     func(mgr *profile.Manager, t string) error { return mgr.Remove(t) },
	},
}

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
	spin  spinner.Model

	mode        mode
	pending     action // action awaiting input/confirm
	target      string // profile the pending action targets
	selectAfter string // profile to highlight after the next reload
	status      string
	statusErr   bool
	busy        bool // an async action is in flight
	width       int
	height      int
	loaded      bool

	inv        profile.Inventory // categorized assets of the inspected profile
	detailName string            // profile shown in modeDetail
	tab        int               // active category tab
	itemCursor int               // selected item within the active tab
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
	st.Selected = st.Selected.Foreground(selFg).Background(accent).Bold(true)
	t.SetStyles(st)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(accent)

	return model{mgr: mgr, tbl: t, input: ti, help: help.New(), vp: viewport.New(0, 0), spin: sp}
}

// start marks an action in flight, sets a progress message, and kicks the
// spinner so slow operations (large saves, diffs) don't look frozen.
func (m model) start(status string, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m.busy, m.status, m.statusErr = true, status, false
	return m, tea.Batch(cmd, m.spin.Tick)
}

func (m model) Init() tea.Cmd { return tea.Batch(m.loadProfiles, m.spin.Tick) }

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

type inventoryMsg struct {
	name string
	inv  profile.Inventory
	err  error
}

// inspect walks a saved profile and categorizes its assets.
func (m model) inspect(name string) tea.Cmd {
	mgr := *m.mgr
	return func() tea.Msg {
		inv, err := mgr.Inspect(name)
		return inventoryMsg{name: name, inv: inv, err: err}
	}
}

type editorFinishedMsg struct{ err error }

// openInEditor suspends the TUI and opens a file in the user's editor.
func openInEditor(p string) tea.Cmd {
	editor := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR"), "vi")
	c := exec.Command(editor, p)
	return tea.ExecProcess(c, func(err error) tea.Msg { return editorFinishedMsg{err: err} })
}

// ---- update -------------------------------------------------------------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
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
		if m.selectAfter != "" {
			for i, p := range msg.profiles {
				if p.Name == m.selectAfter {
					m.tbl.SetCursor(i)
					break
				}
			}
			m.selectAfter = ""
		}
		return m, nil
	case actionMsg:
		m.busy = false
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
	case spinner.TickMsg:
		if !m.busy && m.loaded {
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case inventoryMsg:
		m.busy = false
		if msg.err != nil {
			m.status, m.statusErr = msg.err.Error(), true
			return m, nil
		}
		m.inv, m.detailName = msg.inv, msg.name
		m.tab, m.itemCursor, m.mode = 0, 0, modeDetail
		return m, nil
	case editorFinishedMsg:
		if msg.err != nil {
			m.status, m.statusErr = msg.err.Error(), true
		}
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
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

	case modePreview:
		switch msg.String() {
		case "q", "esc":
			m.mode = modeDetail
			return m, nil
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd

	case modeDetail:
		switch {
		case key.Matches(msg, keys.Quit) || msg.String() == "esc":
			m.mode = modeList
			return m, nil
		case key.Matches(msg, keys.Left):
			m.tab = (m.tab + len(profile.Categories) - 1) % len(profile.Categories)
			m.itemCursor = 0
			return m, nil
		case key.Matches(msg, keys.Right):
			m.tab = (m.tab + 1) % len(profile.Categories)
			m.itemCursor = 0
			return m, nil
		case key.Matches(msg, keys.Up):
			if m.itemCursor > 0 {
				m.itemCursor--
			}
			return m, nil
		case key.Matches(msg, keys.Down):
			if n := len(m.curItems()); n > 0 && m.itemCursor < n-1 {
				m.itemCursor++
			}
			return m, nil
		case key.Matches(msg, keys.Apply):
			m.pending, m.target, m.mode = actApply, m.detailName, modeConfirm
			return m, nil
		case key.Matches(msg, keys.Edit):
			if it, ok := m.curItem(); ok {
				return m, openInEditor(it.Path)
			}
			return m, nil
		case key.Matches(msg, keys.Inspect):
			if it, ok := m.curItem(); ok {
				content := it.Name
				if b, err := os.ReadFile(it.Path); err == nil {
					content = string(b)
				} else {
					content = "cannot read " + it.Path + ": " + err.Error()
				}
				m.vp.SetContent(content)
				m.vp.GotoTop()
				m.mode = modePreview
			}
			return m, nil
		}
		return m, nil

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
			case actSave:
				m.selectAfter = name
				return m.start("saving "+name+"…", m.action("saved "+name, func(mgr *profile.Manager) error { return mgr.SaveNamed(name) }))
			case actNew:
				m.selectAfter = name
				return m.start("creating "+name+"…", m.action("created "+name, func(mgr *profile.Manager) error { return mgr.New(name, "") }))
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case modeConfirm:
		if msg.String() == "y" || msg.String() == "Y" {
			m.mode = modeList
			a, t := confirmActions[m.pending], m.target
			return m.start(a.working(t), m.action(a.done(t), func(mgr *profile.Manager) error { return a.run(mgr, t) }))
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
	case key.Matches(msg, keys.DB):
		m.mgr.DBSnapshot = !m.mgr.DBSnapshot
		if m.mgr.DBSnapshot {
			m.status, m.statusErr = "db snapshots ON — saves now include Copilot databases (slow)", false
		} else {
			m.status, m.statusErr = "db snapshots OFF", false
		}
		return m, nil
	case key.Matches(msg, keys.Apply):
		if n := m.selected(); n != "" {
			m.pending, m.target, m.mode = actApply, n, modeConfirm
		}
		return m, nil
	case key.Matches(msg, keys.Inspect):
		if n := m.selected(); n != "" {
			return m.start("reading "+n+"…", m.inspect(n))
		}
		return m, nil
	case key.Matches(msg, keys.Clean):
		m.pending, m.target, m.mode = actClean, "clean", modeConfirm
		return m, nil
	case key.Matches(msg, keys.Delete):
		if n := m.selected(); n != "" && n != "clean" {
			m.pending, m.target, m.mode = actDelete, n, modeConfirm
		}
		return m, nil
	case key.Matches(msg, keys.On):
		return m.start("re-applying last…", m.action("re-applied last", func(mgr *profile.Manager) error {
			name := mgr.Last()
			if name == "" {
				name = "default"
			}
			return mgr.ApplyNamed(name)
		}))
	case key.Matches(msg, keys.Status):
		return m, m.action("status", writeStatus)
	case key.Matches(msg, keys.Diff):
		if n := m.selected(); n != "" {
			return m.start("diffing "+n+"…", m.action("diff "+n, func(mgr *profile.Manager) error { return writeDiff(mgr, n) }))
		}
		return m, nil
	case key.Matches(msg, keys.Save):
		return m.startInput(actSave)
	case key.Matches(msg, keys.New):
		return m.startInput(actNew)
	}

	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m model) startInput(a action) (tea.Model, tea.Cmd) {
	m.pending = a
	m.input.SetValue("")
	m.input.Focus()
	m.mode = modeInput
	return m, textinput.Blink
}

func (m model) selected() string {
	r := m.tbl.SelectedRow()
	if len(r) < 2 {
		return ""
	}
	return r[1]
}

func (m model) curItems() []profile.InvItem { return m.inv.Items[profile.Categories[m.tab]] }

func (m model) curItem() (profile.InvItem, bool) {
	items := m.curItems()
	if m.itemCursor < 0 || m.itemCursor >= len(items) {
		return profile.InvItem{}, false
	}
	return items[m.itemCursor], true
}

// ---- view ---------------------------------------------------------------

func (m model) View() string {
	if !m.loaded {
		return "\n  " + m.spin.View() + subtleStyle.Render("loading profiles…") + "\n"
	}
	switch m.mode {
	case modeDetail:
		return m.detailView()
	case modePreview:
		return m.previewView()
	}
	var b strings.Builder
	b.WriteString(titleStyle.Render("  copilot-config") + subtleStyle.Render("  ·  Copilot customization profiles"))
	if m.mgr.DBSnapshot {
		b.WriteString(okStyle.Render("  [db]"))
	}
	b.WriteString("\n\n")

	switch m.mode {
	case modeOutput:
		b.WriteString(boxStyle.Width(max(1, m.width-2)).Render(m.vp.View()))
		b.WriteString("\n" + subtleStyle.Render("  ↑/↓ scroll · q/esc back"))
		return b.String()
	case modeInput:
		label := "Save current live config as:"
		if m.pending == actNew {
			label = "New empty profile name:"
		}
		b.WriteString("  " + promptStyle.Render(label) + "\n\n  " + m.input.View() + "\n\n")
		b.WriteString(subtleStyle.Render("  enter confirm · esc cancel"))
		return b.String()
	case modeConfirm:
		b.WriteString("  " + promptStyle.Render(confirmActions[m.pending].prompt(m.target)) + "\n\n")
		b.WriteString(subtleStyle.Render("  y confirm · any other key cancel"))
		return b.String()
	}

	b.WriteString(m.tbl.View() + "\n")
	switch {
	case m.busy:
		b.WriteString("  " + m.spin.View() + promptStyle.Render(m.status) + subtleStyle.Render("  (working…)") + "\n")
	case m.status != "":
		style := okStyle
		if m.statusErr {
			style = errStyle
		}
		b.WriteString("  " + style.Render("• "+m.status) + "\n")
	default:
		b.WriteString("\n")
	}
	b.WriteString("  " + m.help.View(keys))
	return b.String()
}

func (m model) detailView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("  "+m.detailName) + subtleStyle.Render("  ·  profile detail") + "\n\n")

	b.WriteString("  ")
	for i, c := range profile.Categories {
		label := fmt.Sprintf("%s %d", catShort[c], m.inv.Count(c))
		style := tabStyle
		if i == m.tab {
			style = activeTabStyle
		}
		b.WriteString(style.Render(label) + " ")
	}
	b.WriteString("\n\n")

	items := m.curItems()
	if len(items) == 0 {
		b.WriteString(subtleStyle.Render("  (none)") + "\n")
	} else {
		visible, offset := windowItems(items, m.itemCursor, max(1, m.height-9))
		for i, it := range visible {
			if offset+i == m.itemCursor {
				b.WriteString("  " + promptStyle.Render("▸ "+it.Name) + "\n")
			} else {
				b.WriteString("    " + it.Name + "\n")
			}
		}
	}

	b.WriteString("\n" + subtleStyle.Render("  ←/→ category · ↑/↓ item · enter preview · e edit · a apply · q back"))
	return b.String()
}

func (m model) previewView() string {
	var b strings.Builder
	name := m.detailName
	if it, ok := m.curItem(); ok {
		name = it.Name
	}
	b.WriteString(titleStyle.Render("  "+name) + subtleStyle.Render("  ·  preview") + "\n\n")
	b.WriteString(boxStyle.Width(max(1, m.width-2)).Render(m.vp.View()))
	b.WriteString("\n" + subtleStyle.Render("  ↑/↓ scroll · q/esc back"))
	return b.String()
}

// windowItems returns the slice of items visible around the cursor, plus its
// offset, so long lists scroll instead of overflowing the screen.
func windowItems(items []profile.InvItem, cursor, height int) ([]profile.InvItem, int) {
	if height <= 0 || len(items) <= height {
		return items, 0
	}
	offset := cursor - height/2
	if offset < 0 {
		offset = 0
	}
	if offset > len(items)-height {
		offset = len(items) - height
	}
	return items[offset : offset+height], offset
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
