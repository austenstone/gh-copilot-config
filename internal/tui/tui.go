package tui

import (
	"bytes"
	"cmp"
	"fmt"
	"image/color"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"
	"github.com/austenstone/gh-copilot-config/internal/profile"
	"github.com/charmbracelet/x/ansi"
	zone "github.com/lrstanley/bubblezone/v2"
)

// Run launches the interactive TUI against a manager.
func Run(m *profile.Manager) error {
	zone.NewGlobal()
	defer zone.Close()
	dark := lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	_, err := tea.NewProgram(newModel(m, dark)).Run()
	return err
}

// ---- styles -------------------------------------------------------------

// Semantic palette in GitHub's colors: a fixed GitHub blue accent (so it reads
// the same regardless of the terminal's ANSI palette) with green=success,
// red=failure, and a muted gray for secondary text, matching the gh CLI.
// lipgloss v2 dropped AdaptiveColor, so the palette is resolved once at startup
// from the detected background and held in package vars.
var (
	accent  color.Color
	dim     color.Color
	green   color.Color
	red     color.Color
	yellow  color.Color
	selFg   color.Color
	surface color.Color

	titleStyle  lipgloss.Style
	subtleStyle lipgloss.Style
	okStyle     lipgloss.Style
	errStyle    lipgloss.Style
	promptStyle lipgloss.Style
	boxStyle    lipgloss.Style
	ruleStyle   lipgloss.Style

	statusBarStyle lipgloss.Style
	modePillStyle  lipgloss.Style
	okMarkStyle    lipgloss.Style
	errMarkStyle   lipgloss.Style
	warnStyle      lipgloss.Style
	warnMarkStyle  lipgloss.Style

	tabStyle       lipgloss.Style
	activeTabStyle lipgloss.Style

	itemNameStyle      lipgloss.Style
	itemDescStyle      lipgloss.Style
	selItemNameStyle   lipgloss.Style
	selItemDescStyle   lipgloss.Style
	hoverItemNameStyle lipgloss.Style
	hoverItemDescStyle lipgloss.Style
)

// setupStyles resolves the palette and builds every style for the detected
// light/dark background. Called once at model creation.
func setupStyles(dark bool) {
	ld := lipgloss.LightDark(dark)
	accent = ld(lipgloss.Color("#0969da"), lipgloss.Color("#2f81f7")) // GitHub blue
	dim = ld(lipgloss.Color("245"), lipgloss.Color("242"))            // gh's muted gray
	green = lipgloss.Color("2")                                       // success
	red = lipgloss.Color("1")                                         // failure
	yellow = lipgloss.Color("3")                                      // drift / warning
	selFg = lipgloss.Color("15")                                      // white text on a colored bar
	surface = ld(lipgloss.Color("254"), lipgloss.Color("236"))        // bar background

	titleStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	subtleStyle = lipgloss.NewStyle().Foreground(dim)
	okStyle = lipgloss.NewStyle().Foreground(green)
	errStyle = lipgloss.NewStyle().Foreground(red)
	promptStyle = lipgloss.NewStyle().Bold(true).Foreground(accent)
	boxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent)
	ruleStyle = lipgloss.NewStyle().Foreground(dim)

	statusBarStyle = lipgloss.NewStyle().Foreground(dim).Background(surface)
	modePillStyle = lipgloss.NewStyle().Bold(true).Foreground(selFg).Background(accent).Padding(0, 1)
	okMarkStyle = lipgloss.NewStyle().Foreground(green).Background(surface)
	errMarkStyle = lipgloss.NewStyle().Foreground(red).Background(surface)
	warnStyle = lipgloss.NewStyle().Foreground(yellow)
	warnMarkStyle = lipgloss.NewStyle().Foreground(yellow).Background(surface)

	tabStyle = lipgloss.NewStyle().Foreground(dim).Padding(0, 1)
	activeTabStyle = lipgloss.NewStyle().Bold(true).Foreground(selFg).Background(accent).Padding(0, 1)

	// Two-line list rows: an accent bar marks selection, a softer accent marks hover.
	itemNameStyle = lipgloss.NewStyle().PaddingLeft(2)
	itemDescStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(dim)
	selItemNameStyle = lipgloss.NewStyle().
		Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(accent).Foreground(accent).Bold(true).PaddingLeft(1)
	selItemDescStyle = lipgloss.NewStyle().
		Border(lipgloss.ThickBorder(), false, false, false, true).
		BorderForeground(accent).Foreground(dim).PaddingLeft(1)
	hoverItemNameStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(accent)
	hoverItemDescStyle = lipgloss.NewStyle().PaddingLeft(2).Foreground(dim)
}

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
	Up, Down, Left, Right, PrevSurface, NextSurface, Inspect, Apply, On, Clean, Save, Snapshot, New, Diff, Delete, Edit, DB, History, Status, Refresh, Help, Quit key.Binding
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
	Snapshot:    key.NewBinding(key.WithKeys("S"), key.WithHelp("S", "snapshot live")),
	New:         key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	Diff:        key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diff")),
	Delete:      key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "delete")),
	Edit:        key.NewBinding(key.WithKeys("e"), key.WithHelp("e", "open in editor")),
	DB:          key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "toggle db snapshots")),
	History:     key.NewBinding(key.WithKeys("h"), key.WithHelp("h", "history")),
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
	modeHistory
	modeSnapDiff
)

