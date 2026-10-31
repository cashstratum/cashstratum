package ui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Views lists the names RenderFrame accepts.
var Views = []string{"dashboard", "workers", "blocks", "feed", "network", "address", "drilldown", "celebration", "help"}

// celebrationFrames is how many animation ticks pass before the celebration
// frame is captured, enough for the second burst well spread.
const celebrationFrames = 11

// ThemeNamed returns the theme called name: "cashgreen" or "mono".
func ThemeNamed(name string) (Theme, error) {
	switch name {
	case "", "cashgreen":
		return Cashgreen(), nil
	case "mono":
		return Mono(), nil
	}
	return Theme{}, fmt.Errorf("unknown theme %q (want cashgreen or mono)", name)
}

// RenderFrame drives a model to the named view and returns that single
// screen as w×h lines of text with SGR colour codes only: no alternate
// screen, no cursor movement. The clock is opts.Now, so a fixed clock gives a
// fixed frame.
func RenderFrame(p Provider, opts Options, view string, w, h int) (string, error) {
	if p == nil {
		return "", fmt.Errorf("no data source")
	}
	if w <= 0 || h <= 0 {
		return "", fmt.Errorf("invalid size %dx%d", w, h)
	}
	if opts.Now == nil {
		now := time.Now()
		opts.Now = func() time.Time { return now }
	}
	opts.PollInterval = 0
	if view == "celebration" {
		opts.Motion = true
	}
	m := New(p, opts)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})

	switch view {
	case "dashboard":
	case "workers":
		m.tab = TabWorkers
	case "blocks":
		m.tab = TabBlocks
	case "feed":
		m.tab = TabFeed
	case "network":
		m.tab = TabNetwork
	case "help":
		m.help = true
	case "address":
		addr := topAddress(m.snap.Workers)
		if addr == "" {
			return "", fmt.Errorf("view address: the data source has no workers")
		}
		m.tab = TabWorkers
		m.page = addressPage(addr)
	case "drilldown":
		if len(m.snap.Blocks) == 0 {
			return "", fmt.Errorf("view drilldown: the data source has no blocks")
		}
		m.tab = TabBlocks
		settle(m, m.openDrill(m.snap.Blocks[0].Height))
	case "celebration":
		m.Update(BlockFoundMsg{Block: ReplayBlock(m.snap, opts.Now()), Replay: true})
		for i := 0; i < celebrationFrames; i++ {
			m.Update(tickMsg(opts.Now()))
		}
	default:
		return "", fmt.Errorf("unknown view %q (want one of %s)", view, strings.Join(Views, ", "))
	}
	return m.Render(), nil
}

// settle runs a command whose messages complete immediately, feeding them
// back into the model.
func settle(m *Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			settle(m, c)
		}
	default:
		_, next := m.Update(msg)
		settle(m, next)
	}
}

// topAddress is the payout address with the highest combined hashrate.
func topAddress(ws []WorkerRow) string {
	rate := map[string]float64{}
	for _, w := range ws {
		if w.Address != "" {
			rate[w.Address] += w.Hashrate1m
		}
	}
	addrs := make([]string, 0, len(rate))
	for a := range rate {
		addrs = append(addrs, a)
	}
	sort.Slice(addrs, func(i, j int) bool {
		if rate[addrs[i]] != rate[addrs[j]] {
			return rate[addrs[i]] > rate[addrs[j]]
		}
		return addrs[i] < addrs[j]
	})
	if len(addrs) == 0 {
		return ""
	}
	return addrs[0]
}
