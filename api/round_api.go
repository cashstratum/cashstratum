package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"cashstratumapi/internal/apitypes"
	"cashstratumapi/internal/noderpc"
	"cashstratumapi/internal/poolfmt"
	"cashstratumapi/internal/poolindex"
	"cashstratumapi/internal/solvecheck"
)

const (
	// roundCacheTTL is how long a rendered round response is reused.
	roundCacheTTL = 5 * time.Second
	// roundMaintainEvery is the cadence of the orphan check and of the
	// node-field fill for frozen rounds.
	roundMaintainEvery = time.Minute
	// orphanDepth is how many blocks deep a solve is still re-checked
	// against the node's chain.
	orphanDepth = 100
	// historyMaxHours is the longest window /round/history serves.
	historyMaxHours = 72
	// roundNodeTimeout bounds each node call made for a request or a sweep.
	roundNodeTimeout = 5 * time.Second
)

// roundIndex is the part of poolindex.Index the round routes use.
type roundIndex interface {
	LiveRound(now time.Time) poolindex.LiveRound
	Solves(since int64) []poolindex.Solve
	FrozenRound(height int64) (poolindex.FrozenRound, bool)
	SetSolveStatus(height int64, status string) error
	VerifySolveHash(height int64, hash string) (bool, error)
	SetFrozenNode(height, coinbaseSats int64, finder, payroll poolindex.BlockOutput) (bool, error)
	HashrateSeries(from, to time.Time) []poolindex.Point
	BestShareSeries(from, to time.Time) []poolindex.Point
}

// roundDifficultyNode is implemented by nodes that can report the network
// difficulty; *noderpc.Client does.
type roundDifficultyNode interface {
	GetDifficulty(ctx context.Context) (float64, error)
}

// roundNode is the node access the round routes use; *noderpc.Client
// satisfies it.
type roundNode interface {
	GetBlockTemplateValue(ctx context.Context) (int64, error)
	GetBlockCount(ctx context.Context) (int64, error)
	GetBlockHash(ctx context.Context, height int64) (string, error)
	GetBlockCoinbase(ctx context.Context, hash string) (noderpc.BlockCoinbase, error)
}

var (
	// roundIx is nil until the index opened; the round routes answer 503 then.
	roundIx roundIndex
	// roundRPC is nil when no node endpoint is configured.
	roundRPC roundNode
	// roundNow is the clock of the round routes.
	roundNow = time.Now

	roundCacheMu sync.Mutex
	roundCache   = map[string]roundCached{}
)

type roundCached struct {
	at   time.Time
	body []byte
}

// resetRoundCache drops every cached response.
func resetRoundCache() {
	roundCacheMu.Lock()
	roundCache = map[string]roundCached{}
	roundCacheMu.Unlock()
}

func writeRoundError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// serveRound renders a round route through the short response cache. build
// returns the value to encode, or an HTTP status and message on failure;
// only successful bodies are cached.
func serveRound(w http.ResponseWriter, r *http.Request, build func(now time.Time) (any, int, string)) {
	if r.Method != http.MethodGet {
		writeRoundError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if roundIx == nil {
		writeRoundError(w, http.StatusServiceUnavailable, "round index unavailable")
		return
	}
	now := roundNow()
	key := r.URL.RequestURI()
	roundCacheMu.Lock()
	c, ok := roundCache[key]
	roundCacheMu.Unlock()
	if ok && now.Sub(c.at) < roundCacheTTL {
		w.Write(c.body)
		return
	}
	v, status, msg := build(now)
	if status != 0 {
		writeRoundError(w, status, msg)
		return
	}
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		writeRoundError(w, http.StatusInternalServerError, "encoding failed")
		return
	}
	roundCacheMu.Lock()
	roundCache[key] = roundCached{at: now, body: buf.Bytes()}
	roundCacheMu.Unlock()
	w.Write(buf.Bytes())
}