// action identifies a user-initiated operation awaiting input or confirmation.
type action string

const (
	actApply   action = "apply"
	actClean   action = "clean"
	actDelete  action = "delete"
	actSave    action = "save"
	actNew     action = "new"
	actRestore action = "restore"
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
	actRestore: {
		prompt:  func(t string) string { return "Restore snapshot " + t + " to your live Copilot config?" },
		working: func(t string) string { return "restoring " + t + "…" },
		done:    func(t string) string { return "restored " + t },
		run: func(mgr *profile.Manager, t string) error {
			snaps, err := mgr.Snapshots()
			if err != nil {
				return err
			}
			for _, s := range snaps {
				if s.ID == t {
					return mgr.Restore(s)
				}
			}
			return fmt.Errorf("snapshot %s not found", t)
		},
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
	mgr      *profile.Manager
	list     list.Model
	delegate *profileDelegate
	input    textinput.Model
	vp       viewport.Model
	spin     spinner.Model

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
	dark           bool

	inv        profile.Inventory // categorized assets of the inspected profile
	detailName string            // profile shown in modeDetail
	surfaceTab int               // active surface tab
	tab        int               // active category tab within the surface
	itemCursor int               // selected item within the active tab
	lastWheel  time.Time         // throttles high-res scroll bursts to one step

	snaps         []profile.Snapshot // autosave timeline, newest-first
	snapDeltas    []profile.Delta    // per-snapshot change summary vs the prior snapshot
	histCursor    int                // selected snapshot in modeHistory
	snapDiffTitle string             // header shown in modeSnapDiff

	driftState drift  // whether the live config still matches the active profile
	driftName  string // the active profile the drift state describes
}

func newModel(mgr *profile.Manager, dark bool) model {
	setupStyles(dark)

	ti := textinput.New()
	ti.Placeholder = "profile name"
	ti.CharLimit = 64

	del := &profileDelegate{}
	l := list.New(nil, del, 0, 0)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetFilteringEnabled(true)
	l.SetShowFilter(true)
	l.SetShowHelp(true)
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{keys.Inspect, keys.Apply, keys.Save, keys.Diff, keys.Delete}
	}
	l.AdditionalFullHelpKeys = func() []key.Binding {
		return []key.Binding{
			keys.Inspect, keys.Apply, keys.On, keys.Clean, keys.Save, keys.New,
			keys.Diff, keys.Delete, keys.DB, keys.History, keys.Status, keys.Refresh,
		}
	}

	sp := spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(lipgloss.NewStyle().Foreground(accent)))

	return model{mgr: mgr, list: l, delegate: del, input: ti, vp: viewport.New(), spin: sp, dark: dark}
}

// start marks an action in flight, sets a progress message, and kicks the
// spinner so slow operations (large saves, diffs) don't look frozen.
func (m model) start(status string, cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m.busy, m.status, m.statusErr = true, status, false
	return m, tea.Batch(cmd, m.spin.Tick)
}

func (m model) Init() tea.Cmd { return tea.Batch(m.loadProfiles, m.spin.Tick) }

// ---- list item + delegate -----------------------------------------------

// profileItem adapts a profile to the list's two-line item interface.
type profileItem struct{ p profile.Profile }

func (i profileItem) Title() string { return i.p.Name }
func (i profileItem) Description() string {
	return fmt.Sprintf("created %s · modified %s · %s",
		profile.FmtDate(i.p.Created), profile.FmtAgo(i.p.Modified), profile.HumanSize(i.p.Size))
}
func (i profileItem) FilterValue() string { return i.p.Name }

