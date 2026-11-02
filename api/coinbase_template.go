package main

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"cashstratumapi/internal/cashaddr"
)

// /coinbase per-height template.
//
// In btcsolo every user's coinbase for one workbase is the same bytes except
// the miner payout script (src/stratifier.c generate_coinbase and
// __generate_userwb):
//
//	coinbase = coinb1 ‖ en1 ‖ en2 ‖ coinb2Head ‖ len(script) ‖ script ‖ coinb3
//
//	coinb1      version | input | scriptSig length | height | flags | time | nsec | en push length
//	coinb2Head  [btcsig push] | ffffffff sequence | output count | miner value (8 LE)
//	coinb3      [fee value | fee script] | [OP_RETURN output] | locktime
//
// So /coinbase probes the pool ONCE per height under a fixed identity, splits
// that job into these parts, and answers every address by splicing its own
// output script in. A per-user probe (which makes the pool create a user and a
// per-workbase userwb that live until restart) is only the fallback.
//
// en1 and en2 in a spliced answer are placeholders, exactly as in a probe
// answer: the template probe's extranonce1 and a zeroed extranonce2. A solved
// block carries the finding miner's own bytes there.

// stratumHost is the Stratum endpoint /coinbase probes. A variable only so
// tests can point it at a fake pool.
var stratumHost = DefaultStratumHost

// coinbaseTemplateIdentity is the canonical CashAddr the template probe
// authorises (as "<addr>."+ProbeWorkerSuffix), set in main from ckpool.conf's
// pooladdress. Empty means none is configured: the probe then authorises the
// requesting address, as the per-user probe always has.
var coinbaseTemplateIdentity string

// templateRetryInterval stops a lagging pool (one still on an older height
// than callers ask for) from being re-probed by every request: within this
// window after a refresh, a still-unusable template means fallback, not
// another template probe.
const templateRetryInterval = 2 * time.Second

// coinbaseTemplates is the process-wide per-height template cache.
var coinbaseTemplates = newCoinbaseTemplateCache()

// coinbaseTemplate is one parsed Stratum job.
type coinbaseTemplate struct {
	height      int
	coinb1      []byte
	en1Sample   []byte
	en2Size     int
	coinb2Head  []byte
	coinb3      []byte
	networkBits string
	// network is the CashAddr prefix the pool pays on (cashaddrPrefix when
	// the template was fetched). Only addresses on this network are spliced.
	network   string
	fetchedAt time.Time
}

// assemble returns the full coinbase transaction paying script.
func (t *coinbaseTemplate) assemble(script []byte) []byte {
	out := make([]byte, 0, len(t.coinb1)+len(t.en1Sample)+t.en2Size+len(t.coinb2Head)+1+len(script)+len(t.coinb3))
	out = append(out, t.coinb1...)
	out = append(out, t.en1Sample...)
	out = append(out, make([]byte, t.en2Size)...)
	out = append(out, t.coinb2Head...)
	out = append(out, byte(len(script)))
	out = append(out, script...)
	return append(out, t.coinb3...)
}

// splitCoinb2 splits a job's per-user coinb2 into the shared head, the payout
// script and the shared tail by walking its structure, never by searching for
// the script's bytes (the same hash can also sit inside btcsig or the fee
// script).
//
// The btcsig push length is not guessed from coinb2: coinb1 declares the full
// scriptSig length, so whatever the scriptSig still needs after coinb1, en1
// and en2 is the btcsig push at the head of coinb2 (0 when there is none).
func splitCoinb2(coinb1 []byte, en1Size, en2Size int, coinb2 []byte) (head, script, coinb3 []byte, err error) {
	const scriptSigLenOffset = 4 + 1 + 32 + 4 // version, input count, prevout
	scriptLen, off, ok := readVarint(coinb1, scriptSigLenOffset)
	if !ok || off > len(coinb1) {
		return nil, nil, nil, errors.New("coinb1 too short for a scriptSig length")
	}
	sigLen := int(scriptLen) - (len(coinb1) - off) - en1Size - en2Size
	if sigLen < 0 || sigLen > len(coinb2) {
		return nil, nil, nil, fmt.Errorf("scriptSig length %d does not fit coinb1/en1/en2/coinb2", scriptLen)
	}
	if sigLen > 0 && int(coinb2[0]) != sigLen-1 {
		return nil, nil, nil, fmt.Errorf("btcsig push byte %#x does not match its %d-byte remainder", coinb2[0], sigLen-1)
	}

	pos := sigLen
	if pos+4 > len(coinb2) || !bytes.Equal(coinb2[pos:pos+4], []byte{0xff, 0xff, 0xff, 0xff}) {
		return nil, nil, nil, errors.New("no ffffffff sequence after the btcsig push")
	}
	pos += 4

	count, pos, ok := readVarint(coinb2, pos)
	// 1 (dust fee or no fee) or 2 (fee) outputs, each +1 with a witness output.
	if !ok || count < 1 || count > 3 {
		return nil, nil, nil, errors.New("bad output count")
	}
	if pos+8 > len(coinb2) {
		return nil, nil, nil, errors.New("truncated miner value")
	}
	pos += 8
	head = coinb2[:pos]

	if pos >= len(coinb2) {
		return nil, nil, nil, errors.New("truncated miner script length")
	}
	// The pool writes the payout script length as one byte (__generate_userwb).
	n := int(coinb2[pos])
	pos++
	if n == 0 || pos+n > len(coinb2) {
		return nil, nil, nil, errors.New("bad miner script length")
	}
	script = coinb2[pos : pos+n]
	pos += n
	coinb3 = coinb2[pos:]

	// Validate the tail: the remaining outputs, then exactly a locktime.
	tail := 0
	for i := uint64(1); i < count; i++ {
		if tail+8 > len(coinb3) {
			return nil, nil, nil, errors.New("truncated output value in coinb3")
		}
		tail += 8
		l, next, ok := readVarint(coinb3, tail)
		if !ok || next+int(l) > len(coinb3) {
			return nil, nil, nil, errors.New("truncated output script in coinb3")
		}
		tail = next + int(l)
	}
	if len(coinb3)-tail != 4 {
		return nil, nil, nil, fmt.Errorf("coinb3 has %d bytes after its outputs, want a 4-byte locktime", len(coinb3)-tail)
	}
	return head, script, coinb3, nil
}