func fmtFloat(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func roundStr(s string) *string { return &s }

func unixPtr(t time.Time) *int64 {
	if t.IsZero() {
		return nil
	}
	u := t.Unix()
	return &u
}

func positivePtr(n int64) *int64 {
	if n <= 0 {
		return nil
	}
	return &n
}

// positiveFloatPtr is nil for a value that is not a known positive number.
func positiveFloatPtr(f float64) *float64 {
	if f <= 0 {
		return nil
	}
	return &f
}

func satsPtr(n int64) *string { return roundStr(strconv.FormatInt(n, 10)) }

// userHashrates reads each user file's one-hour hashrate (H/s) from the
// users snapshot, summed per canonical address, and the pool's one-minute
// and one-hour figures from pool.status. Both are empty when unreadable.
func userHashrates() (perAddr map[string]float64, pool1m, pool1h *float64) {
	snap, err := usersCache.get()
	if err != nil {
		return nil, nil, nil
	}
	perAddr = map[string]float64{}
	for _, e := range snap.users {
		uf, err := poolfmt.ParseUserFile(bytes.NewReader(e.raw))
		if err != nil {
			continue
		}
		perAddr[poolfmt.CanonicalAddress(e.name)] += uf.Hashrate1hr
	}
	pool1m, pool1h = poolHashrate(snap.pool, "hashrate1m"), poolHashrate(snap.pool, "hashrate1hr")
	return perAddr, pool1m, pool1h
}

func poolHashrate(pool map[string]interface{}, key string) *float64 {
	var s string
	switch v := pool[key].(type) {
	case string:
		s = v
	case json.Number:
		s = v.String()
	default:
		return nil
	}
	f, err := poolfmt.ParseHashrate(s)
	if err != nil {
		return nil
	}
	return &f
}

func handleRound(w http.ResponseWriter, r *http.Request) {
	serveRound(w, r, func(now time.Time) (any, int, string) {
		lr := roundIx.LiveRound(now)
		perAddr, pool1m, pool1h := userHashrates()
		if pts := roundIx.HashrateSeries(now.Add(-10*time.Minute), time.Time{}); len(pts) > 0 {
			v := pts[len(pts)-1].V
			pool1m = &v
		}
		out := apitypes.RoundResponse{
			Timestamp:         now.Unix(),
			LastBlockHeight:   positivePtr(lr.Height),
			StartedAt:         unixPtr(lr.StartedAt),
			TotalWork:         fmtFloat(lr.TotalWork),
			NetworkDifficulty: positiveFloatPtr(lr.NetworkDiff),
			Addresses:         []apitypes.RoundAddress{},
		}
		if pool1m != nil {
			out.Hashrate1m = roundStr(fmtFloat(*pool1m))
		}
		if pool1h != nil {
			out.Hashrate1h = roundStr(fmtFloat(*pool1h))
		}
		if roundRPC != nil {
			ctx, cancel := context.WithTimeout(r.Context(), roundNodeTimeout)
			if v, err := roundRPC.GetBlockTemplateValue(ctx); err == nil {
				out.CoinbaseValueSats = satsPtr(v)
			}
			// Right after a start the log has shown no difficulty yet; the
			// node knows it. Without a node answer it stays unknown (zero).
			if dn, ok := roundRPC.(roundDifficultyNode); ok && lr.NetworkDiff <= 0 {
				if d, err := dn.GetDifficulty(ctx); err == nil && d > 0 {
					lr.NetworkDiff = d
					out.NetworkDifficulty = &d
				}
			}
			cancel()
		}
		for _, a := range lr.Addresses {
			ra := apitypes.RoundAddress{
				Address: a.Address, Username: a.Username,
				Work: fmtFloat(a.Work), Shares: strconv.FormatInt(a.Shares, 10),
				LastShareAt: unixPtr(a.LastShareAt), BestShare: a.BestShare, Online: a.Online,
			}
			if lr.NetworkDiff > 0 {
				p := a.BestShare / lr.NetworkDiff * 100
				ra.BestOfNet = &p
			}
			if h, ok := perAddr[a.Address]; ok {
				ra.Hashrate1h = roundStr(fmtFloat(h))
			}
			out.Addresses = append(out.Addresses, ra)
		}
		return out, 0, ""
	})
}

func handleRoundsSolved(w http.ResponseWriter, r *http.Request) {
	serveRound(w, r, func(now time.Time) (any, int, string) {
		var since int64
		if v := r.URL.Query().Get("since"); v != "" {
			n, err := strconv.ParseInt(v, 10, 64)
			if err != nil || n < 0 {
				return nil, http.StatusBadRequest, "since must be a non-negative block height"
			}
			since = n
		}
		out := apitypes.RoundSolvedResponse{Solves: []apitypes.RoundSolve{}, Timestamp: now.Unix()}
		for _, s := range roundIx.Solves(since) {
			out.Solves = append(out.Solves, apitypes.RoundSolve{
				Height: s.Height, Hash: s.Hash, SolvedAt: unixPtr(s.SolvedAt), Status: s.Status,
			})
		}
		return out, 0, ""
	})
}

func roundOutput(o *poolindex.BlockOutput) *apitypes.RoundOutput {
	if o == nil {
		return nil
	}
	return &apitypes.RoundOutput{Address: o.Address, Vout: o.Vout, ValueSats: strconv.FormatInt(o.Sats, 10)}
}

// handleRoundByHeight serves /round/{height}.
func handleRoundByHeight(w http.ResponseWriter, r *http.Request) {
	serveRound(w, r, func(now time.Time) (any, int, string) {
		h, err := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/round/"), 10, 64)
		if err != nil || h <= 0 {
			return nil, http.StatusBadRequest, "height must be a positive block height"
		}
		fr, ok := roundIx.FrozenRound(h)
		if !ok {
			return nil, http.StatusNotFound, "not a solve height"
		}
		out := apitypes.FrozenRoundResponse{
			Height: fr.Height, Hash: fr.Hash, Status: fr.Status,
			SolvedAt: unixPtr(fr.SolvedAt), PrevBlockHeight: positivePtr(fr.PrevBlockHeight),
			StartedAt: unixPtr(fr.StartedAt), FinderAddress: fr.FinderAddress, FinderWorker: fr.FinderWorker,
			FinderOutput: roundOutput(fr.FinderOutput), PayrollOutput: roundOutput(fr.PayrollOutput),
			TotalWork: fmtFloat(fr.TotalWork), Addresses: []apitypes.FrozenAddress{},
		}
		if fr.CoinbaseSats != nil {
			out.CoinbaseValueSats = satsPtr(*fr.CoinbaseSats)
		}
		for _, a := range fr.Addresses {
			out.Addresses = append(out.Addresses, apitypes.FrozenAddress{
				Address: a.Address, Username: a.Username, Work: fmtFloat(a.Work),
				Shares: strconv.FormatInt(a.Shares, 10), LastShareAt: unixPtr(a.LastShareAt),
			})
		}
		return out, 0, ""
	})
}

// handleRoundTree routes /round/history and /round/{height}.
func handleRoundTree(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/round/history" {
		handleRoundHistory(w, r)
		return
	}
	handleRoundByHeight(w, r)
}

