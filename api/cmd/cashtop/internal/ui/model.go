// Package ui is the cashtop terminal interface: five tabs over a Snapshot
// supplied by a Provider.
package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"cashstratumapi/cmd/cashtop/internal/termcap"
)

// Tab identifies one of the numbered tabs.
type Tab int

const (
	TabDashboard Tab = iota
	TabWorkers
	TabBlocks
	TabFeed
	TabNetwork
	numTabs
)

var tabNames = [numTabs]string{"Dashboard", "Workers", "Blocks", "Feed", "Network"}

func (t Tab) String() string { return tabNames[t] }

const (
	// MinWidth and MinHeight are the smallest usable terminal size.
	MinWidth  = 80
	MinHeight = 24

	tickEvery       = 250 * time.Millisecond
	defaultDebounce = 75 * time.Millisecond
	maxFeedRows     = 2000
)

// Options configure a Model.
type Options struct {
	Caps  termcap.Caps
	Theme *Theme           // nil picks ThemeFor(Caps)
	Now   func() time.Time // nil uses time.Now
	// Motion enables animation; it is forced off under ASCII glyphs.
	Motion bool
	// PollInterval is how often Provider.Snapshot is called; zero or less
	// disables polling (data then arrives only as messages).
	PollInterval time.Duration
	// ResizeDebounce delays applying a resize; zero uses 75 ms.
	ResizeDebounce time.Duration
}

// Context is what views and seams render from.
type Context struct {
	Snap     *Snapshot
	Now      time.Time
	Frame    int
	Caps     termcap.Caps
	Theme    Theme
	Motion   bool
	Provider Provider
	// NetFlash highlights the network line right after a new network block.
	NetFlash bool
	sym      symbols
}

// Model is the bubbletea model.
type Model struct {
	prov  Provider
	opt   Options
	theme Theme
	sym   symbols

	snap Snapshot

	width, height int
	pendW, pendH  int
	resizeSeq     int

	tab   Tab
	frame int
	help  bool

	searching bool
	command   bool // the prompt is a ":" command rather than a search
	query     string
	status    string

	sel        [numTabs]int
	feedOffset int // rows scrolled up from the newest; 0 follows
	rain       bool

	page     Page
	overlays []Overlay

	netBlock NetworkBlockMsg
	netSeen  time.Time // when netBlock arrived; zero before the first
}

type tickMsg time.Time
type resizeMsg struct{ seq int }
type pollMsg struct{}

// New builds a Model and takes the first snapshot synchronously so the first
// frame already has data.
func New(p Provider, o Options) *Model {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.ResizeDebounce <= 0 {
		o.ResizeDebounce = defaultDebounce
	}
	if o.Caps.Glyphs == termcap.ASCII {
		o.Motion = false
	}
	m := &Model{prov: p, opt: o, sym: symbolsFor(o.Caps)}
	if o.Theme != nil {
		m.theme = *o.Theme
	} else {
		m.theme = ThemeFor(o.Caps)
	}
	if p != nil {
		m.snap = p.Snapshot()
	}
	ctx := m.ctx()
	for _, f := range overlayFactories {
		if ov := f(ctx); ov != nil {
			m.overlays = append(m.overlays, ov)
		}
	}
	return m
}

func (m *Model) ctx() *Context {
	return &Context{
		Snap: &m.snap, Now: m.opt.Now(), Frame: m.frame, Caps: m.opt.Caps, Theme: m.theme,
		Motion: m.opt.Motion, Provider: m.prov, sym: m.sym,
		NetFlash: m.opt.Motion && m.netActive(m.opt.Now(), NetBlockFlashFor),
	}
}

// Tab returns the active tab.
func (m *Model) Tab() Tab { return m.tab }

// Init starts the animation tick and the data poll.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(tick(), m.pollAfter())
}

