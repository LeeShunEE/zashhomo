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
// item opens a submenu instead and action is ignored.
//
// A non-empty disabled reason means the action is discouraged in the current
// state: the row is greyed but still selectable, and choosing it asks for
// confirmation before going ahead. Grey therefore never means inert — every
// greyed row carries an action that really runs, and a state the menu cannot act
// on at all is rendered as an info row or simply left out.
//
// An info row is not a choice: it states the context the menu acts on, is
// skipped by the cursor, and renders as a dim label followed by an unstyled
// value, matching the status card above.
type menuItem struct {
	label    string
	value    string // info rows only: the part worth reading, left unstyled
	action   string
	sub      []menuItem
	disabled string
	// confirmYes overrides the wording of the confirming option shown for a
	// greyed row; it defaults to "Run it anyway".
	confirmYes string
	info       bool
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
		b.WriteString(line("subs", subsSummary(cfg)))
	}
	// Every line() ends in a newline; keeping the last one would render an empty
	// row inside the card, pushing the bottom border away from the final field.
	return strings.TrimRight(b.String(), "\n")
}

// subscriptionName returns the display name of the subscription at index i,
// falling back to a positional name for entries the subscription didn't name.
func subscriptionName(i int, s config.Subscription) string { return s.DisplayName(i) }

// activeSubLabel names the subscription currently generating config.yaml, or
// says why there isn't one.
func activeSubLabel(cfg *config.Config) string {
	i := cfg.ActiveIndex()
	if i >= 0 {
		return subscriptionName(i, cfg.Subscriptions[i])
	}
	if len(cfg.Subscriptions) == 0 {
		return "none — no subscriptions yet"
	}
	return "none — every subscription is disabled"
}

// subsSummary is the status-card line for subscriptions: how many there are and
// which one is live.
func subsSummary(cfg *config.Config) string {
	if len(cfg.Subscriptions) == 0 {
		return "0"
	}
	return fmt.Sprintf("%d  (active: %s)", len(cfg.Subscriptions), activeSubLabel(cfg))
}

// subscriptionTags renders the state flags shown after a subscription's name.
func subscriptionTags(s config.Subscription, active bool) string {
	var tags []string
	if active {
		tags = append(tags, "active")
	}
	if !s.Enabled() {
		tags = append(tags, "disabled")
	}
	if !s.AutoUpdate() {
		tags = append(tags, "no auto-update")
	}
	if len(tags) == 0 {
		return ""
	}
	return "  [" + strings.Join(tags, ", ") + "]"
}

// menuConfig loads the config for building menus, falling back to defaults so a
// missing or unreadable file still renders a usable menu instead of nothing.
func menuConfig() *config.Config {
	cfg, err := config.Load(paths.New().Config)
	if err != nil || cfg == nil {
		return config.Default()
	}
	return cfg
}

// subscriptionsMenu builds the Subscriptions submenu. It leads with the active
// profile, since every other action here is relative to it. With nothing
// configured yet the two browsing entries are left out rather than shown as
// empty lists — the info row already says there is nothing there.
func subscriptionsMenu(cfg *config.Config) []menuItem {
	items := []menuItem{{label: "Current active", value: activeSubLabel(cfg), info: true}}
	if len(cfg.Subscriptions) > 0 {
		items = append(items,
			menuItem{label: "Switch active subscription ▸", sub: switchSubscriptionMenu(cfg)},
			menuItem{label: "List subscriptions ▸", sub: subscriptionMenu(cfg)},
		)
	}

	updateAll := menuItem{label: "Update all now", action: "sub update"}
	if len(cfg.Subscriptions) == 0 {
		updateAll.disabled = "no subscriptions yet"
	}
	return append(items,
		menuItem{label: "Add subscription…", action: "sub-add"},
		updateAll,
		menuItem{label: "Set global refresh interval…", action: "sub-interval"},
		menuItem{label: "Open config file", action: "sub edit"},
	)
}

// switchSubscriptionMenu lets the user pick the profile to run, one row per
// subscription. The active one is left out — switching to what is already
// running is not an action. A disabled one stays, greyed: switching to it is a
// real thing to want, it just has to be enabled first, which the confirmation
// offers to do.
func switchSubscriptionMenu(cfg *config.Config) []menuItem {
	active := cfg.ActiveIndex()
	items := make([]menuItem, 0, len(cfg.Subscriptions))
	for i, s := range cfg.Subscriptions {
		if i == active {
			continue
		}
		it := menuItem{label: subscriptionName(i, s), action: fmt.Sprintf("sub switch %d", i)}
		if !s.Enabled() {
			it.action = fmt.Sprintf("sub-enable-switch %d", i)
			it.disabled = "disabled — switching will enable it first"
			it.confirmYes = "Enable it and switch"
		}
		items = append(items, it)
	}
	return items
}

