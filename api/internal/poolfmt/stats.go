package poolfmt

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
)

// RejectBuckets counts rejected shares by cause.
type RejectBuckets struct {
	Stale, Duplicate, LowDiff, NTime, BadJob, Malformed, Worker int64
}

// RejectStats carries the optional reject accounting that newer pools emit.
// Present is false for pools that predate it; the counters are then zero but
// meaningless.
type RejectStats struct {
	Present    bool
	Rejected   int64
	Count      int64
	Buckets    RejectBuckets
	LastReject Opt[int64]
	LastReason Opt[string]
	// Rates use the hashrate suffix format and are reported in H/s units.
	Rate1m, Rate5m, Rate1hr Opt[float64]
}

// UserStats is the statistics object shared by users/<address> files and the
// "User <address>:{...}" log lines. The solve-time log form carries only the
// hashrates, shares and authorised; LastShare.OK distinguishes the periodic
// form.
type UserStats struct {
	Hashrate1m, Hashrate5m, Hashrate1hr, Hashrate1d, Hashrate7d float64

	LastShare   Opt[int64]
	Workers     Opt[int64]
	Shares      int64
	RoundShares Opt[int64]
	BestShare   Opt[float64]
	Luck        Opt[float64]
	NetworkDiff Opt[float64]
	BestEver    Opt[int64]
	Authorised  int64

	Reject RejectStats
}

// WorkerStats is one worker[] entry or a "Worker <name>:{...}" log line.
type WorkerStats struct {
	Name string

	Hashrate1m, Hashrate5m, Hashrate1hr, Hashrate1d, Hashrate7d float64

	LastShare Opt[int64]
	Shares    Opt[int64]
	BestShare Opt[float64]
	BestEver  Opt[int64]

	Reject RejectStats

	Connects             Opt[int64]
	Disconnects          Opt[int64]
	LastDisconnect       Opt[int64]
	LastDisconnectReason Opt[string]
}

// UserFile is a parsed users/<address> file.
type UserFile struct {
	UserStats
	Worker []WorkerStats
}

type rawStats struct {
	WorkerName *string `json:"workername"`

	H1m  json.RawMessage `json:"hashrate1m"`
	H5m  json.RawMessage `json:"hashrate5m"`
	H1hr json.RawMessage `json:"hashrate1hr"`
	H1d  json.RawMessage `json:"hashrate1d"`
	H7d  json.RawMessage `json:"hashrate7d"`

	LastShare   *int64   `json:"lastshare"`
	Workers     *int64   `json:"workers"`
	Shares      *int64   `json:"shares"`
	RoundShares *int64   `json:"roundshares"`
	BestShare   *float64 `json:"bestshare"`
	Luck        *float64 `json:"luck"`
	NetworkDiff *float64 `json:"networkdiff"`
	BestEver    *int64   `json:"bestever"`
	Authorised  *int64   `json:"authorised"`

	Rejected    *int64 `json:"rejected"`
	RejectedCnt *int64 `json:"rejectedcount"`
	Rejects     *struct {
		Stale, Duplicate, LowDiff, NTime, BadJob, Malformed, Worker int64
	} `json:"-"`
	RejectsRaw       json.RawMessage `json:"rejects"`
	LastReject       *int64          `json:"lastreject"`
	LastRejectReason *string         `json:"lastrejectreason"`
	RR1m             json.RawMessage `json:"rejectrate1m"`
	RR5m             json.RawMessage `json:"rejectrate5m"`
	RR1hr            json.RawMessage `json:"rejectrate1hr"`

	Connects       *int64  `json:"connects"`
	Disconnects    *int64  `json:"disconnects"`
	LastDisconnect *int64  `json:"lastdisconnect"`
	LastDiscReason *string `json:"lastdisconnectreason"`
}

// hr decodes a hashrate that is normally a JSON string but may be a number.
func hr(raw json.RawMessage) (float64, bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || string(raw) == "null" {
		return 0, false
	}
	var s string
	if raw[0] == '"' {
		if json.Unmarshal(raw, &s) != nil {
			return 0, false
		}
	} else {
		s = string(raw)
	}
	f, err := ParseHashrate(s)
	return f, err == nil
}

func hrOpt(raw json.RawMessage) Opt[float64] {
	f, ok := hr(raw)
	return Opt[float64]{f, ok}
}

func optI(p *int64) Opt[int64] {
	if p == nil {
		return Opt[int64]{}
	}
	return some(*p)
}