func tick() tea.Cmd {
	return tea.Tick(tickEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *Model) pollAfter() tea.Cmd {
	if m.opt.PollInterval <= 0 || m.prov == nil {
		return nil
	}
	return tea.Tick(m.opt.PollInterval, func(time.Time) tea.Msg { return pollMsg{} })
}

func (m *Model) poll() tea.Cmd {
	p := m.prov
	return func() tea.Msg { return SnapshotMsg{Snapshot: p.Snapshot()} }
}

// Update handles one message.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd
	ctx := m.ctx()

	// Overlays see everything first; an active one swallows keys.
	fwd := msg
	if t, ok := msg.(tickMsg); ok {
		fwd = TickMsg{Time: time.Time(t), Frame: m.frame + 1}
	}
	takeover := false
	for _, ov := range m.overlays {
		cmds = append(cmds, ov.Update(fwd, ctx))
		takeover = takeover || ov.Active()
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		if m.width == 0 {
			m.width, m.height = msg.Width, msg.Height
			break
		}
		m.resizeSeq++
		m.pendW, m.pendH = msg.Width, msg.Height
		seq := m.resizeSeq
		cmds = append(cmds, tea.Tick(m.opt.ResizeDebounce, func(time.Time) tea.Msg { return resizeMsg{seq} }))
	case resizeMsg:
		if msg.seq == m.resizeSeq {
			m.width, m.height = m.pendW, m.pendH
		}
	case tickMsg:
		m.frame++
		cmds = append(cmds, tick())
	case pollMsg:
		cmds = append(cmds, m.poll())
	case SnapshotMsg:
		feed := m.snap.Feed
		m.snap = msg.Snapshot
		if len(m.snap.Feed) == 0 {
			m.snap.Feed = feed
		}
		cmds = append(cmds, m.pollAfter())
	case FeedRowsMsg:
		m.snap.Feed = append(m.snap.Feed, msg.Rows...)
		if n := len(m.snap.Feed); n > maxFeedRows {
			m.snap.Feed = append([]FeedRow(nil), m.snap.Feed[n-maxFeedRows:]...)
		}
		if m.feedOffset > 0 {
			m.feedOffset += len(msg.Rows)
		}
	case BlockFoundMsg:
		if !msg.Replay && (len(m.snap.Blocks) == 0 || m.snap.Blocks[0].Height != msg.Block.Height) {
			m.snap.Blocks = append([]BlockRow{msg.Block}, m.snap.Blocks...)
			m.snap.Pool.LastBlock = msg.Block.Time
		}
	case NetworkBlockMsg:
		if !takeover {
			m.netBlock, m.netSeen = msg, m.opt.Now()
		}
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if takeover {
			return m, tea.Batch(cmds...)
		}
		cmds = append(cmds, m.key(msg))
		return m, tea.Batch(cmds...)
	}

	if m.page != nil {
		var c tea.Cmd
		m.page, c = m.page.Update(msg, m.ctx())
		cmds = append(cmds, c)
	}
	return m, tea.Batch(cmds...)
}

// netActive reports whether a network block arrived less than d ago.
func (m *Model) netActive(now time.Time, d time.Duration) bool {
	return !m.netSeen.IsZero() && now.Sub(m.netSeen) < d
}

func (m *Model) key(k tea.KeyPressMsg) tea.Cmd {
	s := k.String()
	if m.searching {
		return m.searchKey(k)
	}
	if m.help {
		switch s {
		case "?", "esc", "q", "enter":
			m.help = false
		}
		return nil
	}
	m.status = ""
	switch s {
	case "q":
		return tea.Quit
	case "?":
		m.help = true
		return nil
	case "/":
		m.searching, m.command, m.query = true, false, ""
		return nil
	case ":":
		m.searching, m.command, m.query = true, true, ""
		return nil
	}
	if m.page != nil {
		if s == "esc" || s == "backspace" {
			m.page = nil
			return nil
		}
		var c tea.Cmd
		m.page, c = m.page.Update(k, m.ctx())
		return c
	}
	switch s {
	case "1", "2", "3", "4", "5":
		m.tab = Tab(s[0] - '1')
	case "tab", "right", "l":
		m.tab = (m.tab + 1) % numTabs
	case "shift+tab", "left", "h":
		m.tab = (m.tab + numTabs - 1) % numTabs
	case "down", "j":
		m.move(1)
	case "up", "k":
		m.move(-1)
	case "pgdown":
		m.move(10)
	case "pgup":
		m.move(-10)
	case "home", "g":
		m.move(-1 << 30)
	case "end", "G":
		m.move(1 << 30)
	case "r":
		if m.tab == TabFeed {
			if shareRain == nil {
				m.status = "share rain is not available in this build"
			} else {
				m.rain = !m.rain
			}
		}
	case "enter":
		return m.enter()
	}
	return nil
}