func handleRoundHistory(w http.ResponseWriter, r *http.Request) {
	serveRound(w, r, func(now time.Time) (any, int, string) {
		q := r.URL.Query()
		hours, bucket := 24, 60
		if v := q.Get("hours"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || n < 1 || n > historyMaxHours {
				return nil, http.StatusBadRequest, "hours must be between 1 and 72"
			}
			hours = n
		}
		if v := q.Get("bucket_minutes"); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil || (n != 5 && n != 10 && n != 15 && n != 30 && n != 60) {
				return nil, http.StatusBadRequest, "bucket_minutes must be one of 5, 10, 15, 30, 60"
			}
			bucket = n
		}
		step := int64(bucket) * 60
		last := now.Unix() / step * step
		first := (now.Unix() - int64(hours)*3600) / step * step
		n := int((last-first)/step) + 1
		sums := make([]float64, n)
		counts := make([]int, n)
		best := make([]float64, n)
		hasBest := make([]bool, n)
		from := time.Unix(first, 0)
		to := time.Unix(last+step, 0)
		idx := func(t int64) int { return int((t - first) / step) }
		for _, p := range roundIx.HashrateSeries(from, to) {
			i := idx(p.T)
			sums[i] += p.V
			counts[i]++
		}
		for _, p := range roundIx.BestShareSeries(from, to) {
			i := idx(p.T)
			if !hasBest[i] || p.V > best[i] {
				best[i], hasBest[i] = p.V, true
			}
		}
		out := apitypes.RoundHistoryResponse{BucketMinutes: bucket, Buckets: make([]apitypes.RoundHistoryBucket, n), Timestamp: now.Unix()}
		for i := range out.Buckets {
			start := first + int64(i)*step
			b := apitypes.RoundHistoryBucket{BucketStart: start, Partial: start+step > now.Unix()}
			if counts[i] > 0 {
				b.Hashrate = roundStr(fmtFloat(sums[i] / float64(counts[i])))
			}
			if hasBest[i] {
				b.BestShare = roundStr(fmtFloat(best[i]))
			}
			out.Buckets[i] = b
		}
		return out, 0, ""
	})
}

// registerRoundRoutes adds the round routes to mux.
func registerRoundRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/round", authMiddleware(handleRound))
	mux.HandleFunc("/rounds/solved", authMiddleware(handleRoundsSolved))
	mux.HandleFunc("/round/", authMiddleware(handleRoundTree))
}

