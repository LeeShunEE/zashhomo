package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/LeeShunEE/zashhomo/internal/config"
	"github.com/LeeShunEE/zashhomo/internal/paths"
	"github.com/LeeShunEE/zashhomo/internal/svc"
)

// bigBanner is the ASCII-art title shown at the top of the interactive menu.
const bigBanner = ` _____   _    ____  _   _ _   _  ___  __  __  ___
|__  /  / \  / ___|| | | | | | |/ _ \|  \/  |/ _ \
  / /  / _ \ \___ \| |_| | |_| | | | | |\/| | | | |
 / /_ / ___ \ ___) |  _  |  _  | |_| | |  | | |_| |
/____/_/   \_\____/|_| |_|_| |_|\___/|_|  |_|\___/`

func init() {
	// Initialize theme based on terminal background
	theme = AdaptiveTheme()
}

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

// menuHeader renders the big banner plus a compact status block for st. It is
// shown at the top of the menu on every redraw, so the state (including "not
// installed") is always visible. Config-derived fields fall back to defaults
// when nothing has been installed yet, so the block renders in every state.
func menuHeader(st svc.State) string {
	var b strings.Builder
	b.WriteString(theme.Banner.Render(bigBanner))
	b.WriteString("\n\n")

	dot, word, style := "○", "not installed", theme.StatusWarn
	switch {
	case !st.Installed:
		// keep the not-installed defaults
	case st.Running:
		dot, word, style = "●", "running", theme.StatusOk
	default:
		dot, word, style = "○", "stopped", theme.StatusWarn
	}

	line := func(label, val string) string {
		return theme.Label.Render(fmt.Sprintf("%-8s", label)) + val + "\n"
	}
	b.WriteString(line("service", style.Render(dot+" "+word)))
	b.WriteString(line("version", version))

	cfg, _ := config.Load(paths.New().Config)
	if cfg != nil {
		b.WriteString(line("proxy", systemProxyStatus(cfg)))
		b.WriteString(line("mixed", mixedProxyURL(cfg)))
		b.WriteString(line("tun", tunStatus(cfg)))
		b.WriteString(line("panel", panelURL(cfg)))
		b.WriteString(line("kernel", orDash(cfg.CoreVersion)))
		b.WriteString(line("panelv", orDash(cfg.UIVersion)))
		b.WriteString(line("subs", fmt.Sprintf("%d", len(cfg.Subscriptions))))
	}
	// Every line() ends in a newline; keeping the last one would render an empty
	// row inside the card, pushing the bottom border away from the final field.
	return strings.TrimRight(b.String(), "\n")
}

// subscriptionName returns the display name of the subscription at index i,
// falling back to a positional name for entries the subscription didn't name.
func subscriptionName(i int, s config.Subscription) string {
	if s.Name != "" {
		return s.Name
	}
	return fmt.Sprintf("sub-%d", i)
}

// subscriptionMenu builds a dynamic submenu listing all subscriptions.
func subscriptionMenu() []menuItem {
	cfg, _ := config.Load(paths.New().Config)
	if cfg == nil || len(cfg.Subscriptions) == 0 {
		return []menuItem{
			{label: "No subscriptions configured", action: "noop", disabled: "use 'Add subscription' to add one"},
		}
	}

	items := make([]menuItem, 0, len(cfg.Subscriptions))
	for i, sub := range cfg.Subscriptions {
		items = append(items, menuItem{
			label:  subscriptionName(i, sub),
			action: fmt.Sprintf("sub show %d", i),
		})
	}
	return items
}

