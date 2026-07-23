package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// menuItem is one selectable row. action is the command line (minus the
// "zashhomo" prefix) run when the item is chosen; when sub is non-empty the
// item opens a submenu instead and action is ignored.
type menuItem struct {
	label  string
	action string
	sub    []menuItem
}

var (
	menuTitleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13"))
	menuSelectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("14"))
	menuHintStyle     = lipgloss.NewStyle().Faint(true)
)

// rootMenu is the top-level management menu. Items whose commands need free-form
// input (a subscription URL) use a sentinel action handled by cmdInteractive.
func rootMenu() []menuItem {
	return []menuItem{
		{label: "Status", action: "status"},
		{label: "Start service", action: "service start"},
		{label: "Stop service", action: "service stop"},
		{label: "Restart service", action: "service restart"},
		{label: "Update ▸", sub: []menuItem{
			{label: "Kernel (--core)", action: "update --core"},
			{label: "Panel (--ui)", action: "update --ui"},
			{label: "Self (--self)", action: "update --self"},
			{label: "Everything (--all)", action: "update --all"},
		}},
		{label: "Subscriptions ▸", sub: []menuItem{
			{label: "Add subscription…", action: "sub-add"},
			{label: "Update & reload", action: "sub update"},
		}},
		{label: "Install (defaults)", action: "install"},
		{label: "Uninstall", action: "uninstall"},
		{label: "Version", action: "version"},
		{label: "Help", action: "help"},
		{label: "Exit", action: "exit"},
	}
}

// menuModel drives an arrow-key menu with nested submenus. Selecting a leaf sets
// choice and quits; Esc/Backspace pops a submenu or, at the root, exits.
type menuModel struct {
	stack   [][]menuItem
	titles  []string
	cursors []int
	choice  string
}

func newMenuModel() menuModel {
	return menuModel{
		stack:   [][]menuItem{rootMenu()},
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
		m.choice = "exit"
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
			m.choice = "exit"
			return m, tea.Quit
		}
	case "enter", "right", "l":
		it := items[*cur]
		if len(it.sub) > 0 {
			m.stack = append(m.stack, it.sub)
			m.titles = append(m.titles, strings.TrimSuffix(it.label, " ▸"))
			m.cursors = append(m.cursors, 0)
		} else {
			m.choice = it.action
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
		if i == cur {
			b.WriteString(menuSelectedStyle.Render("❯ " + it.label))
		} else {
			b.WriteString("  " + it.label)
		}
		b.WriteByte('\n')
	}
	b.WriteByte('\n')
	b.WriteString(menuHintStyle.Render("↑/↓ move · enter select · esc back · q quit"))
	b.WriteByte('\n')
	return b.String()
}

// runMenu shows the menu on the alternate screen and returns the chosen action
// (or "exit"). The alt screen keeps the menu from cluttering scrollback, so
// command output printed afterwards reads as a clean log.
func runMenu() (string, error) {
	p := tea.NewProgram(newMenuModel(), tea.WithAltScreen())
	res, err := p.Run()
	if err != nil {
		return "", err
	}
	final, ok := res.(menuModel)
	if !ok {
		return "exit", nil
	}
	if final.choice == "" {
		return "exit", nil
	}
	return final.choice, nil
}

// menuBanner is printed once when entering the interactive console.
func menuBanner() string {
	return fmt.Sprintf("zashhomo %s — interactive console", version)
}
