// Package app is the live data source: it reads a pool install read-only
// (log, users, pool status, sharelogs, solve records), asks the node over
// read-only RPC and ZMQ, and feeds the terminal UI.
package app

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"cashstratumapi/cmd/cashtop/internal/celebrate"
	"cashstratumapi/cmd/cashtop/internal/events"
	"cashstratumapi/cmd/cashtop/internal/feed"
	"cashstratumapi/cmd/cashtop/internal/index"
	"cashstratumapi/cmd/cashtop/internal/rpc"
	"cashstratumapi/cmd/cashtop/internal/tail"
	"cashstratumapi/cmd/cashtop/internal/termcap"
	"cashstratumapi/cmd/cashtop/internal/ui"
	"cashstratumapi/internal/poolfmt"
	"cashstratumapi/internal/zmqsub"
)

// Config configures an App.
type Config struct {
	Root string // pool install directory
	Conf string // pool configuration file; empty disables RPC and ZMQ
	Caps termcap.Caps

	CacheDir string // index cache override
	Memory   bool   // keep the index in memory only

	Poll        time.Duration // log poll interval; default 500 ms
	StatsEvery  time.Duration // users / pool status / sharelog refresh; default 5 s
	RPCEvery    time.Duration // node refresh; default 15 s
	OrphanEvery int           // network blocks between orphan re-checks; default 6
	Backlog     int64         // bytes of log history shown in the feed at start; default 64 KiB

	NoNode bool // never contact the node (no RPC, no ZMQ)
	Now    func() time.Time

	// ProcNet is the directory holding the kernel TCP tables; empty means
	// /proc/net on Linux and no listener check elsewhere.
	ProcNet string
	Log     *log.Logger // diagnostics; nil discards. Never stdout.
}

const maxFeed = 2000

// App is a live ui.Provider.
type App struct {
	cfg        Config
	bestWarned map[string]bool
	logPath    string
	ix         *index.Index
	tl         *tail.Tailer
	fe         *feed.Engine
	join       poolfmt.Joiner
	ev         *events.Engine

	node     *rpc.Client
	enricher events.Node // reward and hash lookups; the node client unless replaced
	zmqEP    string
	zmqSub   atomic.Pointer[zmqsub.Subscriber]
	kick     chan struct{}
	netBlks  atomic.Int64

	closed  sync.Once
	mu      sync.Mutex
	feed    []ui.FeedRow
	stats   fileStats
	net     ui.NetworkInfo
	logNet  logNet
	nb      netTrack
	lastLog time.Time // newest timestamp seen on a parsed log line
	ports   []int     // stratum listen ports from the pool configuration
	rewards map[int64]int64
	retries map[int64]retryState // failed reward lookups by height
	hint    string
}

// logNet is what the log itself says about the network and template.
type logNet struct {
	diff        float64
	zmq         time.Time
	tmpl        time.Time
	tmplTxs     int
	tipHash     string
	tipHashTime time.Time
}

// Open opens the index, seeds the block-find dedupe set and positions the
// log tailer at the index cursor, so nothing already in the log is ever
// announced as new.
func Open(cfg Config) (*App, error) {
	if cfg.Poll <= 0 {
		cfg.Poll = 500 * time.Millisecond
	}
	if cfg.StatsEvery <= 0 {
		cfg.StatsEvery = 5 * time.Second
	}
	if cfg.RPCEvery <= 0 {
		cfg.RPCEvery = 15 * time.Second
	}
	if cfg.OrphanEvery <= 0 {
		cfg.OrphanEvery = 6
	}
	if cfg.Backlog <= 0 {
		cfg.Backlog = 64 << 10
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = log.New(io.Discard, "", 0)
	}
	a := &App{cfg: cfg, logPath: filepath.Join(cfg.Root, "logs", "cashstratum.log"),
		kick: make(chan struct{}, 1), rewards: map[int64]int64{}, retries: map[int64]retryState{}}

	ix, err := index.Open(index.Options{Root: cfg.Root, CacheDir: cfg.CacheDir, Memory: cfg.Memory,
		Tail: tail.Options{Poll: cfg.Poll}})
	if err != nil {
		return nil, err
	}
	a.ix = ix
	v := ix.Snapshot()
	start := v.Cursor.Offset

	seen := events.Heights(nil)
	for _, b := range v.Blocks {
		seen = append(seen, b.Height)
	}
	sources := []events.SeenSource{seen}
	if f, err := os.Open(filepath.Join(cfg.Root, "solved-heights.idx")); err == nil {
		sources = append(sources, events.SolvedHeightsSource(f))
		f.Close()
	}
	if f, err := os.Open(filepath.Join(cfg.Root, "solves", "INDEX.tsv")); err == nil {
		sources = append(sources, events.SolvesIndexSource(f))
		f.Close()
	}
	// The cursor is where the next new line starts; the engine treats events
	// at or before its start offset as history, so the boundary is one byte
	// earlier or a solve on the very first new line would be swallowed.
	a.ev = events.New(start-1, sources...)
	a.fe = feed.NewEngine(feed.Config{})
	a.backlog(start)

	cur := v.Cursor
	var from *tail.Cursor
	if cur.Inode != 0 {
		from = &cur
	}
	tl, err := tail.New(a.logPath, from, tail.Options{Poll: cfg.Poll})
	if err != nil {
		ix.Close()
		return nil, err
	}
	a.tl = tl

	a.ports = stratumPorts(cfg.Conf)
	if !cfg.NoNode && cfg.Conf != "" {
		a.node, a.zmqEP = loadNode(cfg.Conf, cfg.Log)
		if a.node != nil {
			a.enricher = a.node
		}
	}
	a.refreshFiles()
	return a, nil
}