// subscriptionMenu lists every subscription; each row opens its detail page.
func subscriptionMenu(cfg *config.Config) []menuItem {
	active := cfg.ActiveIndex()
	items := make([]menuItem, 0, len(cfg.Subscriptions))
	for i, s := range cfg.Subscriptions {
		items = append(items, menuItem{
			label: subscriptionName(i, s) + subscriptionTags(s, i == active) + " ▸",
			sub:   subscriptionDetailMenu(cfg, i),
		})
	}
	return items
}

// subscriptionDetailMenu is one subscription's own page: its state on top, then
// everything that can be done to it. The enable/disable and scheduled-update
// rows are toggles, so each is labelled with the action it performs rather than
// the state it is in.
func subscriptionDetailMenu(cfg *config.Config, i int) []menuItem {
	s := cfg.Subscriptions[i]
	active := cfg.ActiveIndex() == i

	items := []menuItem{
		{label: "url", value: s.URL, info: true},
		{label: "state", value: detailState(s, active), info: true},
		{label: "schedule", value: scheduleSummary(cfg, s), info: true},
		{label: "updated", value: lastUpdated(s), info: true},
	}

	// "Set as active" only earns a row when it would do something. Already active
	// is stated by the state line above, and a disabled subscription has "Enable
	// this subscription" right below — that is the step to take first.
	if !active && s.Enabled() {
		items = append(items, menuItem{label: "Set as active", action: fmt.Sprintf("sub switch %d", i)})
	}
	items = append(items, menuItem{label: "Update now", action: fmt.Sprintf("sub update %d", i)})

	if s.Enabled() {
		items = append(items, menuItem{label: "Disable this subscription", action: fmt.Sprintf("sub disable %d", i)})
	} else {
		items = append(items, menuItem{label: "Enable this subscription", action: fmt.Sprintf("sub enable %d", i)})
	}

	if s.AutoUpdate() {
		items = append(items, menuItem{label: "Turn scheduled update off", action: fmt.Sprintf("sub auto %d off", i)})
	} else {
		items = append(items, menuItem{label: "Turn scheduled update on", action: fmt.Sprintf("sub auto %d on", i)})
	}

	return append(items,
		menuItem{label: "Change update interval…", action: fmt.Sprintf("sub-interval-at %d", i)},
		menuItem{label: "Delete this subscription", action: fmt.Sprintf("sub-remove %d", i)},
	)
}

// detailState summarises enabled/active on the detail page's state line.
func detailState(s config.Subscription, active bool) string {
	state := "enabled"
	if !s.Enabled() {
		state = "disabled"
	}
	if active {
		state += ", active"
	}
	return state
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
	subs := menuItem{label: "Subscriptions ▸", sub: subscriptionsMenu(menuConfig())}
	sysProxy := menuItem{label: "System proxy ▸", sub: []menuItem{
		{label: "Enable system proxy", action: "system-proxy enable"},
		{label: "Disable system proxy", action: "system-proxy disable"},
	}}
	uninstall := menuItem{label: "Uninstall", action: "uninstall"}
	// The guided setup applies in every state — it walks the same five steps and
	// marks the ones already satisfied — so it is never greyed out.
	guide := menuItem{label: "Guided setup", action: "onboard"}
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
		return []menuItem{install, dashboard, subs, sysProxy, update, start, stop, restart, uninstall, guide, version, help, exit}
	case !st.Running:
		return []menuItem{start, restart, stop, dashboard, subs, sysProxy, update, install, uninstall, guide, version, help, exit}
	default:
		return []menuItem{dashboard, stop, restart, start, subs, sysProxy, update, install, uninstall, guide, version, help, exit}
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
	root := rootMenu(st)
	return menuModel{
		header:  header,
		stack:   [][]menuItem{root},
		titles:  []string{"zashhomo"},
		cursors: []int{firstSelectable(root)},
	}
}

// firstSelectable returns the index the cursor should start on: info rows are
// labels rather than choices, so it skips them.
func firstSelectable(items []menuItem) int {
	for i, it := range items {
		if !it.info {
			return i
		}
	}
	return 0
}

