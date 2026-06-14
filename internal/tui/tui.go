package tui

import (
	"bytes"
	"cmp"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/austenstone/gh-copilot-config/internal/profile"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Run launches the interactive TUI against a manager.
func Run(m *profile.Manager) error {
	_, err := tea.NewProgram(newModel(m), tea.WithAltScreen(), tea.WithMouseCellMotion()).Run()
	return err
}

// ---- styles -------------------------------------------------------------

// Semantic palette in GitHub's colors: a fixed GitHub blue accent (so it reads
// the same regardless of the terminal's ANSI palette) with green=success,
// red=failure, and a muted gray for secondary text, matching the gh CLI.
var (
	accent  = lipgloss.AdaptiveColor{Light: "#0969da", Dark: "#2f81f7"} // GitHub blue
	dim     = lipgloss.AdaptiveColor{Light: "245", Dark: "242"}         // gh's muted gray
	green   = lipgloss.Color("2")                                       // success
	red     = lipgloss.Color("1")                                       // failure
	selFg   = lipgloss.Color("15")                                      // white text on a colored bar
	surface = lipgloss.AdaptiveColor{Light: "254", Dark: "236"}         // bar background

	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	subtleStyle = lipgloss.NewStyle().Foreground(dim)
	okStyle     = lipgloss.NewStyle().Foreground(green)
	errStyle    = lipgloss.NewStyle().Foreground(red)
	promptStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent)
	ruleStyle   = lipgloss.NewStyle().Foreground(dim)

	statusBarStyle = lipgloss.NewStyle().Foreground(dim).Background(surface)
	modePillStyle  = lipgloss.NewStyle().Bold(true).Foreground(selFg).Background(accent).Padding(0, 1)
	selRowStyle    = lipgloss.NewStyle().Bold(true).Foreground(selFg).Background(accent)
	okMarkStyle    = lipgloss.NewStyle().Foreground(green).Background(surface)
	errMarkStyle   = lipgloss.NewStyle().Foreground(red).Background(surface)

	tabStyle       = lipgloss.NewStyle().Foreground(dim).Padding(0, 1)
	activeTabStyle = lipgloss.NewStyle().Bold(true).Foreground(selFg).Background(accent).Padding(0, 1)
)

// catShort maps category labels to compact tab titles.
var catShort = map[string]string{
	profile.CatInstructions: "Instr",
	profile.CatPrompts:      "Prompts",
	profile.CatAgents:       "Agents",
	profile.CatSkills:       "Skills",
	profile.CatHooks:        "Hooks",
	profile.CatMCP:          "MCP",
	profile.CatExtensions:   "Ext",
	profile.CatPlugins:      "Plugins",
}

// surfaceShort maps surfaces to compact tab titles.
var surfaceShort = map[profile.Surface]string{
	profile.SurfaceCLI:      "CLI",
	profile.SurfaceVSCode:   "VS Code",
	profile.SurfaceInsiders: "Insiders",
	profile.SurfaceApp:      "App",
	profile.SurfaceAgents:   "Agents",
	profile.SurfaceHistory:  "History",
}

func surfaceLabel(s profile.Surface) string {
	if l, ok := surfaceShort[s]; ok {
		return l
	}
	return string(s)
}

// ---- key bindings -------------------------------------------------------

type keyMap struct {
	Up, Down, Left, Right, PrevSurface, NextSurface, Inspect, Apply, On, Clean, Save, New, Diff, Delete, Edit, DB, Status, Refresh, Help, Quit key.Binding
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
	Up:          key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:        key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Left:        key.NewBinding(key.WithKeys("left", "h"), key.WithHelp("←/h", "prev category")),
	Right:       key.NewBinding(key.WithKeys("right", "l"), key.WithHelp("→/l", "next category")),
	PrevSurface: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("⇧tab", "prev surface")),
	NextSurface: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next surface")),
	Inspect:     key.NewBinding(key.WithKeys("enter", "i"), key.WithHelp("enter", "inspect")),
	Apply:       key.NewBinding(key.WithKeys("a"), key.WithHelp("a", "apply")),
	On:          key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "re-apply last")),
	Clean:       key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clean")),
	Save:        key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "save")),
	New:         key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	Diff:        key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diff")),
	Delete:      key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "delete")),
	Edit:        key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "open in editor")),
	DB:          key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "toggle db snapshots")),
	Status:      key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "status")),
	Refresh:     key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	Help:        key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
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

	mode           mode
	pending        action // action awaiting input/confirm
	target         string // profile the pending action targets
	selectAfter    string // profile to highlight after the next reload
	status         string
	statusErr      bool
	busy           bool // an async action is in flight
	previewLoading bool // a preview is rendering off the UI loop
	width          int
	height         int
	loaded         bool

	inv        profile.Inventory // categorized assets of the inspected profile
	detailName string            // profile shown in modeDetail
	surfaceTab int               // active surface tab
	tab        int               // active category tab within the surface
	itemCursor int               // selected item within the active tab
	lastWheel  time.Time         // throttles high-res scroll bursts to one step
}