// removeSubscriptionMenu builds the submenu for deleting a subscription: one row
// per entry, each carrying its index in the action so the user picks the victim
// with the arrow keys instead of typing a number. Choosing a row only opens the
// confirmation — the delete itself happens in removeSubscriptionAt.
func removeSubscriptionMenu() []menuItem {
	cfg, _ := config.Load(paths.New().Config)
	if cfg == nil || len(cfg.Subscriptions) == 0 {
		return []menuItem{
			{label: "No subscriptions configured", action: "noop", disabled: "nothing to remove"},
		}
	}

	items := make([]menuItem, 0, len(cfg.Subscriptions))
	for i, sub := range cfg.Subscriptions {
		items = append(items, menuItem{
			label:  subscriptionName(i, sub),
			action: fmt.Sprintf("sub-remove %d", i),
		})
	}
	return items
}

// rootMenu builds the top-level management menu, ordered and annotated for the
// current service state: the most useful next action leads, and actions that
// don't apply yet are greyed with a reason. Items whose commands need free-form
// input (a subscription URL) use a sentinel action handled by cmdInteractive.
func rootMenu(st svc.State) []menuItem {
	install := menuItem{label: "Install (defaults)", action: "install"}
	start := menuItem{label: "Start service", action: "service start"}
	stop := menuItem{label: "Stop service", action: "service stop"}
	restart := menuItem{label: "Restart service", action: "service restart"}
	dashboard := menuItem{label: "Open dashboard", action: "dashboard"}
	update := menuItem{label: "Software Update ▸", sub: []menuItem{
		{label: "Kernel (--core)", action: "update --core"},
		{label: "Panel (--ui)", action: "update --ui"},
		{label: "Self (--self)", action: "update --self"},
		{label: "Everything (--all)", action: "update --all"},
	}}
	subs := menuItem{label: "Subscriptions ▸", sub: []menuItem{
		{label: "List subscriptions ▸", sub: subscriptionMenu()},
		{label: "Add subscription…", action: "sub-add"},
		{label: "Remove subscription ▸", sub: removeSubscriptionMenu()},
		{label: "Update & reload", action: "sub update"},
		{label: "Set refresh interval…", action: "sub-interval"},
		{label: "Open config file", action: "sub edit"},
	}}
	sysProxy := menuItem{label: "System proxy ▸", sub: []menuItem{
		{label: "Enable system proxy", action: "system-proxy enable"},
		{label: "Disable system proxy", action: "system-proxy disable"},
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
		// The panel is served by the daemon, so there is nothing to open yet.
		dashboard.disabled = "not installed yet"
	case st.Running:
		start.disabled = "service already running"
	default:
		stop.disabled = "service is not running"
		dashboard.disabled = "service is not running"
	}

	// Order so the obvious next step leads.
	switch {
	case !st.Installed:
		return []menuItem{install, dashboard, subs, sysProxy, update, start, stop, restart, uninstall, version, help, exit}
	case !st.Running:
		return []menuItem{start, restart, stop, dashboard, subs, sysProxy, update, install, uninstall, version, help, exit}
	default:
		return []menuItem{dashboard, stop, restart, start, subs, sysProxy, update, install, uninstall, version, help, exit}
	}
}

// menuModel drives an arrow-key menu with nested submenus. Selecting a leaf sets
// choice and quits; Esc/Backspace pops a submenu or, at the root, exits.
type menuModel struct {
	header  string
	stack   [][]menuItem
	titles  []string
	cursors []int
	choice  menuItem
}

func newMenuModel(st svc.State, header string) menuModel {
	return menuModel{
		header:  header,
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

	// Header area with card border (bigBanner + status only). The card style's
	// bottom margin already supplies the single blank line separating it from
	// what follows, so only that margin line is terminated here — writing more
	// would stack extra blank rows above the first menu item.
	if m.header != "" {
		b.WriteString(theme.Card.Render(m.header))
		b.WriteByte('\n')
	}

	// Breadcrumb below the card — the root "zashhomo" is omitted since the
	// banner (hero badge) inside the card already shows it. It sits directly on
	// top of the items it labels.
	if crumbs := m.titles[1:]; len(crumbs) > 0 {
		b.WriteString(theme.Title.Render(strings.Join(crumbs, " ▸ ")))
		b.WriteByte('\n')
	}

	// Menu items list
	cur := *m.cursor()
	for i, it := range m.current() {
		label := it.label
		if it.disabled != "" {
			label += "  (" + it.disabled + ")"
		}

		// Selection indicator
		prefix := "  "
		if i == cur {
			prefix = "❯ "
		}

		// Apply styles
		var rendered string
		switch {
		case i == cur && it.disabled != "":
			rendered = theme.Disabled.Faint(true).Render(prefix + label)
		case i == cur:
			rendered = theme.Selected.Render(prefix + label)
		case it.disabled != "":
			rendered = theme.Disabled.Render(prefix + label)
		default:
			rendered = theme.MenuItem.Render(prefix + label)
		}
		b.WriteString(rendered)
		b.WriteByte('\n')
	}

	// Bottom hint area
	b.WriteByte('\n')
	b.WriteString(theme.Hint.Render("↑/↓ move · enter select · esc back · q quit"))
	b.WriteByte('\n')

	return b.String()
}

// confirmModel is the second gate in front of a destructive action: it names the
// target, spells out that the change cannot be undone, and makes the user move
// the cursor onto the destructive option. The cursor starts on "Cancel" so the
// Enter that opened this screen can never delete anything by itself.
type confirmModel struct {
	title    string   // what is about to happen, e.g. "Remove subscription"
	details  []string // the target's identifying lines (name, URL, …)
	warning  string   // why this is irreversible
	yesLabel string   // label of the destructive option
	cursor   int      // 0 = cancel, 1 = confirm
	answer   bool
}

func (m confirmModel) Init() tea.Cmd { return nil }

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	switch key.String() {
	case "ctrl+c", "esc", "q":
		return m, tea.Quit
	case "up", "down", "left", "right", "k", "j", "h", "l", "tab":
		m.cursor = 1 - m.cursor
	case "enter":
		m.answer = m.cursor == 1
		return m, tea.Quit
	}
	return m, nil
}

func (m confirmModel) View() string {
	var card strings.Builder
	card.WriteString(theme.Danger.Render(m.title))
	card.WriteByte('\n')
	for _, d := range m.details {
		card.WriteString("\n  " + theme.OutputValue.Render(d))
	}
	if m.warning != "" {
		card.WriteString("\n\n" + theme.Danger.Render("⚠ "+m.warning))
	}

	var b strings.Builder
	b.WriteString(theme.Card.Render(card.String()))
	b.WriteString("\n\n")

	options := []string{"Cancel", m.yesLabel}
	for i, opt := range options {
		prefix := "  "
		if i == m.cursor {
			prefix = "❯ "
		}
		switch {
		case i == m.cursor && i == 1:
			b.WriteString(theme.Danger.Render(prefix + opt))
		case i == m.cursor:
			b.WriteString(theme.Selected.Render(prefix + opt))
		default:
			b.WriteString(theme.MenuItem.Render(prefix + opt))
		}
		b.WriteByte('\n')
	}

	b.WriteByte('\n')
	b.WriteString(theme.Hint.Render("↑/↓ move · enter confirm · esc cancel"))
	b.WriteByte('\n')
	return b.String()
}

// runConfirm shows m on the alternate screen and reports whether the user picked
// the destructive option. Any other exit (Esc, Ctrl-C, a broken TTY) is a "no".
func runConfirm(m confirmModel) (bool, error) {
	res, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		return false, err
	}
	final, ok := res.(confirmModel)
	return ok && final.answer, nil
}

// runMenu shows the menu (built for st) on the alternate screen and returns the
// chosen leaf item, or an item with action "exit". The alt screen keeps the menu
// from cluttering scrollback, so command output printed afterwards reads as a
// clean log.
func runMenu(st svc.State, header string) (menuItem, error) {
	p := tea.NewProgram(newMenuModel(st, header), tea.WithAltScreen())
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