// moveCursor steps from by delta, wrapping around the list and passing over info
// rows. It returns from unchanged when there is nothing selectable to land on.
func moveCursor(items []menuItem, from, delta int) int {
	n := len(items)
	if n == 0 {
		return from
	}
	for step := 1; step <= n; step++ {
		i := ((from+delta*step)%n + n) % n
		if !items[i].info {
			return i
		}
	}
	return from
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
		*cur = moveCursor(items, *cur, -1)
	case "down", "j":
		*cur = moveCursor(items, *cur, 1)
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
		if it.info {
			// The cursor never rests on an info row, but a list of nothing but
			// info rows would leave it there; don't act on a label.
			return m, nil
		}
		if len(it.sub) > 0 {
			m.stack = append(m.stack, it.sub)
			m.titles = append(m.titles, strings.TrimSuffix(it.label, " ▸"))
			m.cursors = append(m.cursors, firstSelectable(it.sub))
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

	// Menu items list. Every row — info, plain, greyed — is indented by its
	// two-character prefix and nothing else, so the styles must not carry padding
	// of their own or the columns drift apart.
	cur := *m.cursor()
	keyWidth := infoKeyWidth(m.current())
	for i, it := range m.current() {
		// Info rows state the context the menu acts on. The label is dim and the
		// value is left unstyled, the same split the status card above uses, so
		// the part worth reading is the bright one. They sit at the same depth
		// as unselected options, marking them as context rather than choices.
		if it.info {
			b.WriteString("    " + theme.InfoKey.Render(fmt.Sprintf("%-*s", keyWidth, it.label)) + it.value)
			b.WriteByte('\n')
			continue
		}

		label := it.label
		if it.disabled != "" {
			label += "  (" + it.disabled + ")"
		}

		// Unselected rows sink into a 4-space gutter, grouping them as the list.
		// The selected row shrinks to 2 characters (arrow + space), pulling the
		// text left to stand out. The cursor stays visually anchored — only the
		// depth changes, not the column of the arrow itself.
		prefix := "    "
		if i == cur {
			prefix = "❯ "
		}

		// Colour carries one meaning at a time: highlighted for the cursor, dim for
		// discouraged, plain otherwise. A greyed row under the cursor is drawn like
		// any other selection — the warning is the confirmation it opens, which
		// states the reason in full and defaults to Cancel.
		var rendered string
		switch {
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

	// Bottom hint area, aligned to the same column as the rows above it.
	b.WriteByte('\n')
	b.WriteString("  " + theme.Hint.Render("↑/↓ move · enter select · esc back · q quit"))
	b.WriteByte('\n')

	return b.String()
}

// infoKeyWidth is the column width that lines up the values of a menu's info
// rows. Two spaces of gutter keep the key and value from touching.
func infoKeyWidth(items []menuItem) int {
	width := 0
	for _, it := range items {
		if it.info && len(it.label) > width {
			width = len(it.label)
		}
	}
	if width == 0 {
		return 0
	}
	return width + 2
}

// confirmModel is a two-option prompt. For a destructive action (danger set) it
// is the second gate: it names the target, spells out that the change cannot be
// undone, paints the confirming option red, and leaves the cursor on "Cancel" so
// the Enter that opened the screen can never delete anything by itself. Benign
// prompts clear danger and start the cursor on the confirming option.
type confirmModel struct {
	title    string   // what is about to happen, e.g. "Remove subscription"
	details  []string // the target's identifying lines (name, URL, …)
	warning  string   // why this is irreversible; shown only when set
	noLabel  string   // label of the declining option; defaults to "Cancel"
	yesLabel string   // label of the confirming option
	danger   bool     // the confirming option destroys something
	cursor   int      // 0 = decline, 1 = confirm
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
	titleStyle := theme.Title
	if m.danger {
		titleStyle = theme.Danger
	}

	var card strings.Builder
	card.WriteString(titleStyle.Render(m.title))
	card.WriteByte('\n')
	for _, d := range m.details {
		card.WriteString("\n  " + theme.OutputValue.Render(d))
	}
	if m.warning != "" {
		warnStyle := theme.StatusWarn
		if m.danger {
			warnStyle = theme.Danger
		}
		card.WriteString("\n\n" + warnStyle.Render("⚠ "+m.warning))
	}

	var b strings.Builder
	b.WriteString(theme.Card.Render(card.String()))
	b.WriteString("\n\n")

	noLabel := m.noLabel
	if noLabel == "" {
		noLabel = "Cancel"
	}
	for i, opt := range []string{noLabel, m.yesLabel} {
		prefix := "  "
		if i == m.cursor {
			prefix = "❯ "
		}
		switch {
		case i == m.cursor && i == 1 && m.danger:
			b.WriteString(theme.Danger.Render(prefix + opt))
		case i == m.cursor:
			b.WriteString(theme.Selected.Render(prefix + opt))
		default:
			b.WriteString(theme.MenuItem.Render(prefix + opt))
		}
		b.WriteByte('\n')
	}

	b.WriteByte('\n')
	b.WriteString("  " + theme.Hint.Render("↑/↓ move · enter confirm · esc cancel"))
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