func newModel(mgr *profile.Manager) model {
	ti := textinput.New()
	ti.Placeholder = "profile name"
	ti.CharLimit = 64

	cols := []table.Column{
		{Title: "", Width: 2},
		{Title: "PROFILE", Width: 18},
		{Title: "CREATED", Width: 12},
		{Title: "MODIFIED", Width: 14},
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

func (m model) loadProfiles() (msg tea.Msg) {
	defer func() {
		if r := recover(); r != nil {
			msg = profilesMsg{err: fmt.Errorf("panic: %v", r)}
		}
	}()
	ps, err := m.mgr.Profiles("created", false)
	return profilesMsg{profiles: ps, err: err}
}

// action runs an engine operation off the UI loop, capturing its output.
// Bubble Tea does not recover panics raised inside commands, so we guard here
// to surface a crash as a status message rather than wrecking the terminal.
func (m model) action(title string, fn func(mgr *profile.Manager) error) tea.Cmd {
	mgr := *m.mgr
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				msg = actionMsg{title: title, err: fmt.Errorf("panic: %v", r)}
			}
		}()
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
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				msg = inventoryMsg{name: name, err: fmt.Errorf("panic: %v", r)}
			}
		}()
		inv, err := mgr.Inspect(name)
		return inventoryMsg{name: name, inv: inv, err: err}
	}
}

type previewMsg struct {
	name    string
	content string
}

// readFile loads a file's contents off the UI loop for preview, rendering
// markdown with glamour and syntax-highlighting everything else.
func readFile(name, path string, width int) tea.Cmd {
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				msg = previewMsg{name: name, content: fmt.Sprintf("panic: %v", r)}
			}
		}()
		b, err := os.ReadFile(path)
		if err != nil {
			return previewMsg{name: name, content: "cannot read " + path + ": " + err.Error()}
		}
		return previewMsg{name: name, content: render(string(b), path, width)}
	}
}

type editorFinishedMsg struct{ err error }

// openInEditor suspends the TUI and opens a file in the user's editor. When
// neither $VISUAL nor $EDITOR is set, it falls back to the OS default opener.
func openInEditor(p string) tea.Cmd {
	var c *exec.Cmd
	if editor := cmp.Or(os.Getenv("VISUAL"), os.Getenv("EDITOR")); editor != "" {
		c = exec.Command(editor, p)
	} else {
		switch runtime.GOOS {
		case "darwin":
			c = exec.Command("open", "-W", "-t", p)
		case "windows":
			c = exec.Command("cmd", "/c", "start", "/wait", "", p)
		default:
			c = exec.Command("xdg-open", p)
		}
	}
	return tea.ExecProcess(c, func(err error) tea.Msg { return editorFinishedMsg{err: err} })
}

