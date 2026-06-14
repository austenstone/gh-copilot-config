package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type mode int

const (
	modeList mode = iota
	modeOutput
	modeInput
	modeConfirm
)

type profile struct {
	name     string
	created  time.Time
	modified time.Time
	sizeKB   int64
	active   bool
	last     bool
}

type profilesMsg struct {
	profiles []profile
	err      string
}

type actionDoneMsg struct {
	title string
	out   string
	err   bool
}

var (
	accent   = lipgloss.Color("212")
	dim      = lipgloss.Color("241")
	good     = lipgloss.Color("42")
	bad      = lipgloss.Color("203")
	titleSt  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	subtleSt = lipgloss.NewStyle().Foreground(dim)
	okSt     = lipgloss.NewStyle().Foreground(good)
	errSt    = lipgloss.NewStyle().Foreground(bad)
	helpSt   = lipgloss.NewStyle().Foreground(dim)
	borderSt = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent)
	promptSt = lipgloss.NewStyle().Bold(true).Foreground(accent)
)

type keymap struct {
	up, down, apply, on, clean, save, newp, diff, del, status, refresh, help, quit key.Binding
}

var keys = keymap{
	up:      key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	down:    key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	apply:   key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "apply")),
	on:      key.NewBinding(key.WithKeys("o"), key.WithHelp("o", "re-apply last")),
	clean:   key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "clean")),
	save:    key.NewBinding(key.WithKeys("s"), key.WithHelp("s", "save")),
	newp:    key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "new")),
	diff:    key.NewBinding(key.WithKeys("d"), key.WithHelp("d", "diff")),
	del:     key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "delete")),
	status:  key.NewBinding(key.WithKeys("g"), key.WithHelp("g", "status")),
	refresh: key.NewBinding(key.WithKeys("r"), key.WithHelp("r", "refresh")),
	help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
}

type model struct {
	engineDir string
	tbl       table.Model
	profiles  []profile
	mode      mode
	vp        viewport.Model
	input     textinput.Model
	pending   string // action awaiting input/confirm
	pendName  string // profile the pending action targets
	status    string
	statusErr bool
	showHelp  bool
	width     int
	height    int
	loaded    bool
}

func runTUI(engineDir string) error {
	_, err := tea.NewProgram(newModel(engineDir), tea.WithAltScreen()).Run()
	return err
}

func newModel(engineDir string) model {
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

	return model{engineDir: engineDir, tbl: t, input: ti, vp: viewport.New(0, 0)}
}

func (m model) Init() tea.Cmd {
	return m.loadProfiles()
}

func (m model) loadProfiles() tea.Cmd {
	engineDir := m.engineDir
	return func() tea.Msg {
		out, err := runEngineCapture(engineDir, "list", "--porcelain")
		if err != nil {
			return profilesMsg{err: strings.TrimSpace(out)}
		}
		var ps []profile
		for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
			if line == "" {
				continue
			}
			f := strings.Split(line, "\t")
			if len(f) < 6 {
				continue
			}
			ps = append(ps, profile{
				name:     f[0],
				created:  epoch(f[1]),
				modified: epoch(f[2]),
				sizeKB:   atoi(f[3]),
				active:   f[4] == "1",
				last:     f[5] == "1",
			})
		}
		return profilesMsg{profiles: ps}
	}
}

func (m model) runAction(title string, args ...string) tea.Cmd {
	engineDir := m.engineDir
	return func() tea.Msg {
		out, err := runEngineCapture(engineDir, args...)
		return actionDoneMsg{title: title, out: strings.TrimSpace(out), err: err != nil}
	}
}

