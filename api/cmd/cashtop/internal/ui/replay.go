package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"cashstratumapi/cmd/cashtop/internal/celebrate"
	"cashstratumapi/cmd/cashtop/internal/events"
	"cashstratumapi/cmd/cashtop/internal/termcap"
)

// ReplayBlock is the block a replay shows: the newest one that was not
// rejected, or a stand-in at the current height when there is none yet.
func ReplayBlock(s Snapshot, now time.Time) BlockRow {
	for _, b := range s.Blocks {
		if b.Status != BlockRejected {
			return b
		}
	}
	h := s.Pool.Height
	if h <= 0 {
		h = s.Network.Tip + 1
	}
	return BlockRow{Height: h, Proto: "SV1", Time: now, Luck: PoolLuck(s.Pool.Accepted, s.Pool.NetworkDiff), Status: BlockPending}
}

// ReplayCmd replays a block find: the celebration takeover and, unless
// quiet, the bell and desktop notification. ":test-celebrate" and
// --replay-last-block both use it.
func ReplayCmd(caps termcap.Caps, s Snapshot, now time.Time) tea.Cmd {
	b := ReplayBlock(s, now)
	show := func() tea.Msg { return BlockFoundMsg{Block: b, Replay: true} }
	addr, rig := events.SplitFinder(b.Finder)
	ev := events.Replay(events.BlockFound{
		Height: int64(b.Height), Finder: b.Finder, Address: addr, Rig: rig, Proto: b.Proto, Hash: b.Hash,
		LuckPct: b.Luck, Time: b.Time, Reward: float64(b.Reward) / 1e8, HasReward: b.Reward > 0,
	})
	return tea.Batch(show, celebrate.Cmd(caps, ev))
}