// backlog reads the log tail before the start offset so the feed and the
// network fields are populated at once. Its events can never announce a
// block: they sit at or before the engine's start offset.
func (a *App) backlog(end int64) {
	f, err := os.Open(a.logPath)
	if err != nil {
		return
	}
	defer f.Close()
	from := max(end-a.cfg.Backlog, 0)
	buf := make([]byte, end-from)
	n, _ := f.ReadAt(buf, from)
	buf = buf[:n]
	off := from
	if from > 0 {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			return
		}
		buf, off = buf[i+1:], from+int64(i+1)
	}
	for len(buf) > 0 {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			break
		}
		a.line(string(buf[:i]), off, nil)
		buf, off = buf[i+1:], off+int64(i+1)
	}
	// A split message still held by the Joiner completes with the first
	// tailed line; its offset stays before the start, so it never announces.
	a.mu.Lock()
	a.appendFeed(feedRows(a.fe.Flush()))
	a.mu.Unlock()
}

// line handles one raw log line. The feed engine rejoins split messages
// itself, so it gets the raw line; the events engine and the parser get the
// lines rejoined by the Joiner. Messages for the program are appended to
// out when it is non-nil.
func (a *App) line(text string, off int64, out *[]tea.Msg) {
	rows := feedRows(a.fe.Feed(text, off))
	a.mu.Lock()
	a.appendFeed(rows)
	a.mu.Unlock()
	if out != nil && len(rows) > 0 {
		*out = append(*out, ui.FeedRowsMsg{Rows: rows})
	}
	for _, j := range a.join.Feed(text, off) {
		a.event(j.Line, j.Offset, out)
	}
}

// event handles one logical (rejoined) log line.
func (a *App) event(text string, off int64, out *[]tea.Msg) {
	ev := poolfmt.ParseLine(text)
	if ev.HasTime {
		a.noteLog(ev.Time)
	}
	if ev.Kind == poolfmt.KindUnknown {
		return
	}
	a.mu.Lock()
	switch ev.Kind {
	case poolfmt.KindNetworkDiff:
		if ev.Diff > 0 {
			a.logNet.diff = ev.Diff
			a.noteLogDiff(ev.Diff, evTime(ev, a.cfg.Now))
		}
	case poolfmt.KindZMQBlock:
		a.logNet.zmq = evTime(ev, a.cfg.Now)
	case poolfmt.KindStoredWorkbase:
		a.logNet.tmpl, a.logNet.tmplTxs = evTime(ev, a.cfg.Now), int(ev.Count)
	case poolfmt.KindBlockHashChanged:
		a.logNet.tipHash, a.logNet.tipHashTime = ev.Hash, evTime(ev, a.cfg.Now)
	}
	a.mu.Unlock()
	for _, o := range a.ev.Feed(ev, off) {
		switch o := o.(type) {
		case events.BlockFound:
			if out != nil {
				*out = append(*out, ui.BlockFoundMsg{Block: blockRow(o)})
				if c := celebrate.Cmd(a.cfg.Caps, o); c != nil {
					*out = append(*out, cmdMsg{c})
				}
			}
			go a.enrich(o.Height)
		case events.BlockUpdate:
			if out != nil {
				*out = append(*out, ui.BlockUpdateMsg{
					Height: int(o.Height),
					Hash:   o.Hash,
					Luck:   o.LuckPct,
				})
			}
		case events.NetworkBlock:
			a.netBlks.Add(1)
			if m, ok := a.noteNetworkBlock(o.Hash, o.Time); ok && out != nil {
				*out = append(*out, m)
			}
			a.poke()
		}
	}
}

// cmdMsg defers running a command until the message is delivered, so its
// timers start when it is sent rather than when it is queued.
type cmdMsg struct{ cmd tea.Cmd }

func evTime(ev poolfmt.Event, now func() time.Time) time.Time {
	if ev.HasTime {
		return ev.Time
	}
	return now()
}

func (a *App) appendFeed(rows []ui.FeedRow) {
	a.feed = append(a.feed, rows...)
	if n := len(a.feed); n > maxFeed {
		a.feed = append([]ui.FeedRow(nil), a.feed[n-maxFeed:]...)
	}
}