func runEngineCapture(engineDir string, args ...string) (string, error) {
	cmd := exec.Command("bash", append([]string{enginePath(engineDir)}, args...)...)
	cmd.Env = append(os.Environ(), "CC_NO_TUI=1")
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.String(), err
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.tbl.SetHeight(max(5, msg.Height-9))
		m.vp.Width, m.vp.Height = msg.Width-2, max(5, msg.Height-7)
		return m, nil

	case profilesMsg:
		m.loaded = true
		if msg.err != "" {
			m.status, m.statusErr = msg.err, true
			return m, nil
		}
		m.profiles = msg.profiles
		m.tbl.SetRows(profileRows(msg.profiles))
		return m, nil

	case actionDoneMsg:
		m.status, m.statusErr = msg.title, msg.err
		if strings.TrimSpace(msg.out) != "" {
			m.mode = modeOutput
			m.vp.SetContent(msg.out)
			m.vp.GotoTop()
		}
		return m, m.loadProfiles()

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
				return m, m.runAction("saved '"+name+"'", "save", name, "-y")
			case "new":
				return m, m.runAction("created '"+name+"'", "new", name)
			}
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd

	case modeConfirm:
		switch msg.String() {
		case "y", "Y":
			m.mode = modeList
			return m, m.dispatchConfirmed()
		default:
			m.mode = modeList
			m.status, m.statusErr = "cancelled", false
			return m, nil
		}
	}

	// modeList
	switch {
	case key.Matches(msg, keys.quit):
		return m, tea.Quit
	case key.Matches(msg, keys.help):
		m.showHelp = !m.showHelp
		return m, nil
	case key.Matches(msg, keys.refresh):
		m.status = ""
		return m, m.loadProfiles()
	case key.Matches(msg, keys.apply):
		if n := m.selected(); n != "" {
			m.pending, m.pendName = "apply", n
			m.mode = modeConfirm
		}
		return m, nil
	case key.Matches(msg, keys.clean):
		m.pending, m.pendName = "clean", "clean"
		m.mode = modeConfirm
		return m, nil
	case key.Matches(msg, keys.del):
		if n := m.selected(); n != "" && n != "clean" {
			m.pending, m.pendName = "delete", n
			m.mode = modeConfirm
		}
		return m, nil
	case key.Matches(msg, keys.on):
		return m, m.runAction("re-applied last profile", "on")
	case key.Matches(msg, keys.status):
		return m, m.runAction("status", "status")
	case key.Matches(msg, keys.diff):
		if n := m.selected(); n != "" {
			return m, m.runAction("diff "+n, "diff", n)
		}
		return m, nil
	case key.Matches(msg, keys.save):
		m.pending = "save"
		m.input.SetValue("")
		m.input.Focus()
		m.mode = modeInput
		return m, textinput.Blink
	case key.Matches(msg, keys.newp):
		m.pending = "new"
		m.input.SetValue("")
		m.input.Focus()
		m.mode = modeInput
		return m, textinput.Blink
	}

	var cmd tea.Cmd
	m.tbl, cmd = m.tbl.Update(msg)
	return m, cmd
}

func (m model) dispatchConfirmed() tea.Cmd {
	switch m.pending {
	case "apply":
		return m.runAction("applied '"+m.pendName+"'", "apply", m.pendName)
	case "clean":
		return m.runAction("applied 'clean'", "clean")
	case "delete":
		return m.runAction("deleted '"+m.pendName+"'", "rm", m.pendName, "-y")
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

func (m model) View() string {
	if !m.loaded {
		return "\n  loading profiles…\n"
	}

	var b strings.Builder
	b.WriteString(titleSt.Render("  gh copilot-config") + subtleSt.Render("  ·  Copilot customization profiles") + "\n\n")

	switch m.mode {
	case modeOutput:
		b.WriteString(borderSt.Width(m.width - 2).Render(m.vp.View()))
		b.WriteString("\n" + helpSt.Render("  ↑/↓ scroll · q/esc back"))
		return b.String()

	case modeInput:
		label := "Save current live config as:"
		if m.pending == "new" {
			label = "New empty profile name:"
		}
		b.WriteString("  " + promptSt.Render(label) + "\n\n  " + m.input.View() + "\n\n")
		b.WriteString(helpSt.Render("  enter confirm · esc cancel"))
		return b.String()

	case modeConfirm:
		q := m.confirmQuestion()
		b.WriteString("  " + promptSt.Render(q) + "\n\n")
		b.WriteString(helpSt.Render("  y confirm · any other key cancel"))
		return b.String()
	}

	b.WriteString(m.tbl.View() + "\n")
	if m.status != "" {
		st := okSt
		if m.statusErr {
			st = errSt
		}
		b.WriteString("  " + st.Render("• "+m.status) + "\n")
	} else {
		b.WriteString("\n")
	}
	b.WriteString(m.helpView())
	return b.String()
}

func (m model) confirmQuestion() string {
	switch m.pending {
	case "apply":
		return "Apply profile '" + m.pendName + "' to your live Copilot config?"
	case "clean":
		return "Reset live Copilot config to vanilla (apply 'clean')?"
	case "delete":
		return "Permanently delete profile '" + m.pendName + "'?"
	}
	return "Continue?"
}

func (m model) helpView() string {
	if m.showHelp {
		lines := []string{
			"  enter apply   o re-apply last   c clean   s save   n new",
			"  d diff        x delete          g status  r refresh",
			"  ↑/↓ move      ? hide help        q quit",
		}
		return helpSt.Render(strings.Join(lines, "\n"))
	}
	return helpSt.Render("  enter apply · s save · n new · d diff · x delete · ? help · q quit")
}

func profileRows(ps []profile) []table.Row {
	rows := make([]table.Row, 0, len(ps))
	for _, p := range ps {
		mark := " "
		if p.active {
			mark = "●"
		}
		rows = append(rows, table.Row{mark, p.name, fmtDate(p.created), fmtDate(p.modified), humanSize(p.sizeKB)})
	}
	return rows
}

func epoch(s string) time.Time {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}
	}
	return time.Unix(n, 0)
}

func atoi(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

func fmtDate(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	return t.Format("2006-01-02")
}

func humanSize(kb int64) string {
	switch {
	case kb < 1024:
		return strconv.FormatInt(kb, 10) + "K"
	case kb < 1024*1024:
		return fmt.Sprintf("%.1fM", float64(kb)/1024)
	default:
		return fmt.Sprintf("%.1fG", float64(kb)/(1024*1024))
	}
}