// ---- update -------------------------------------------------------------

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.help.Width = msg.Width
		m.vp.Width = max(1, msg.Width-4)
		m.vp.Height = max(1, msg.Height-5) // matches boxed() chrome
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
		if !m.busy && !m.previewLoading && m.loaded {
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
		m.status, m.statusErr = "", false
		m.inv, m.detailName = msg.inv, msg.name
		m.surfaceTab, m.tab, m.itemCursor, m.mode = 0, 0, 0, modeDetail
		return m, nil
	case editorFinishedMsg:
		if msg.err != nil {
			m.status, m.statusErr = msg.err.Error(), true
		}
		return m, nil
	case previewMsg:
		m.previewLoading = false
		m.vp.SetContent(msg.content)
		m.vp.GotoTop()
		m.mode = modePreview
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
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
		case key.Matches(msg, keys.NextSurface):
			if n := len(m.surfaces()); n > 0 {
				m.surfaceTab = (m.surfaceTab + 1) % n
				m.tab, m.itemCursor = 0, 0
			}
			return m, nil
		case key.Matches(msg, keys.PrevSurface):
			if n := len(m.surfaces()); n > 0 {
				m.surfaceTab = (m.surfaceTab + n - 1) % n
				m.tab, m.itemCursor = 0, 0
			}
			return m, nil
		case key.Matches(msg, keys.Left):
			if n := len(m.surfaceFeatures()); n > 0 {
				m.tab = (m.tab + n - 1) % n
			}
			m.itemCursor = 0
			return m, nil
		case key.Matches(msg, keys.Right):
			if n := len(m.surfaceFeatures()); n > 0 {
				m.tab = (m.tab + 1) % n
			}
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
				m.mode, m.previewLoading = modePreview, true
				return m, tea.Batch(readFile(it.Name, it.Path, max(1, m.width-4)), m.spin.Tick)
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
	case key.Matches(msg, keys.Quit) || msg.String() == "esc":
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

// listDataTop is the screen row of the first profile row: title, rule, and a
// blank line from heading(), then the table's header and its bottom border.
const listDataTop = 5

// wheelStep coalesces a burst of high-resolution scroll events (macOS momentum
// scrolling on Retina displays fires many per gesture) into a single step.
const wheelStep = 80 * time.Millisecond

// handleMouse routes wheel scrolling and click-to-select. The profile table has
// no native mouse support, so clicks are mapped to row cursor moves by hand.
func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	wheel := msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown

	// Discrete list/detail navigation advances one item per gesture; viewport
	// content (preview/output) scrolls freely so reading long files stays smooth.
	if wheel && (m.mode == modeList || m.mode == modeDetail) {
		if time.Since(m.lastWheel) < wheelStep {
			return m, nil
		}
		m.lastWheel = time.Now()
	}

	switch m.mode {
	case modeList:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			m.tbl.MoveUp(1)
			return m, nil
		case tea.MouseButtonWheelDown:
			m.tbl.MoveDown(1)
			return m, nil
		}
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft {
			if row := msg.Y - listDataTop; row >= 0 && row < len(m.tbl.Rows()) {
				m.tbl.SetCursor(row)
				if n := m.selected(); n != "" {
					return m.start("reading "+n+"…", m.inspect(n))
				}
			}
		}
		return m, nil
	case modeDetail:
		switch msg.Button {
		case tea.MouseButtonWheelUp:
			if m.itemCursor > 0 {
				m.itemCursor--
			}
		case tea.MouseButtonWheelDown:
			if n := len(m.curItems()); n > 0 && m.itemCursor < n-1 {
				m.itemCursor++
			}
		}
		return m, nil
	case modePreview, modeOutput:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) selected() string {
	r := m.tbl.SelectedRow()
	if len(r) < 2 {
		return ""
	}
	return r[1]
}

func (m model) surfaces() []profile.Surface { return m.inv.Surfaces() }

// curSurface is the surface for the active surface tab, clamped to range.
func (m model) curSurface() profile.Surface {
	ss := m.surfaces()
	if len(ss) == 0 {
		return ""
	}
	i := m.surfaceTab
	if i >= len(ss) {
		i = len(ss) - 1
	}
	return ss[i]
}

// surfaceFeatures lists the feature categories present in the current surface,
// in canonical order, so empty categories never show as tabs.
func (m model) surfaceFeatures() []string {
	s := m.curSurface()
	var out []string
	for _, f := range profile.Categories {
		if m.inv.Count(s, f) > 0 {
			out = append(out, f)
		}
	}
	return out
}

func (m model) curItems() []profile.InvItem {
	feats := m.surfaceFeatures()
	if m.tab < 0 || m.tab >= len(feats) {
		return nil
	}
	return m.inv.Items[m.curSurface()][feats[m.tab]]
}

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
	switch m.mode {
	case modeOutput:
		footer := subtleStyle.Render("  ↑/↓ scroll · q/esc back")
		return m.boxed(m.titleBar(), footer)
	}

	var b strings.Builder
	b.WriteString(m.heading("copilot-config", "Copilot customization profiles"))

	switch m.mode {
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

	b.WriteString(m.tbl.View() + "\n\n")
	b.WriteString(m.statusline() + "\n")
	b.WriteString(subtleStyle.Render("  " + m.help.View(keys)))
	return b.String()
}

// heading renders the app title with a subtitle and a full-width rule beneath.
func (m model) heading(title, subtitle string) string {
	line := titleStyle.Render("  "+title) + subtleStyle.Render("  ·  "+subtitle)
	return line + "\n" + ruleStyle.Render(strings.Repeat("─", max(1, m.width))) + "\n\n"
}