// newCoinbaseTemplate parses one probe job into a template.
func newCoinbaseTemplate(job stratumJob, network string, fetchedAt time.Time) (*coinbaseTemplate, error) {
	coinb1, err := hex.DecodeString(job.coinb1)
	if err != nil {
		return nil, fmt.Errorf("coinb1: %w", err)
	}
	coinb2, err := hex.DecodeString(job.coinb2)
	if err != nil {
		return nil, fmt.Errorf("coinb2: %w", err)
	}
	en1, err := hex.DecodeString(job.extranonce1)
	if err != nil {
		return nil, fmt.Errorf("extranonce1: %w", err)
	}
	if job.en2Size < 0 {
		return nil, errors.New("negative extranonce2 size")
	}
	height := extractBlockHeight(job.coinb1)
	if height == nil {
		return nil, errors.New("coinb1 carries no block height")
	}
	head, _, coinb3, err := splitCoinb2(coinb1, len(en1), job.en2Size, coinb2)
	if err != nil {
		return nil, err
	}
	return &coinbaseTemplate{
		height:      *height,
		coinb1:      coinb1,
		en1Sample:   en1,
		en2Size:     job.en2Size,
		coinb2Head:  head,
		coinb3:      coinb3,
		networkBits: job.networkBits,
		network:     network,
		fetchedAt:   fetchedAt,
	}, nil
}

// fetchCoinbaseTemplate probes the pool once as identity and parses the job.
func fetchCoinbaseTemplate(identity string) (*coinbaseTemplate, error) {
	fetchedAt := time.Now()
	job, failed, ok := probeStratumJob(CoinbaseResponse{}, "coinbase-template/api", identity+"."+ProbeWorkerSuffix)
	if !ok {
		return nil, errors.New(failed.response.Error)
	}
	return newCoinbaseTemplate(job, cashaddrPrefix, fetchedAt)
}

// templateMaxAge is how long a template serves calls that name no
// candidate_height: the pool's update_interval, the cadence at which it
// issues a fresh job anyway.
func templateMaxAge() time.Duration {
	return stratumJobDeadline - stratumDeadlineSlack
}

// coinbaseTemplateCache holds the one current template. At most one refresh
// probe runs at a time; concurrent callers wait for it and share its result.
type coinbaseTemplateCache struct {
	mu          sync.Mutex
	tmpl        *coinbaseTemplate
	inflight    chan struct{}
	lastRefresh time.Time
	// lastFailed and lastWanted describe the most recent refresh: whether
	// its probe failed, and the candidate_height that asked for it (0 for a
	// legacy call). See recentlyMissed.
	lastFailed bool
	lastWanted int
	fetch      func(identity string) (*coinbaseTemplate, error)
}

// recentlyMissed reports whether a refresh inside templateRetryInterval
// already tried for what this call wants and could not get it: its probe
// failed, or it asked for this height or a higher one and the pool still
// served a lower one (a pool lagging behind its callers). Probing again would
// only repeat that, so the call falls back instead. Caller holds c.mu.
func (c *coinbaseTemplateCache) recentlyMissed(candidateHeight *int) bool {
	if c.lastRefresh.IsZero() || time.Since(c.lastRefresh) >= templateRetryInterval {
		return false
	}
	if c.lastFailed {
		return true
	}
	return candidateHeight != nil && *candidateHeight <= c.lastWanted
}

