package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/LeeShunEE/zashhomo/internal/svc"
)

// menuItem is one selectable row. action is the command line (minus the
// "zashhomo" prefix) run when the item is chosen; when sub is non-empty the
// item opens a submenu instead and action is ignored. A non-empty disabled
// reason means the action doesn't apply in the current state: the row is greyed
// but still selectable, and choosing it prompts for confirmation before forcing.
type menuItem struct {
	label    string
	action   string
	sub      []menuItem
	disabled string
}

var (
	menuTitleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
	menuSelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	menuDisabledStyle = lipgloss.NewStyle().Faint(true)
	menuHintStyle     = lipgloss.NewStyle().Faint(true)
)

// rootMenu builds the top-level management menu, ordered and annotated for the
// current service state: the most useful next action leads, and actions that
// don't apply yet are greyed with a reason. Items whose commands need free-form
// input (a subscription URL) use a sentinel action handled by cmdInteractive.
func rootMenu(st svc.State) []menuItem {
	status := menuItem{label: "Status", action: "status"}
	install := menuItem{label: "Install (defaults)", action: "install"}
	start := menuItem{label: "Start service", action: "service start"}
	stop := menuItem{label: "Stop service", action: "service stop"}
	restart := menuItem{label: "Restart service", action: "service restart"}
	update := menuItem{label: "Update ▸", sub: []menuItem{
		{label: "Kernel (--core)", action: "update --core"},
		{label: "Panel (--ui)", action: "update --ui"},
		{label: "Self (--self)", action: "update --self"},
		{label: "Everything (--all)", action: "update --all"},
	}}
	subs := menuItem{label: "Subscriptions ▸", sub: []menuItem{
		{label: "List subscriptions", action: "sub list"},
		{label: "Add subscription…", action: "sub-add"},
		{label: "Update & reload", action: "sub update"},
		{label: "Set refresh interval…", action: "sub-interval"},
		{label: "Open config file", action: "sub edit"},
	}}
	uninstall := menuItem{label: "Uninstall", action: "uninstall"}
	version := menuItem{label: "Version", action: "version"}
	help := menuItem{label: "Help", action: "help"}
	exit := menuItem{label: "Exit", action: "exit"}

	// Grey out actions that are meaningless in the current state.
	switch {
	case !st.Installed:
		start.disabled = "not installed yet"
		stop.disabled = "not installed yet"
		restart.disabled = "not installed yet"
		uninstall.disabled = "not installed yet"
	case st.Running:
		start.disabled = "service already running"
	default:
		stop.disabled = "service is not running"
	}

	// Order so the obvious next step leads.
	switch {
	case !st.Installed:
		return []menuItem{install, status, subs, update, start, stop, restart, uninstall, version, help, exit}
	case !st.Running:
		return []menuItem{start, status, restart, stop, subs, update, install, uninstall, version, help, exit}
	default:
		return []menuItem{status, stop, restart, start, subs, update, install, uninstall, version, help, exit}
	}
}

// menuModel drives an arrow-key menu with nested submenus. Selecting a leaf sets
// choice and quits; Esc/Backspace pops a submenu or, at the root, exits.
type menuModel struct {
	stack   [][]menuItem
	titles  []string
	cursors []int
	choice  menuItem
}

func newMenuModel(st svc.State) menuModel {
	return menuModel{
		stack:   [][]menuItem{rootMenu(st)},
		titles:  []string{"zashhomo"},
		cursors: []int{0},
	}
}

func (m menuModel) current() []menuItem { return m.stack[len(m.stack)-1] }
func (m *menuModel) cursor() *int       { return &m.cursors[len(m.cursors)-1] }

func (m menuModel) Init() tea.Cmd { return nil }

func (m menuModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	items := m.current()
	cur := m.cursor()
	switch key.String() {
	case "ctrl+c", "q":
		m.choice = menuItem{action: "exit"}
		return m, tea.Quit
	case "up", "k":
		if *cur > 0 {
			*cur--
		} else {
			*cur = len(items) - 1
		}
	case "down", "j":
		if *cur < len(items)-1 {
			*cur++
		} else {
			*cur = 0
		}
	case "esc", "backspace", "left", "h":
		if len(m.stack) > 1 {
			m.stack = m.stack[:len(m.stack)-1]
			m.titles = m.titles[:len(m.titles)-1]
			m.cursors = m.cursors[:len(m.cursors)-1]
		} else {
			m.choice = menuItem{action: "exit"}
			return m, tea.Quit
		}
	case "enter", "right", "l":
		it := items[*cur]
		if len(it.sub) > 0 {
			m.stack = append(m.stack, it.sub)
			m.titles = append(m.titles, strings.TrimSuffix(it.label, " ▸"))
			m.cursors = append(m.cursors, 0)
		} else {
			m.choice = it
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m menuModel) View() string {
	var b strings.Builder
	b.WriteString(menuTitleStyle.Render(strings.Join(m.titles, " ▸ ")))
	b.WriteString("\n\n")
	cur := *m.cursor()
	for i, it := range m.current() {
		label := it.label
		if it.disabled != "" {
			label += "  (" + it.disabled + ")"
		}
		prefix := "  "
		if i == cur {
			prefix = "❯ "
		}
		var style lipgloss.Style
		switch {
		case i == cur && it.disabled != "":
			style = menuSelectedStyle.Faint(true)
		case i == cur:
			style = menuSelectedStyle
		case it.disabled != "":
			style = menuDisabledStyle
		default:
			style = lipgloss.NewStyle()
		}
		b.WriteString(style.Render(prefix + label))
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteString(menuHintStyle.Render("↑/↓ move · enter select · esc back · q quit"))
	b.WriteByte('\n')
	return b.String()
}

// runMenu shows the menu (built for st) on the alternate screen and returns the
// chosen leaf item, or an item with action "exit". The alt screen keeps the menu
// from cluttering scrollback, so command output printed afterwards reads as a
// clean log.
func runMenu(st svc.State) (menuItem, error) {
	p := tea.NewProgram(newMenuModel(st), tea.WithAltScreen())
	res, err := p.Run()
	if err != nil {
		return menuItem{}, err
	}
	final, ok := res.(menuModel)
	if !ok || final.choice.action == "" {
		return menuItem{action: "exit"}, nil
	}
	return final.choice, nil
}

// menuBanner is printed once when entering the interactive console.
func menuBanner() string {
	return fmt.Sprintf("zashhomo %s — interactive console", version)
}