// profileDelegate renders each profile as a two-line row (name + active marker,
// then created·modified·size). It styles the selected and hovered rows
// distinctly and wraps every row in a bubblezone mark so mouse hover and clicks
// can be mapped back to a profile.
type profileDelegate struct {
	hovered     string // name of the row the mouse is currently over
	activeDrift drift  // whether the active profile's live config has drifted
}

func (d *profileDelegate) Height() int                         { return 2 }
func (d *profileDelegate) Spacing() int                        { return 1 }
func (d *profileDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d *profileDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	it, ok := item.(profileItem)
	if !ok {
		return
	}
	width := max(1, m.Width())

	var nameStyle, descStyle lipgloss.Style
	switch {
	case index == m.Index():
		nameStyle, descStyle = selItemNameStyle, selItemDescStyle
	case it.p.Name == d.hovered:
		nameStyle, descStyle = hoverItemNameStyle, hoverItemDescStyle
	default:
		nameStyle, descStyle = itemNameStyle, itemDescStyle
	}

	name := truncate(it.p.Name, max(1, width-6))
	if it.p.Active {
		switch d.activeDrift {
		case driftDrifted:
			name += " " + warnStyle.Render("◐") // live has diverged from the saved profile
		case driftChecking:
			name += " " + subtleStyle.Render("●")
		default:
			name += " " + okStyle.Render("●")
		}
	}
	nameLine := nameStyle.Render(name)
	descLine := descStyle.Render(truncate(it.Description(), max(1, width-3)))

	fmt.Fprint(w, zone.Mark(it.p.Name, nameLine+"\n"+descLine))
}

func items(ps []profile.Profile) []list.Item {
	out := make([]list.Item, 0, len(ps))
	for _, p := range ps {
		out = append(out, profileItem{p})
	}
	return out
}

// ---- commands (side effects) --------------------------------------------

// drift describes whether the live config still matches the active profile.
// Only the active profile can drift, so at most one row carries this state.
type drift int

const (
	driftUnknown drift = iota
	driftChecking
	driftSynced
	driftDrifted
)

type driftMsg struct {
	name    string
	drifted bool
	err     error
}

// checkDrift compares the live config against a saved profile off the UI loop.
// Diff snapshots live and compares it to the profile, so this is the real "has
// my config diverged since I applied it" check, not an mtime guess.
func (m model) checkDrift(name string) tea.Cmd {
	mgr := *m.mgr
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				msg = driftMsg{name: name, err: fmt.Errorf("panic: %v", r)}
			}
		}()
		out, err := mgr.Diff(name)
		if err != nil {
			return driftMsg{name: name, err: err}
		}
		return driftMsg{name: name, drifted: strings.TrimSpace(out) != ""}
	}
}