func newCoinbaseTemplateCache() *coinbaseTemplateCache {
	return &coinbaseTemplateCache{fetch: fetchCoinbaseTemplate}
}

// decide reports whether t answers a call for candidateHeight (nil = legacy
// call), and if not, whether a refresh could make it.
//
//   - candidate == cached height: use it.
//   - candidate  > cached height: refresh.
//   - candidate  < cached height: neither. The pool has moved on and never
//     re-issues an old height, so no template can match; the caller keeps
//     today's stale-height behaviour, a per-user probe.
//   - no candidate: use it while younger than update_interval, else refresh.
func (t *coinbaseTemplate) decide(candidateHeight *int, now time.Time) (use, refresh bool) {
	if t == nil {
		return false, true
	}
	if candidateHeight != nil {
		switch {
		case *candidateHeight == t.height:
			return true, false
		case *candidateHeight > t.height:
			return false, true
		default:
			return false, false
		}
	}
	if now.Sub(t.fetchedAt) < templateMaxAge() {
		return true, false
	}
	return false, true
}

// get returns a template usable for candidateHeight, refreshing it with one
// coalesced probe as identity when needed, or nil when none can be had.
func (c *coinbaseTemplateCache) get(candidateHeight *int, identity string) *coinbaseTemplate {
	c.mu.Lock()
	refreshed := false
	for {
		use, refresh := c.tmpl.decide(candidateHeight, time.Now())
		if use {
			t := c.tmpl
			c.mu.Unlock()
			return t
		}
		if !refresh || refreshed {
			c.mu.Unlock()
			return nil
		}
		if ch := c.inflight; ch != nil {
			c.mu.Unlock()
			<-ch
			c.mu.Lock()
			refreshed = true
			continue
		}
		if c.recentlyMissed(candidateHeight) {
			c.mu.Unlock()
			return nil
		}
		c.lastWanted = 0
		if candidateHeight != nil {
			c.lastWanted = *candidateHeight
		}

		ch := make(chan struct{})
		c.inflight = ch
		fetch := c.fetch
		c.mu.Unlock()

		t, err := func() (t *coinbaseTemplate, err error) {
			defer func() {
				if p := recover(); p != nil {
					err = fmt.Errorf("template probe panicked: %v", p)
				}
			}()
			return fetch(identity)
		}()

		c.mu.Lock()
		if err == nil && t != nil {
			c.tmpl = t
		} else {
			log.Printf("/coinbase: template probe failed, falling back to per-user probes: %v", err)
		}
		c.lastRefresh = time.Now()
		c.lastFailed = err != nil || t == nil
		c.inflight = nil
		close(ch)
		refreshed = true
	}
}

// templateProbeIdentity returns the canonical form of the configured pool
// address when it decodes as a CashAddr on this pool's network, else "".
func templateProbeIdentity(poolAddress string) string {
	if poolAddress == "" {
		return ""
	}
	canonical := canonicalAuthUser(poolAddress)
	prefix, _, _, err := cashaddr.Decode(canonical)
	if err != nil || prefix != cashaddrPrefix {
		return ""
	}
	return canonical
}

// fetchCoinbaseProof answers one /coinbase cache miss: spliced from the
// per-height template when the username is a CashAddr on this pool's network,
// a per-user Stratum probe otherwise (an undecodable or pool-fallback
// username, the wrong network, a stale candidate_height, or no template).
func fetchCoinbaseProof(username string, candidateHeight *int) coinbaseFetchResult {
	canonical := canonicalAuthUser(username)
	prefix, typ, hash, err := cashaddr.Decode(canonical)
	if err != nil || prefix != cashaddrPrefix {
		return fetchCoinbaseFromStratum(username)
	}
	script, err := cashaddr.OutputScript(typ, hash)
	if err != nil {
		return fetchCoinbaseFromStratum(username)
	}

	identity := coinbaseTemplateIdentity
	if identity == "" {
		identity = canonical
	}
	t := coinbaseTemplates.get(candidateHeight, identity)
	if t == nil || t.network != prefix {
		return fetchCoinbaseFromStratum(username)
	}
	return spliceCoinbaseResponse(username, t, script)
}

// spliceCoinbaseResponse builds the response a per-user probe would return
// for this template, through the same tail. Timestamp is the template's fetch
// time: like a probe's, it dates the job's bytes.
func spliceCoinbaseResponse(username string, t *coinbaseTemplate, script []byte) coinbaseFetchResult {
	response := CoinbaseResponse{
		Username:  username,
		Timestamp: t.fetchedAt.Unix(),
	}
	coinbaseHex := hex.EncodeToString(t.assemble(script))
	return finishCoinbaseResponse(response, coinbaseHex, hex.EncodeToString(t.coinb1), t.networkBits)
}