// loadRoundOwner reads pooladdress and btcsig from the pool conf.
func loadRoundOwner(confPath string) solvecheck.Owner { return solvecheck.LoadOwner(confPath) }

// sweepSolves settles every solve against the node's chain by ownership,
// not by comparing the recorded hash (which can be provisional). Where the
// chain block at the solve's height is ours, its hash is recorded, its
// coinbase value and outputs are filled once (vout 0 the finder, vout 1 the
// payroll) and it is accepted; where it is someone else's and the chain has
// moved past that height, the solve is orphaned. A node error, or an owner
// that cannot be told, changes nothing. Solves final for orphanDepth blocks
// are left alone once settled.
func sweepSolves(ctx context.Context, ix roundIndex, node roundNode, owner solvecheck.Owner) {
	if !owner.Known() {
		return
	}
	tip, err := node.GetBlockCount(ctx)
	if err != nil {
		return
	}
	for _, s := range ix.Solves(0) {
		fr, ok := ix.FrozenRound(s.Height)
		if !ok || s.Height > tip {
			continue
		}
		final := tip-s.Height >= orphanDepth
		if final && fr.HashVerified && (fr.Status == poolindex.StatusOrphaned || fr.CoinbaseSats != nil) {
			continue
		}
		v, _, cb, err := solvecheck.Settle(ctx, ix, node, owner, s.Height, tip, s.Status)
		if err != nil {
			log.Printf("round %d: settling: %v", s.Height, err)
		}
		if v == solvecheck.Ours && len(cb.Outputs) >= 2 && fr.CoinbaseSats == nil {
			var total int64
			for _, o := range cb.Outputs {
				total += o.Sats
			}
			conv := func(o noderpc.BlockOutput) poolindex.BlockOutput {
				return poolindex.BlockOutput{Address: poolfmt.CanonicalAddress(o.Address), Vout: o.Vout, Sats: o.Sats}
			}
			if _, err := ix.SetFrozenNode(s.Height, total, conv(cb.Outputs[0]), conv(cb.Outputs[1])); err != nil {
				log.Printf("round %d: saving node fields: %v", s.Height, err)
			}
		}
	}
}

// runRoundMaintenance sweeps until ctx is done.
func runRoundMaintenance(ctx context.Context, ix roundIndex, node roundNode, owner solvecheck.Owner, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		sweep := func() {
			c, cancel := context.WithTimeout(ctx, 4*roundNodeTimeout)
			defer cancel()
			sweepSolves(c, ix, node, owner)
		}
		sweep()
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// startRoundIndex opens the round index and starts its sync and maintenance
// loops. On failure the round routes keep answering 503 and the rest of the
// API is unaffected.
func startRoundIndex(ctx context.Context, roundConfPath string) {
	opt := poolindex.Options{
		Root:       filepath.Dir(filepath.Dir(filepath.Clean(logPath))),
		LogPath:    logPath,
		CacheDir:   roundIndexDir(),
		RoundStore: roundsDir(),
	}
	ix, err := poolindex.Open(opt)
	if err != nil {
		log.Printf("Round index: disabled: %v", err)
		return
	}
	go ix.Run(ctx)
	roundIx = ix
	if btcdRPC != nil {
		client := noderpc.NewWithOptions(btcdRPC, roundNodeTimeout, "cashstratum-api")
		roundRPC = client
		owner := loadRoundOwner(roundConfPath)
		if !owner.Known() {
			log.Printf("Round index: no pooladdress or btcsig in the conf -- orphan check and node fields disabled")
		}
		go runRoundMaintenance(ctx, ix, client, owner, roundMaintainEvery)
	}
	log.Printf("Round index: cache %s, rounds %s", opt.CacheDir, opt.RoundStore)
}

// roundStateRoot is the directory the round index and store default to,
// beside the log directory.
func roundStateRoot() string {
	return filepath.Join(filepath.Dir(filepath.Dir(filepath.Clean(logPath))), "api-state")
}

func roundIndexDir() string {
	if v := getEnvWithFallback("CASHSTRATUM_INDEX_DIR", "CKPOOL_INDEX_DIR"); v != "" {
		return v
	}
	return filepath.Join(roundStateRoot(), "index")
}

func roundsDir() string {
	if v := getEnvWithFallback("CASHSTRATUM_ROUNDS_DIR", "CKPOOL_ROUNDS_DIR"); v != "" {
		return v
	}
	return filepath.Join(roundStateRoot(), "rounds")
}
