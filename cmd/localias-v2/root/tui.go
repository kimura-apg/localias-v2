package root

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Turborepo/Wrangler-style TUI for `localias dev`: the wrapped command's
// output scrolls in a viewport, a fixed status bar always shows the
// alias->port mapping, and keybindings are displayed inline.

// ptyBytesMsg carries a raw chunk of the wrapped process's output; the
// model feeds it to its terminal-emulation buffer so multi-line progress
// redraws (CR / line-erase / cursor-up across embedded newlines) update
// in place exactly like a real terminal.
type ptyBytesMsg []byte

type statusMsg struct {
	port   int
	detail string // e.g. "ready: upstream answered 200 OK"
}

type childExitMsg struct{ err error }

type noteMsg string

type clearNoteMsg struct{}

const maxLogLines = 2000 //nolint:gochecknoglobals

var (
	tuiStyleStatus = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)
	tuiStyleKey     = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	tuiStyleAlias   = lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
	tuiStylePort    = lipgloss.NewStyle().Foreground(lipgloss.Color("86"))
	tuiStyleWaiting = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	tuiStyleReady   = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
)

type devModel struct {
	alias     string
	cmdArgs   []string
	port      int
	ready     string
	logs      *viewport.Model
	vt        *termBuffer
	width     int
	height    int
	follow    bool
	childGone bool
	quitReq   *chan struct{}
	quitOnce  *sync.Once
	toast     string
}

func newDevModel(alias string, cmdArgs []string, quitReq *chan struct{}) devModel {
	vp := viewport.New(80, 20)
	vp.SetContent("")
	return devModel{alias: alias, cmdArgs: cmdArgs, logs: &vp, vt: newTermBuffer(),
		follow: true, quitReq: quitReq, quitOnce: &sync.Once{}}
}

func (m devModel) url() string {
	if strings.Contains(m.alias, "://") {
		return m.alias
	}
	return fmt.Sprintf("http://%s", m.alias)
}

func (m devModel) Init() tea.Cmd {
	return nil
}

func (m devModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.logs.Width = msg.Width
		m.logs.Height = maxInt(3, msg.Height-4) // log pane + gap + bordered status + keys
		return m, nil
	case ptyBytesMsg:
		m.vt.Write(msg)
		m.logs.SetContent(m.vt.String())
		if m.follow {
			m.logs.GotoBottom()
		}
		return m, nil
	case statusMsg:
		m.port = msg.port
		m.ready = msg.detail
		return m, nil
	case childExitMsg:
		m.childGone = true
		return m, tea.Quit
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			if m.quitOnce != nil {
				m.quitOnce.Do(func() { close(*m.quitReq) })
			}
			return m, tea.Quit
		case "o":
			if m.port != 0 {
				openBrowser(m.url())
			}
			return m, nil
		case "c":
			copyToClipboard(m.url())
			return m, nil
		case "up", "k":
			m.logs.LineUp(1)
			m.follow = false
		case "down", "j":
			m.logs.LineDown(1)
		case "pgup", "b":
			m.logs.HalfViewUp()
			m.follow = false
		case "pgdown", " ":
			m.logs.HalfViewDown()
		case "g", "home":
			m.logs.GotoTop()
			m.follow = false
		case "G", "end":
			m.logs.GotoBottom()
			m.follow = true
		}
		if m.follow {
			m.logs.GotoBottom()
		}
		return m, nil
	case noteMsg:
		m.toast = string(msg)
		return m, tea.Tick(4*time.Second, func(time.Time) tea.Msg { return clearNoteMsg{} })
	case clearNoteMsg:
		m.toast = ""
		return m, nil
	}
	return m, nil
}

func (m devModel) View() string {
	var mapping string
	if m.port != 0 {
		mapping = fmt.Sprintf("%s %s %s",
			tuiStyleAlias.Render(m.alias),
			tuiStyleKey.Render("→"),
			tuiStylePort.Render(fmt.Sprintf("127.0.0.1:%d", m.port)),
		)
	} else {
		mapping = tuiStyleWaiting.Render(fmt.Sprintf("%s · waiting for a listening port…", m.alias))
	}
	readyPart := ""
	if m.ready != "" {
		readyPart = "  " + tuiStyleReady.Render(m.ready)
	}
	keys := tuiStyleKey.Render("o open · c copy url · ↑↓ scroll · q quit")
	if m.toast != "" {
		keys = tuiStyleReady.Render(m.toast) + "  ·  " + keys
	}
	bar := tuiStyleStatus.Render(mapping+readyPart) + "\n" + keys
	return m.logs.View() + "\n\n" + bar
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