func (m *Model) move(d int) {
	if m.tab == TabFeed {
		m.feedOffset -= d
		if m.feedOffset < 0 {
			m.feedOffset = 0
		}
		if mx := len(m.snap.Feed) - 1; m.feedOffset > mx {
			m.feedOffset = max(mx, 0)
		}
		return
	}
	n := m.rows()
	i := m.sel[m.tab] + d
	if i >= n {
		i = n - 1
	}
	if i < 0 {
		i = 0
	}
	m.sel[m.tab] = i
}

func (m *Model) rows() int {
	switch m.tab {
	case TabWorkers:
		return len(m.snap.Workers)
	case TabBlocks:
		return len(m.snap.Blocks)
	}
	return 0
}

func (m *Model) enter() tea.Cmd {
	switch m.tab {
	case TabBlocks:
		if i := m.sel[TabBlocks]; i < len(m.snap.Blocks) {
			return m.openDrill(m.snap.Blocks[i].Height)
		}
	case TabWorkers:
		ws := sortedWorkers(m.snap.Workers, m.opt.Now())
		if i := m.sel[TabWorkers]; i < len(ws) && ws[i].Address != "" {
			m.page = addressPage(ws[i].Address)
		}
	}
	return nil
}

func (m *Model) openDrill(height int) tea.Cmd {
	m.page = &drillPage{height: height}
	p := m.prov
	if p == nil {
		return nil
	}
	return func() tea.Msg {
		d, err := p.DrillDown(height)
		return DrillDownMsg{Height: height, Data: d, Err: err}
	}
}

// View renders the whole screen.
func (m *Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	if !m.opt.Caps.NoColor {
		v.BackgroundColor = m.theme.Background
	}
	return v
}

// Render returns the screen content, for tests and snapshots.
func (m *Model) Render() string { return m.render() }

func (m *Model) render() string {
	w, h := m.width, m.height
	if w == 0 || h == 0 {
		return ""
	}
	if w < MinWidth || h < MinHeight {
		return tooSmall(m.theme, w, h)
	}
	ctx := m.ctx()
	for _, ov := range m.overlays {
		if ov.Active() {
			return ov.Render(ctx, w, h)
		}
	}
	bodyH := h - 2
	banner := ctx.stale()
	if banner {
		bodyH--
	}
	netBanner := m.netActive(ctx.Now, NetBlockBannerFor)
	if netBanner {
		bodyH--
	}
	var body []string
	switch {
	case m.help:
		body = renderHelp(ctx, w, bodyH)
	case m.page != nil:
		body = m.page.Render(ctx, w, bodyH)
	default:
		body = m.renderTab(ctx, w, bodyH)
	}
	lines := make([]string, 0, h)
	lines = append(lines, m.header(ctx, w))
	if banner {
		lines = append(lines, staleBanner(ctx, w))
	}
	if netBanner {
		lines = append(lines, netBlockBanner(ctx, m.netBlock, w))
	}
	for i := 0; i < bodyH; i++ {
		l := ""
		if i < len(body) {
			l = body[i]
		}
		lines = append(lines, fit(l, w))
	}
	lines = append(lines, m.footer(ctx, w))
	return strings.Join(lines, "\n")
}

