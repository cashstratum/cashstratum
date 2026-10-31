package ui

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m *Model) searchKey(k tea.KeyPressMsg) tea.Cmd {
	switch k.String() {
	case "esc":
		m.searching, m.command = false, false
		return nil
	case "enter":
		m.searching = false
		if m.command {
			m.command = false
			return m.runCommand(strings.TrimSpace(m.query))
		}
		return m.resolve(strings.TrimSpace(m.query))
	case "backspace":
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
		}
		return nil
	}
	if k.Text != "" && len(m.query) < 128 {
		m.query += k.Text
	}
	return nil
}

// runCommand executes a ":" command.
func (m *Model) runCommand(c string) tea.Cmd {
	switch strings.ToLower(c) {
	case "":
		return nil
	case "q", "quit":
		return tea.Quit
	case "test-celebrate":
		return ReplayCmd(m.opt.Caps, m.snap, m.opt.Now())
	}
	m.status = "unknown command :" + c + " (try :test-celebrate or :q)"
	return nil
}

// resolve routes a search: digits are a height, CashAddr-looking input an
// address, anything else a worker name.
func (m *Model) resolve(q string) tea.Cmd {
	if q == "" {
		return nil
	}
	if h, err := strconv.Atoi(q); err == nil && h >= 0 {
		m.tab = TabBlocks
		for i, b := range m.snap.Blocks {
			if b.Height == h {
				m.sel[TabBlocks] = i
			}
		}
		return m.openDrill(h)
	}
	lq := strings.ToLower(q)
	if isAddress(lq) {
		addr := q
		for _, w := range m.snap.Workers {
			if strings.Contains(strings.ToLower(w.Address), lq) {
				addr = w.Address
				break
			}
		}
		m.page = addressPage(addr)
		return nil
	}
	ws := sortedWorkers(m.snap.Workers, m.opt.Now())
	for i, w := range ws {
		if strings.Contains(strings.ToLower(w.Name), lq) {
			m.page = nil
			m.tab = TabWorkers
			m.sel[TabWorkers] = i
			return nil
		}
	}
	m.status = "no height, address or worker matches " + strconv.Quote(q)
	return nil
}

func isAddress(s string) bool {
	if strings.Contains(s, ":") {
		return true
	}
	return len(s) >= 25 && (s[0] == 'q' || s[0] == 'p') && !strings.Contains(s, ".")
}

// addressSummary is the built-in address view: the address's workers, its
// blocks and its best share.
type addressSummary struct{ addr string }

func (a *addressSummary) Title() string { return "address" }

func (a *addressSummary) Update(tea.Msg, *Context) (Page, tea.Cmd) { return a, nil }

func (a *addressSummary) Render(ctx *Context, w, h int) []string {
	t := ctx.Theme
	s := ctx.Snap
	var ws []WorkerRow
	var rate, best float64
	for _, wr := range s.Workers {
		if wr.Address == a.addr {
			ws = append(ws, wr)
			rate += wr.Hashrate1m
			best = max(best, wr.BestShare)
		}
	}
	var found int
	bare := a.addr
	if i := strings.IndexByte(bare, ':'); i >= 0 {
		bare = bare[i+1:]
	}
	for _, b := range s.Blocks {
		if strings.Contains(b.Finder, bare) {
			found++
		}
	}
	out := []string{
		" " + t.Title.Render("Address") + " " + t.Text.Render(a.addr), "",
		" " + kv(ctx, "workers", 14, itoa(len(ws))),
		" " + kv(ctx, "hashrate 1m", 14, fmtHash(rate)),
		" " + kv(ctx, "best share", 14, fmtDiff(best)),
		" " + kv(ctx, "blocks found", 14, itoa(found)),
		"",
	}
	keep := fitColumns(workerCols, w-1)
	out = append(out, " "+tableHead(ctx, workerCols, keep))
	for _, r := range sortedWorkers(ws, ctx.Now) {
		badge, tag := rigBadge(ctx, r)
		last := dash
		if !r.LastShare.IsZero() {
			last = fmtDur(ctx.Now.Sub(r.LastShare))
		}
		out = append(out, " "+tableRow(workerCols, keep, []string{" ", badge + " " + tag, r.Name,
			shortAddr(r.Address, ctx.sym.Ellipsis), agentShort(r.UserAgent), ctx.hash(r.Hashrate1m),
			ctx.hash(r.Hashrate1h), ctx.hash(r.Hashrate1d), fmtDiff(r.Diff), fmtDiff(r.BestShare), last,
			stateLabel(ctx, ctx.classify(r))}))
	}
	return out
}