// Step reads new log lines once and returns the messages they produce.
func (a *App) Step() []tea.Msg {
	var out []tea.Msg
	n := 0
	_ = a.tl.Poll(func(l tail.Line) { n++; a.line(l.Text, l.Offset, &out) })
	if n == 0 { // idle poll: release anything held waiting for a continuation
		if rows := feedRows(a.fe.Flush()); len(rows) > 0 {
			a.mu.Lock()
			a.appendFeed(rows)
			a.mu.Unlock()
			out = append(out, ui.FeedRowsMsg{Rows: rows})
		}
		for _, j := range a.join.Flush() {
			a.event(j.Line, j.Offset, &out)
		}
	}
	return out
}

// Run keeps everything current until ctx ends, delivering messages through
// send (normally tea.Program.Send).
func (a *App) Run(ctx context.Context, send func(tea.Msg)) {
	// Run returns only once its helpers have, so a caller that closes the
	// App next never races an enrichment that is still writing.
	var wg sync.WaitGroup
	defer wg.Wait()
	wg.Go(func() { a.ix.Run(ctx) })
	if a.node != nil {
		wg.Go(func() { a.nodeLoop(ctx, send) })
	}
	if a.zmqEP != "" {
		wg.Go(func() { a.zmqLoop(ctx) })
	}
	poll := time.NewTicker(a.cfg.Poll)
	stats := time.NewTicker(a.cfg.StatsEvery)
	defer poll.Stop()
	defer stats.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
			msgs := a.Step()
			Deliver(msgs, send)
			if hasBlock(msgs) {
				_ = a.ix.Sync()
				send(ui.SnapshotMsg{Snapshot: a.snapshot(false)})
			}
		case <-stats.C:
			a.refreshFiles()
			send(ui.SnapshotMsg{Snapshot: a.snapshot(false)})
		}
	}
}

// Snapshot implements ui.Provider; it includes the feed backlog.
func (a *App) Snapshot() ui.Snapshot { return a.snapshot(true) }

// Close releases the index lock and the log handle.
func (a *App) Close() error {
	var err error
	a.closed.Do(func() {
		if a.tl != nil {
			a.tl.Close()
		}
		err = a.ix.Close()
	})
	return err
}

func (a *App) poke() {
	select {
	case a.kick <- struct{}{}:
	default:
	}
}

func feedRows(rs []feed.Row) []ui.FeedRow {
	if len(rs) == 0 {
		return nil
	}
	out := make([]ui.FeedRow, 0, len(rs))
	for _, r := range rs {
		tag, colour := feedTag(r)
		// The view prints the time itself; drop the copy in the text.
		text := r.Text
		if r.Stamp != "" {
			text = strings.TrimPrefix(text, r.Stamp+" ")
		}
		out = append(out, ui.FeedRow{Time: r.Time, Style: tag, Text: text, Colour: colour})
	}
	return out
}

// feedTag maps a feed style onto a short tag and a theme palette index
// (0 text, 1 accent, 2 warn, 3 bad, 4 gold, 5 dim, 6 blue, 7 violet).
func feedTag(r feed.Row) (string, int) {
	switch r.Style {
	case feed.StyleConnect:
		return "AUTH", 1
	case feed.StyleDrop:
		return "DROP", 2
	case feed.StyleFlap:
		return "FLAP", 2
	case feed.StylePool:
		return "POOL", 0
	case feed.StylePoolAlert:
		return "POOL", 3
	case feed.StyleDim:
		return "", 5
	case feed.StyleBlockBad:
		return "BLOCK", 3
	case feed.StyleBlockGood:
		return "BLOCK", 4
	case feed.StyleNewBlock:
		return "NET", 6
	case feed.StyleNetDiff:
		return "DIFF", 6
	case feed.StyleError:
		return "ERR", 3
	case feed.StyleAddr:
		return "USER", 7
	}
	return "", 0
}

func protoLabel(p string) string {
	switch strings.ToUpper(strings.TrimSpace(p)) {
	case "SV2", "SV2/JD", "JD":
		return "SV2"
	}
	return "SV1"
}

func blockRow(b events.BlockFound) ui.BlockRow {
	r := ui.BlockRow{Height: int(b.Height), Hash: b.Hash, Finder: b.Finder, Proto: protoLabel(b.Proto),
		Time: b.Time, Luck: b.LuckPct, Status: ui.BlockPending}
	if b.HasReward {
		r.Reward = int64(b.Reward*1e8 + 0.5)
	}
	return r
}

// Deliver sends msgs through send, running deferred commands.
func Deliver(msgs []tea.Msg, send func(tea.Msg)) {
	for _, m := range msgs {
		if c, ok := m.(cmdMsg); ok {
			go func() { send(c.cmd()) }()
			continue
		}
		send(m)
	}
}

func hasBlock(msgs []tea.Msg) bool {
	for _, m := range msgs {
		switch m.(type) {
		case ui.BlockFoundMsg, ui.BlockUpdateMsg:
			return true
		}
	}
	return false
}