func (m *Model) renderTab(ctx *Context, w, h int) []string {
	switch m.tab {
	case TabWorkers:
		return renderWorkers(ctx, w, h, m.sel[TabWorkers])
	case TabBlocks:
		return renderBlocks(ctx, w, h, m.sel[TabBlocks])
	case TabFeed:
		if m.rain && shareRain != nil {
			return shareRain(ctx, w, h)
		}
		return renderFeed(ctx, w, h, m.feedOffset)
	case TabNetwork:
		return renderNetwork(ctx, w, h)
	}
	return renderDashboard(ctx, w, h)
}

func (m *Model) header(ctx *Context, w int) string {
	t := m.theme
	var b strings.Builder
	b.WriteString(t.Title.Render(" cashtop "))
	for i := Tab(0); i < numTabs; i++ {
		label := " " + string(rune('1'+i)) + " " + tabNames[i] + " "
		if i == m.tab && m.page == nil && !m.help {
			b.WriteString(t.TabActive.Render(label))
		} else {
			b.WriteString(t.TabIdle.Render(label))
		}
	}
	if m.page != nil {
		b.WriteString(t.Dim.Render(" " + m.sym.Cursor + " "))
		b.WriteString(t.TabActive.Render(" " + m.page.Title() + " "))
	}
	beat := m.sym.Beat[0]
	if m.opt.Motion {
		beat = m.sym.Beat[(m.frame/2)%len(m.sym.Beat)]
	}
	clock := ctx.Now.UTC().Format("15:04:05") + " UTC "
	rhs := t.Accent.Render(beat) + " " + t.Text.Render(clock)
	rw := 2 + len(clock)
	return fit(b.String(), w-rw) + right(rhs, rw)
}

func (m *Model) footer(ctx *Context, w int) string {
	t := m.theme
	if m.searching && m.command {
		return fit(t.Accent.Render(" : ")+t.Text.Render(m.query)+t.Accent.Render("_")+
			t.Dim.Render("   test-celebrate "+m.sym.Sep+" q "+m.sym.Sep+" esc cancel"), w)
	}
	if m.searching {
		return fit(t.Accent.Render(" / ")+t.Text.Render(m.query)+t.Accent.Render("_")+
			t.Dim.Render("   height, address or worker "+m.sym.Sep+" enter go "+m.sym.Sep+" esc cancel"), w)
	}
	if m.status != "" {
		return fit(" "+t.Warn.Render(m.status), w)
	}
	sep := " " + m.sym.Sep + " "
	keys := " 1-5 tabs" + sep + "/ search" + sep + "enter open" + sep + "? help" + sep + "q quit"
	if m.page != nil {
		keys = " esc back" + sep + "j/k scroll" + sep + "? help" + sep + "q quit"
	} else if m.tab == TabFeed {
		keys += sep + "j/k scroll" + sep + "G follow"
	}
	mode := m.theme.Name
	if m.opt.Caps.Glyphs == termcap.ASCII {
		mode += " ascii"
	}
	return fit(t.Dim.Render(keys), w-len(mode)-1) + t.Dim.Render(mode+" ")
}

func tooSmall(t Theme, w, h int) string {
	msg := []string{
		"terminal too small",
		itoa(w) + "x" + itoa(h) + ", need " + itoa(MinWidth) + "x" + itoa(MinHeight),
		"resize or press q",
	}
	lines := make([]string, h)
	top := (h - len(msg)) / 2
	for i, s := range msg {
		if top+i < 0 || top+i >= h {
			continue
		}
		pad := (w - len(s)) / 2
		if pad < 0 {
			pad = 0
		}
		lines[top+i] = fit(strings.Repeat(" ", pad)+t.Warn.Render(s), w)
	}
	for i := range lines {
		if lines[i] == "" {
			lines[i] = strings.Repeat(" ", max(w, 0))
		}
	}
	return strings.Join(lines, "\n")
}