// statusline renders a full-width bar with a mode pill and the latest status.
func (m model) statusline() string {
	pill := modePillStyle.Render("PROFILES")

	mark, text := "", ""
	switch {
	case m.busy:
		text = m.status + " …"
	case m.statusErr:
		mark, text = errMarkStyle.Render("✗"), m.status
	case m.status != "":
		mark, text = okMarkStyle.Render("✓"), m.status
	case m.mgr.DBSnapshot:
		text = "db snapshot"
	}

	body := mark
	if text != "" {
		if mark != "" {
			body += " "
		}
		body += text
	}
	seg := statusBarStyle.Width(max(1, m.width-lipgloss.Width(pill))).Render(" " + body)
	return lipgloss.JoinHorizontal(lipgloss.Top, pill, seg)
}

// truncate clamps s to w display cells, adding an ellipsis when it overflows.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	return ansi.Truncate(s, w, "…")
}

func (m model) detailView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("  "+m.detailName) + "\n\n")

	surfaces := m.surfaces()
	if len(surfaces) == 0 {
		b.WriteString(subtleStyle.Render("  (empty profile — nothing customized)") + "\n")
		b.WriteString("\n" + subtleStyle.Render("  q back"))
		return b.String()
	}

	// Surface row (primary axis).
	b.WriteString("  ")
	for i, s := range surfaces {
		label := fmt.Sprintf("%s %d", surfaceLabel(s), m.inv.SurfaceTotal(s))
		style := tabStyle
		if i == m.surfaceTab {
			style = activeTabStyle
		}
		b.WriteString(style.Render(label) + " ")
	}
	b.WriteString("\n")

	// Feature row (secondary axis, scoped to the active surface).
	cur := m.curSurface()
	feats := m.surfaceFeatures()
	b.WriteString("  ")
	if len(feats) == 0 {
		b.WriteString(subtleStyle.Render("(none)"))
	} else {
		for i, f := range feats {
			label := fmt.Sprintf("%s %d", catShort[f], m.inv.Count(cur, f))
			style := tabStyle
			if i == m.tab {
				style = activeTabStyle
			}
			b.WriteString(style.Render(label) + " ")
		}
	}
	b.WriteString("\n\n")

	items := m.curItems()
	if len(items) == 0 {
		b.WriteString(subtleStyle.Render("  (none)") + "\n")
	} else {
		visible, offset := windowItems(items, m.itemCursor, max(1, m.height-11))
		for i, it := range visible {
			if offset+i == m.itemCursor {
				b.WriteString("  " + promptStyle.Render("▸ "+it.Name) + "\n")
			} else {
				b.WriteString("    " + it.Name + "\n")
			}
		}
	}

	b.WriteString("\n" + subtleStyle.Render("  tab surface · ←/→ category · ↑/↓ item · enter preview · e edit · a apply · q back"))
	return b.String()
}

func (m model) previewView() string {
	name := m.detailName
	if it, ok := m.curItem(); ok {
		name = it.Name
	}
	header := titleStyle.Render("  "+name) + subtleStyle.Render("  ·  preview")
	footer := subtleStyle.Render("  ↑/↓ scroll · q/esc back")
	return m.boxed(header, footer)
}

// titleBar is the app header shared by the list and full-screen overlays.
func (m model) titleBar() string {
	bar := titleStyle.Render("  copilot-config") + subtleStyle.Render("  ·  Copilot customization profiles")
	if m.mgr.DBSnapshot {
		bar += okStyle.Render("  [db]")
	}
	return bar
}

// boxed composes a header, a bordered viewport, and a footer into a full-height
// layout. The viewport is sized from the measured header/footer so there are no
// hardcoded vertical offsets to drift out of sync.
func (m model) boxed(header, footer string) string {
	const borderRows = 2 // boxStyle top + bottom
	m.vp.Width = max(1, m.width-4)
	m.vp.Height = max(1, m.height-lipgloss.Height(header)-lipgloss.Height(footer)-borderRows-1)
	body := m.vp.View()
	if m.previewLoading {
		body = lipgloss.Place(m.vp.Width, m.vp.Height, lipgloss.Center, lipgloss.Center,
			m.spin.View()+subtleStyle.Render(" rendering…"))
	}
	box := boxStyle.Width(max(1, m.width-2)).Render(body)
	return lipgloss.JoinVertical(lipgloss.Left, header, "", box, footer)
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
		out = append(out, table.Row{mark, p.Name, profile.FmtDate(p.Created), profile.FmtAgo(p.Modified), profile.HumanSize(p.Size)})
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