func (m model) loadProfiles() (msg tea.Msg) {
	defer func() {
		if r := recover(); r != nil {
			msg = profilesMsg{err: fmt.Errorf("panic: %v", r)}
		}
	}()
	ps, err := m.mgr.Profiles("modified", true)
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

type historyMsg struct {
	snaps  []profile.Snapshot
	deltas []profile.Delta
	err    error
}

// loadHistory reads the autosave timeline and computes each snapshot's change
// summary against the snapshot taken before it.
func (m model) loadHistory() (msg tea.Msg) {
	defer func() {
		if r := recover(); r != nil {
			msg = historyMsg{err: fmt.Errorf("panic: %v", r)}
		}
	}()
	mgr := *m.mgr
	snaps, err := mgr.Snapshots()
	if err != nil {
		return historyMsg{err: err}
	}
	deltas := make([]profile.Delta, len(snaps))
	for i := range snaps {
		if i+1 < len(snaps) {
			if d, err := profile.DeltaBetween(snaps[i+1].Dir, snaps[i].Dir); err == nil {
				deltas[i] = d
			}
		}
	}
	return historyMsg{snaps: snaps, deltas: deltas}
}

type snapDiffMsg struct {
	title   string
	content string
	err     error
}

// diffSnaps renders the unified diff between a snapshot and the one before it.
func (m model) diffSnaps(newer, older profile.Snapshot, width int) tea.Cmd {
	mgr := *m.mgr
	return func() (msg tea.Msg) {
		defer func() {
			if r := recover(); r != nil {
				msg = snapDiffMsg{err: fmt.Errorf("panic: %v", r)}
			}
		}()
		out, err := mgr.DiffSnapshots(older, newer)
		if err != nil {
			return snapDiffMsg{err: err}
		}
		if out == "" {
			out = "no differences between these snapshots"
		}
		title := fmt.Sprintf("%s → %s", older.ID, newer.ID)
		return snapDiffMsg{title: title, content: render(out, "snapshot.diff", width)}
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
		m.list.SetSize(msg.Width, max(1, msg.Height-4))
		m.vp.SetWidth(max(1, msg.Width-4))
		m.vp.SetHeight(max(1, msg.Height-5)) // matches boxed() chrome
		return m, nil
	case profilesMsg:
		m.loaded = true
		if msg.err != nil {
			m.status, m.statusErr = msg.err.Error(), true
			return m, nil
		}
		cmd := m.list.SetItems(items(msg.profiles))
		if m.selectAfter != "" {
			for i, p := range msg.profiles {
				if p.Name == m.selectAfter {
					m.list.Select(i)
					break
				}
			}
			m.selectAfter = ""
		}
		// Kick a drift check for the active profile (the only one that can drift).
		if active := m.mgr.Active(); active != "" && m.mgr.Exists(active) {
			m.driftState, m.driftName = driftChecking, active
			m.delegate.activeDrift = driftChecking
			return m, tea.Batch(cmd, m.checkDrift(active))
		}
		m.driftState, m.driftName = driftUnknown, ""
		m.delegate.activeDrift = driftUnknown
		return m, cmd
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
	case driftMsg:
		if msg.name != m.mgr.Active() {
			return m, nil // stale result for a profile that's no longer active
		}
		switch {
		case msg.err != nil:
			m.driftState = driftUnknown
		case msg.drifted:
			m.driftState = driftDrifted
		default:
			m.driftState = driftSynced
		}
		m.driftName = msg.name
		m.delegate.activeDrift = m.driftState
		return m, nil
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
	case historyMsg:
		m.busy = false
		if msg.err != nil {
			m.status, m.statusErr = msg.err.Error(), true
			return m, nil
		}
		m.status, m.statusErr = "", false
		m.snaps, m.snapDeltas, m.histCursor, m.mode = msg.snaps, msg.deltas, 0, modeHistory
		return m, nil
	case snapDiffMsg:
		m.busy = false
		if msg.err != nil {
			m.status, m.statusErr = msg.err.Error(), true
			return m, nil
		}
		m.snapDiffTitle = msg.title
		m.vp.SetContent(msg.content)
		m.vp.GotoTop()
		m.mode = modeSnapDiff
		return m, nil
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
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

	case modeSnapDiff:
		switch msg.String() {
		case "q", "esc":
			m.mode = modeHistory
			return m, nil
		}
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(msg)
		return m, cmd

	case modeHistory:
		switch {
		case key.Matches(msg, keys.Quit) || msg.String() == "esc":
			m.mode = modeList
			return m, nil
		case key.Matches(msg, keys.Up):
			if m.histCursor > 0 {
				m.histCursor--
			}
			return m, nil
		case key.Matches(msg, keys.Down):
			if m.histCursor < len(m.snaps)-1 {
				m.histCursor++
			}
			return m, nil
		case key.Matches(msg, keys.Inspect):
			older := m.histCursor + 1
			if older >= len(m.snaps) {
				m.status, m.statusErr = "oldest snapshot — no earlier revision to diff", false
				return m, nil
			}
			return m.start("diffing snapshots…", m.diffSnaps(m.snaps[m.histCursor], m.snaps[older], max(1, m.width-4)))
		case key.Matches(msg, keys.Apply):
			if m.histCursor >= len(m.snaps) {
				return m, nil
			}
			m.pending, m.target, m.mode = actRestore, m.snaps[m.histCursor].ID, modeConfirm
			return m, nil
		}
		return m, nil

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

	// modeList. While the filter input is open the list owns every key (so
	// typing 'g', 'q', etc. filters instead of triggering app actions).
	if m.list.SettingFilter() {
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		return m, cmd
	}

	switch {
	case key.Matches(msg, keys.Quit):
		return m, tea.Quit
	case msg.String() == "esc":
		if m.list.FilterState() != list.Unfiltered {
			break // let the list clear an applied filter instead of quitting
		}
		return m, tea.Quit
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
	case key.Matches(msg, keys.History):
		return m.start("reading history…", m.loadHistory)
	case key.Matches(msg, keys.Save):
		return m.startInput(actSave)
	case key.Matches(msg, keys.Snapshot):
		return m.start("snapshotting live…", m.action("snapshot captured", func(mgr *profile.Manager) error { return mgr.Snapshot() }))
	case key.Matches(msg, keys.New):
		return m.startInput(actNew)
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m model) startInput(a action) (tea.Model, tea.Cmd) {
	m.pending = a
	m.input.SetValue("")
	m.input.Focus()
	m.mode = modeInput
	return m, textinput.Blink
}

// wheelStep coalesces a burst of high-resolution scroll events (macOS momentum
// scrolling on Retina displays fires many per gesture) into a single step.
const wheelStep = 80 * time.Millisecond

// handleMouse routes wheel scrolling, hover highlighting, and click-to-inspect.
// Rows are located via bubblezone marks laid down during render.
func (m model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch e := msg.(type) {
	case tea.MouseWheelMsg:
		return m.handleWheel(e)
	case tea.MouseMotionMsg:
		if m.mode == modeList {
			m.delegate.hovered = m.rowAt(e)
		}
		return m, nil
	case tea.MouseClickMsg:
		if m.mode == modeList && e.Button == tea.MouseLeft {
			if name := m.rowAt(e); name != "" {
				m.selectByName(name)
				return m.start("reading "+name+"…", m.inspect(name))
			}
		}
		return m, nil
	}
	return m, nil
}

// rowAt returns the profile name whose row contains the mouse event, or "".
func (m model) rowAt(msg tea.MouseMsg) string {
	for _, it := range m.list.VisibleItems() {
		pi, ok := it.(profileItem)
		if !ok {
			continue
		}
		if zone.Get(pi.p.Name).InBounds(msg) {
			return pi.p.Name
		}
	}
	return ""
}

// selectByName moves the list cursor to the named profile, if visible.
func (m *model) selectByName(name string) {
	for i, it := range m.list.VisibleItems() {
		if pi, ok := it.(profileItem); ok && pi.p.Name == name {
			m.list.Select(i)
			return
		}
	}
}

// handleWheel advances the list/detail one item per gesture (throttled), while
// viewport content scrolls freely so reading long files stays smooth.
func (m model) handleWheel(e tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	if m.mode == modeList || m.mode == modeDetail || m.mode == modeHistory {
		if time.Since(m.lastWheel) < wheelStep {
			return m, nil
		}
		m.lastWheel = time.Now()
	}

	switch m.mode {
	case modeList:
		switch e.Button {
		case tea.MouseWheelUp:
			m.list.CursorUp()
		case tea.MouseWheelDown:
			m.list.CursorDown()
		}
		return m, nil
	case modeDetail:
		switch e.Button {
		case tea.MouseWheelUp:
			if m.itemCursor > 0 {
				m.itemCursor--
			}
		case tea.MouseWheelDown:
			if n := len(m.curItems()); n > 0 && m.itemCursor < n-1 {
				m.itemCursor++
			}
		}
		return m, nil
	case modeHistory:
		switch e.Button {
		case tea.MouseWheelUp:
			if m.histCursor > 0 {
				m.histCursor--
			}
		case tea.MouseWheelDown:
			if m.histCursor < len(m.snaps)-1 {
				m.histCursor++
			}
		}
		return m, nil
	case modePreview, modeOutput, modeSnapDiff:
		var cmd tea.Cmd
		m.vp, cmd = m.vp.Update(e)
		return m, cmd
	}
	return m, nil
}

func (m model) selected() string {
	if it, ok := m.list.SelectedItem().(profileItem); ok {
		return it.p.Name
	}
	return ""
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

// View wraps the rendered screen in a bubblezone scan (so row marks resolve to
// coordinates) and enables the alt screen and full mouse motion, which in
// bubbletea v2 are set on the returned view rather than as program options.
func (m model) View() tea.View {
	v := tea.NewView(zone.Scan(m.render()))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion
	return v
}

func (m model) render() string {
	if !m.loaded {
		return "\n  " + m.spin.View() + subtleStyle.Render("loading profiles…") + "\n"
	}
	switch m.mode {
	case modeDetail:
		return m.detailView()
	case modePreview:
		return m.previewView()
	case modeHistory:
		return m.historyView()
	case modeSnapDiff:
		return m.snapDiffView()
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

	b.WriteString(m.list.View() + "\n")
	b.WriteString(m.statusline())
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
	case m.driftState == driftDrifted:
		mark, text = warnMarkStyle.Render("⚠"), "live drifted from "+m.driftName+" · s save · o re-apply"
	case m.driftState == driftSynced:
		mark, text = okMarkStyle.Render("✓"), "in sync with "+m.driftName
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

// historyView lists the autosave timeline newest-first, each row stamped with a
// per-category summary of what changed since the snapshot before it.
func (m model) historyView() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("  Snapshot history") + subtleStyle.Render("  ·  captured before every apply, save, and delete") + "\n\n")
	if len(m.snaps) == 0 {
		b.WriteString(subtleStyle.Render("  (no snapshots yet — one is captured automatically before each destructive change)") + "\n")
		b.WriteString("\n" + subtleStyle.Render("  q back"))
		return b.String()
	}
	start, end := windowRange(m.histCursor, len(m.snaps), max(1, m.height-7))
	for i := start; i < end; i++ {
		s := m.snaps[i]
		when := fmt.Sprintf("%s  %s", profile.FmtDate(s.Taken), profile.FmtAgo(s.Taken))
		summary := deltaSummary(m.snapDeltas[i])
		if i == len(m.snaps)-1 {
			summary = subtleStyle.Render("baseline (oldest)")
		}
		label := ""
		if l := snapLabel(s); l != "" {
			label = "  " + subtleStyle.Render(l)
		}
		if i == m.histCursor {
			b.WriteString("  " + promptStyle.Render("▸ "+when) + label + "   " + summary + "\n")
		} else {
			b.WriteString("    " + subtleStyle.Render(when) + label + "   " + summary + "\n")
		}
	}
	b.WriteString("\n" + subtleStyle.Render("  ↑/↓ snapshot · enter diff vs previous · a restore to live · q back"))
	return b.String()
}

// snapLabel renders a snapshot's provenance: what triggered it and the profile
// it involved, e.g. "overwrite → work" or "apply ← work".
func snapLabel(s profile.Snapshot) string {
	switch s.Trigger {
	case "apply", "restore":
		return s.Trigger + " ← " + s.Target
	case "overwrite", "delete":
		return s.Trigger + " → " + s.Target
	case "":
		return ""
	default:
		return strings.TrimSpace(s.Trigger + " " + s.Target)
	}
}

func (m model) snapDiffView() string {
	header := titleStyle.Render("  "+m.snapDiffTitle) + subtleStyle.Render("  ·  snapshot diff")
	footer := subtleStyle.Render("  ↑/↓ scroll · q/esc back")
	return m.boxed(header, footer)
}

// deltaSummary renders a snapshot's per-category change counts: green additions,
// accent modifications, red removals.
func deltaSummary(d profile.Delta) string {
	if !d.Changed() {
		return subtleStyle.Render("no changes")
	}
	modStyle := lipgloss.NewStyle().Foreground(accent)
	var parts []string
	for _, c := range d.Cats {
		var seg []string
		if c.Added > 0 {
			seg = append(seg, okStyle.Render(fmt.Sprintf("+%d", c.Added)))
		}
		if c.Modified > 0 {
			seg = append(seg, modStyle.Render(fmt.Sprintf("~%d", c.Modified)))
		}
		if c.Removed > 0 {
			seg = append(seg, errStyle.Render(fmt.Sprintf("-%d", c.Removed)))
		}
		parts = append(parts, catShort[c.Category]+" "+strings.Join(seg, ""))
	}
	return strings.Join(parts, subtleStyle.Render(" · "))
}

// windowRange returns the [start,end) slice of n items visible around cursor.
func windowRange(cursor, n, height int) (start, end int) {
	if height <= 0 || n <= height {
		return 0, n
	}
	start = cursor - height/2
	if start < 0 {
		start = 0
	}
	if start > n-height {
		start = n - height
	}
	return start, start + height
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
	m.vp.SetWidth(max(1, m.width-4))
	m.vp.SetHeight(max(1, m.height-lipgloss.Height(header)-lipgloss.Height(footer)-borderRows-1))
	body := m.vp.View()
	if m.previewLoading {
		body = lipgloss.Place(m.vp.Width(), m.vp.Height(), lipgloss.Center, lipgloss.Center,
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
