package fixturegen

import (
	"encoding/json"
	"fmt"
	"strings"
)

type workerDoc struct {
	Workername  string  `json:"workername"`
	Hashrate1m  string  `json:"hashrate1m"`
	Hashrate5m  string  `json:"hashrate5m"`
	Hashrate1hr string  `json:"hashrate1hr"`
	Hashrate1d  string  `json:"hashrate1d"`
	Hashrate7d  string  `json:"hashrate7d"`
	Lastshare   int64   `json:"lastshare"`
	Shares      int64   `json:"shares"`
	Bestshare   float64 `json:"bestshare"`
	Bestever    int64   `json:"bestever"`
}

type userDoc struct {
	Hashrate1m  string      `json:"hashrate1m"`
	Hashrate5m  string      `json:"hashrate5m"`
	Hashrate1hr string      `json:"hashrate1hr"`
	Hashrate1d  string      `json:"hashrate1d"`
	Hashrate7d  string      `json:"hashrate7d"`
	Lastshare   int64       `json:"lastshare"`
	Workers     int         `json:"workers"`
	Shares      int64       `json:"shares"`
	Roundshares int64       `json:"roundshares"`
	Bestshare   float64     `json:"bestshare"`
	Luck        float64     `json:"luck"`
	Networkdiff float64     `json:"networkdiff"`
	Bestever    int64       `json:"bestever"`
	Authorised  int64       `json:"authorised"`
	Worker      []workerDoc `json:"worker"`
}

// UserFile renders the pretty-printed per-user stats document (4-space
// indent, trailing newline) for user index u. Worker names carry no address
// prefix, as in the real files.
func (g *Generator) UserFile(u int) []byte {
	h := g.hr(u)
	d := userDoc{
		Hashrate1m: h[0], Hashrate5m: h[1], Hashrate1hr: h[2], Hashrate1d: h[3], Hashrate7d: h[4],
		Lastshare: g.now.Unix() - int64(g.rng.IntN(20)), Workers: g.nWorkers,
		Shares: 5_000_000 + int64(g.rng.IntN(1_000_000)), Roundshares: int64(g.NetworkDiff * g.round / 100 / float64(g.nUsers)),
		Bestshare: 1e9 * g.rng.Float64(), Luck: g.round / float64(g.nUsers), Networkdiff: g.NetworkDiff,
		Bestever: int64(2e12), Authorised: g.start.Unix(),
	}
	for w := 0; w < g.nWorkers; w++ {
		wh := g.hr(u)
		d.Worker = append(d.Worker, workerDoc{
			Workername: BareAddr(u) + "." + Rig(w),
			Hashrate1m: wh[0], Hashrate5m: wh[1], Hashrate1hr: wh[2], Hashrate1d: wh[3], Hashrate7d: wh[4],
			Lastshare: d.Lastshare - int64(g.rng.IntN(60)), Shares: 1_000_000 + int64(g.rng.IntN(500_000)),
			Bestshare: 1e8 * g.rng.Float64(), Bestever: int64(1e12),
		})
	}
	b, _ := json.MarshalIndent(d, "", "    ")
	return append(b, '\n')
}

// PoolStatus renders the three-line pool.status file.
func (g *Generator) PoolStatus() []byte {
	j := g.PoolStatusJSON()
	return []byte(strings.Join(j[:], "\n") + "\n")
}

// ShareLine renders one compact share record in the pool's key order. Pass
// valid=false for a below-target share. sdiff is the achieved difficulty.
func (g *Generator) ShareLine(workinfoid int64, valid bool, diff, sdiff float64) string {
	u, w := g.rng.IntN(g.nUsers), g.rng.IntN(g.nWorkers)
	result, errStr, errn := "true", "Valid", 0
	if !valid {
		result, errStr, errn = "false", "Above target", 5
	}
	hash := Hash(g.rng.IntN(1 << 20))
	return fmt.Sprintf(`{"workinfoid":%d,"clientid":%d,"enonce1":"%08x","nonce2":"%08x","nonce":"%08x","ntime":"%08x","diff":%s,"sdiff":%s,"hash":"%s","result":%s,"error":"%s","errn":%d,"createdate":"%d,%d","createby":"code","createcode":"parse_submit","createinet":"0.0.0.0:3333","workername":"%s","username":"%s","address":"%s","agent":"%s","version_mask":"1fffe000"}`,
		workinfoid, 1000+g.rng.IntN(100), g.rng.Uint32(), g.rng.Uint32(), g.rng.Uint32(), uint32(g.now.Unix()),
		f(diff, 1), f(sdiff, 6), hash, result, errStr, errn, g.now.Unix(), g.rng.IntN(1_000_000_000),
		BareAddr(u)+"."+Rig(w), Addr(u), IP(u*g.nWorkers+w), "Antminer S19/1.0|bitaxe")
}

// SharelogPath is the relative path for a workbase's share file.
func SharelogPath(height int, workinfoid int64) string {
	return fmt.Sprintf("%08x/%016x.sharelog", height, workinfoid)
}

// SolvedHeightsIdx renders solved-heights.idx: a marker line then sorted
// %08x heights.
func SolvedHeightsIdx(heights []int) []byte {
	hs := append([]int(nil), heights...)
	for i := 1; i < len(hs); i++ {
		for j := i; j > 0 && hs[j] < hs[j-1]; j-- {
			hs[j], hs[j-1] = hs[j-1], hs[j]
		}
	}
	var sb strings.Builder
	sb.WriteString("# cashstratum solve index v1: verified baseline\n")
	prev := -1
	for _, h := range hs {
		if h == prev {
			continue
		}
		prev = h
		fmt.Fprintf(&sb, "%08x\n", h)
	}
	return []byte(sb.String())
}

// SolveFileName is "<height>-<hash>.log".
func SolveFileName(height int, hash string) string { return fmt.Sprintf("%d-%s.log", height, hash) }

// SolveFile renders a preserved-solve file: seven "# key: value" header
// lines, a blank line, then the log window.
func SolveFile(height int, hash, finder, stamp string, window []string) []byte {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# CashStratum solve context — preserved %s UTC\n", stamp)
	sb.WriteString("# source:   logs/cashstratum.log (anchor line 1)\n")
	fmt.Fprintf(&sb, "# height:   %d\n", height)
	fmt.Fprintf(&sb, "# hash:     %s (ZMQ-seen: %s)\n", hash, hash)
	fmt.Fprintf(&sb, "# finder:   %s\n", finder)
	sb.WriteString("# chain:    confirmations=12 at preserve time -> ACCEPTED\n")
	fmt.Fprintf(&sb, "# window:   lines 1..%d\n\n", len(window))
	for _, l := range window {
		sb.WriteString(l)
	}
	return []byte(sb.String())
}

// SolveIndexRow is one INDEX.tsv record.
type SolveIndexRow struct {
	Height        int
	Hash          string
	Timestamp     string
	Outcome       string
	Finder        string
	Confirmations int
}

// SolveIndex renders INDEX.tsv.
func SolveIndex(rows []SolveIndexRow) []byte {
	var sb strings.Builder
	sb.WriteString("height\tblockhash\ttimestamp\toutcome\tfinder\tconfirmations\tcontext_file\n")
	for _, r := range rows {
		fmt.Fprintf(&sb, "%d\t%s\t%s\t%s\t%s\t%d\t%s\n", r.Height, r.Hash, r.Timestamp, r.Outcome, r.Finder, r.Confirmations, SolveFileName(r.Height, r.Hash))
	}
	return []byte(sb.String())
}