func optF(p *float64) Opt[float64] {
	if p == nil {
		return Opt[float64]{}
	}
	return some(*p)
}

func optS(p *string) Opt[string] {
	if p == nil {
		return Opt[string]{}
	}
	return some(*p)
}

func (r *rawStats) reject() RejectStats {
	var rs RejectStats
	rs.Present = r.Rejected != nil || r.RejectedCnt != nil || len(r.RejectsRaw) > 0
	if r.Rejected != nil {
		rs.Rejected = *r.Rejected
	}
	if r.RejectedCnt != nil {
		rs.Count = *r.RejectedCnt
	}
	if len(r.RejectsRaw) > 0 {
		var b struct {
			Stale     int64 `json:"stale"`
			Duplicate int64 `json:"duplicate"`
			LowDiff   int64 `json:"lowdiff"`
			NTime     int64 `json:"ntime"`
			BadJob    int64 `json:"badjob"`
			Malformed int64 `json:"malformed"`
			Worker    int64 `json:"worker"`
		}
		if json.Unmarshal(r.RejectsRaw, &b) == nil {
			rs.Buckets = RejectBuckets(b)
		}
	}
	rs.LastReject = optI(r.LastReject)
	rs.LastReason = optS(r.LastRejectReason)
	rs.Rate1m, rs.Rate5m, rs.Rate1hr = hrOpt(r.RR1m), hrOpt(r.RR5m), hrOpt(r.RR1hr)
	return rs
}

func (r *rawStats) user() UserStats {
	var u UserStats
	u.Hashrate1m, _ = hr(r.H1m)
	u.Hashrate5m, _ = hr(r.H5m)
	u.Hashrate1hr, _ = hr(r.H1hr)
	u.Hashrate1d, _ = hr(r.H1d)
	u.Hashrate7d, _ = hr(r.H7d)
	u.LastShare = optI(r.LastShare)
	u.Workers = optI(r.Workers)
	if r.Shares != nil {
		u.Shares = *r.Shares
	}
	u.RoundShares = optI(r.RoundShares)
	u.BestShare = optF(r.BestShare)
	u.Luck = optF(r.Luck)
	u.NetworkDiff = optF(r.NetworkDiff)
	u.BestEver = optI(r.BestEver)
	if r.Authorised != nil {
		u.Authorised = *r.Authorised
	}
	u.Reject = r.reject()
	return u
}

func (r *rawStats) worker() WorkerStats {
	var w WorkerStats
	if r.WorkerName != nil {
		w.Name = *r.WorkerName
	}
	w.Hashrate1m, _ = hr(r.H1m)
	w.Hashrate5m, _ = hr(r.H5m)
	w.Hashrate1hr, _ = hr(r.H1hr)
	w.Hashrate1d, _ = hr(r.H1d)
	w.Hashrate7d, _ = hr(r.H7d)
	w.LastShare = optI(r.LastShare)
	w.Shares = optI(r.Shares)
	w.BestShare = optF(r.BestShare)
	w.BestEver = optI(r.BestEver)
	w.Reject = r.reject()
	w.Connects = optI(r.Connects)
	w.Disconnects = optI(r.Disconnects)
	w.LastDisconnect = optI(r.LastDisconnect)
	w.LastDisconnectReason = optS(r.LastDiscReason)
	return w
}

func decodeStats(js string) (*rawStats, bool) {
	var r rawStats
	if err := json.Unmarshal([]byte(js), &r); err != nil {
		return nil, false
	}
	return &r, true
}

// ParseUserFile parses a users/<address> file. An empty or torn file yields an
// error; callers should retry once and otherwise keep the last good copy.
func ParseUserFile(r io.Reader) (UserFile, error) {
	b, err := readAll(r)
	if err != nil {
		return UserFile{}, err
	}
	if len(bytes.TrimSpace(b)) == 0 {
		return UserFile{}, ErrEmpty
	}
	var top struct {
		rawStats
		Worker []rawStats `json:"worker"`
	}
	if err := json.Unmarshal(b, &top); err != nil {
		return UserFile{}, err
	}
	uf := UserFile{UserStats: top.rawStats.user()}
	for i := range top.Worker {
		uf.Worker = append(uf.Worker, top.Worker[i].worker())
	}
	return uf, nil
}

func trimLine(s string) string { return strings.TrimRight(s, " \t\r\n") }
