package poolfmt

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// PoolStatus is the pool.status file (three JSON lines) or one of the three
// "Pool:{...}" log variants. The Has* flags say which parts were filled.
type PoolStatus struct {
	HasStatus, HasHashrate, HasShares bool

	// Line 1.
	Runtime, LastUpdate, Users, Workers, Idle, Disconnected int64

	// Line 2, in H/s.
	Hashrate1m, Hashrate5m, Hashrate15m, Hashrate1hr, Hashrate6hr, Hashrate1d, Hashrate7d float64

	// Line 3. Diff and Luck are progress toward the current block, in percent of the network
	// difficulty; they reset when the pool solves a block.
	Diff, Luck                  float64
	Accepted, Rejected          int64
	BestShare                   float64
	SPS1m, SPS5m, SPS15m, SPS1h float64
}

type rawPool struct {
	Runtime      *int64 `json:"runtime"`
	LastUpdate   int64  `json:"lastupdate"`
	Users        int64  `json:"Users"`
	Workers      int64  `json:"Workers"`
	Idle         int64  `json:"Idle"`
	Disconnected int64  `json:"Disconnected"`

	Diff      *float64 `json:"diff"`
	Luck      float64  `json:"luck"`
	Accepted  int64    `json:"accepted"`
	Rejected  int64    `json:"rejected"`
	BestShare float64  `json:"bestshare"`
	SPS1m     float64  `json:"SPS1m"`
	SPS5m     float64  `json:"SPS5m"`
	SPS15m    float64  `json:"SPS15m"`
	SPS1h     float64  `json:"SPS1h"`
}

// poolFrom decodes one Pool JSON object into dst and reports which part it was.
// The part is chosen by the first key, as the writer emits them in a fixed order.
func poolFrom(js string, dst *PoolStatus) bool {
	js = strings.TrimSpace(js)
	var first string
	switch {
	case strings.HasPrefix(js, `{"runtime"`):
		first = "status"
	case strings.HasPrefix(js, `{"hashrate1m"`):
		first = "hash"
	case strings.HasPrefix(js, `{"diff"`):
		first = "shares"
	default:
		return false
	}
	switch first {
	case "status":
		var r rawPool
		if json.Unmarshal([]byte(js), &r) != nil || r.Runtime == nil {
			return false
		}
		dst.HasStatus = true
		dst.Runtime, dst.LastUpdate = *r.Runtime, r.LastUpdate
		dst.Users, dst.Workers, dst.Idle, dst.Disconnected = r.Users, r.Workers, r.Idle, r.Disconnected
	case "hash":
		var m map[string]json.RawMessage
		if json.Unmarshal([]byte(js), &m) != nil {
			return false
		}
		dst.HasHashrate = true
		dst.Hashrate1m, _ = hr(m["hashrate1m"])
		dst.Hashrate5m, _ = hr(m["hashrate5m"])
		dst.Hashrate15m, _ = hr(m["hashrate15m"])
		dst.Hashrate1hr, _ = hr(m["hashrate1hr"])
		dst.Hashrate6hr, _ = hr(m["hashrate6hr"])
		dst.Hashrate1d, _ = hr(m["hashrate1d"])
		dst.Hashrate7d, _ = hr(m["hashrate7d"])
	case "shares":
		var r rawPool
		if json.Unmarshal([]byte(js), &r) != nil || r.Diff == nil {
			return false
		}
		dst.HasShares = true
		dst.Diff, dst.Luck = *r.Diff, r.Luck
		dst.Accepted, dst.Rejected, dst.BestShare = r.Accepted, r.Rejected, r.BestShare
		dst.SPS1m, dst.SPS5m, dst.SPS15m, dst.SPS1h = r.SPS1m, r.SPS5m, r.SPS15m, r.SPS1h
	}
	return true
}

// ParsePoolStatus parses pool.status. Fewer than three lines, or any line that
// fails to parse, returns an error so the caller can retry.
func ParsePoolStatus(r io.Reader) (PoolStatus, error) {
	b, err := readAll(r)
	if err != nil {
		return PoolStatus{}, err
	}
	var lines []string
	for _, l := range bytes.Split(b, []byte{'\n'}) {
		if s := strings.TrimSpace(string(l)); s != "" {
			lines = append(lines, s)
		}
	}
	if len(lines) == 0 {
		return PoolStatus{}, ErrEmpty
	}
	if len(lines) < 3 {
		return PoolStatus{}, ErrIncomplete
	}
	var ps PoolStatus
	for _, l := range lines[:3] {
		if !poolFrom(l, &ps) {
			return PoolStatus{}, ErrIncomplete
		}
	}
	if !ps.HasStatus || !ps.HasHashrate || !ps.HasShares {
		return PoolStatus{}, ErrIncomplete
	}
	return ps, nil
}
