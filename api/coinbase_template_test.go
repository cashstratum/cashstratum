package main

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cashstratumapi/internal/cashaddr"
)

// ---------------------------------------------------------------------------
// Fixtures and helpers
// ---------------------------------------------------------------------------

func readHexFixture(t *testing.T, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	b, err := hex.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		t.Fatalf("decode %s: %v", name, err)
	}
	return b
}

// splitOnChainCoinbase cuts a full coinbase transaction into the parts a
// Stratum job carries. coinb1 ends right after the extranonce push length,
// which follows the height, flags, time and nsec pushes.
func splitOnChainCoinbase(t *testing.T, tx []byte, en1Size int) (coinb1, en1, en2, coinb2 []byte) {
	t.Helper()
	_, pos, ok := readVarint(tx, 41)
	if !ok {
		t.Fatal("no scriptSig length")
	}
	for i := 0; i < 4; i++ { // height, flags, time, nsec
		n := int(tx[pos])
		if n > 0x4b {
			t.Fatalf("push %d is not a direct push (%#x)", i, n)
		}
		pos += 1 + n
	}
	enLen := int(tx[pos])
	pos++
	return tx[:pos], tx[pos : pos+en1Size], tx[pos+en1Size : pos+enLen], tx[pos+enLen:]
}

func jobFromParts(coinb1, en1 []byte, en2Size int, coinb2 []byte) stratumJob {
	return stratumJob{
		coinb1:      hex.EncodeToString(coinb1),
		coinb2:      hex.EncodeToString(coinb2),
		extranonce1: hex.EncodeToString(en1),
		en2Size:     en2Size,
		networkBits: "180204a1",
	}
}

// encodeTestCashAddr builds a CashAddr for any prefix and type (the
// fixturegen encoder only does bitcoincash P2PKH).
func encodeTestCashAddr(prefix string, typ byte, hash []byte) string {
	const charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"
	payload := append([]byte{typ}, hash...)
	var data []byte
	acc, bits := 0, 0
	for _, b := range payload {
		acc = acc<<8 | int(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			data = append(data, byte(acc>>bits)&0x1f)
		}
	}
	if bits > 0 {
		data = append(data, byte(acc<<(5-bits))&0x1f)
	}
	vals := make([]byte, 0, len(prefix)+1+len(data)+8)
	for i := 0; i < len(prefix); i++ {
		vals = append(vals, prefix[i]&0x1f)
	}
	vals = append(vals, 0)
	vals = append(vals, data...)
	vals = append(vals, make([]byte, 8)...)
	chk := testPolymod(vals)
	var sb strings.Builder
	sb.WriteString(prefix + ":")
	for _, d := range data {
		sb.WriteByte(charset[d])
	}
	for i := 7; i >= 0; i-- {
		sb.WriteByte(charset[(chk>>(5*uint(i)))&0x1f])
	}
	return sb.String()
}

func testPolymod(v []byte) uint64 {
	c := uint64(1)
	for _, d := range v {
		c0 := byte(c >> 35)
		c = ((c & 0x07ffffffff) << 5) ^ uint64(d)
		for i, g := range []uint64{0x98f2bc8e61, 0x79b76d99e2, 0xf33e5fb3c4, 0xae2eabe2a8, 0x1e4f43e470} {
			if c0&(1<<uint(i)) != 0 {
				c ^= g
			}
		}
	}
	return c ^ 1
}

func testHash(seed byte) []byte {
	h := make([]byte, 20)
	for i := range h {
		h[i] = seed ^ byte(i*7)
	}
	return h
}

func testAddr(seed byte) string {
	return encodeTestCashAddr("bitcoincash", cashaddr.TypeP2PKH, testHash(seed))
}

// evidence04Parts is the evidence 04 job (height 975663) split into its
// Stratum parts; the fake pool serves it.
func evidence04Parts(t *testing.T) (coinb1, en1 []byte, en2Size int, head, coinb3, minerScript []byte) {
	t.Helper()
	tx := readHexFixture(t, "coinbase-evidence04.hex")
	coinb1, en1, en2, coinb2 := splitOnChainCoinbase(t, tx, 4)
	head, minerScript, coinb3, err := splitCoinb2(coinb1, len(en1), len(en2), coinb2)
	if err != nil {
		t.Fatalf("split evidence 04: %v", err)
	}
	return coinb1, en1, len(en2), head, coinb3, minerScript
}

// ---------------------------------------------------------------------------
// Fake Stratum pool
// ---------------------------------------------------------------------------

type fakePoolMode int

const (
	// notifyBeforeAuth is btcsolo's real order: the user's job, then the
	// authorise reply, then silence until update_interval.
	notifyBeforeAuth fakePoolMode = iota
	// authBeforeNotify sends the reply first, then the job.
	authBeforeNotify
	// refuseAuth sends a job and then refuses the authorisation.
	refuseAuth
)

type fakePool struct {
	t      *testing.T
	ln     net.Listener
	mode   fakePoolMode
	conns  atomic.Int32
	height atomic.Int32

	coinb1Base []byte
	en1        []byte
	en2Size    int
	head       []byte
	coinb3     []byte
	poolScript []byte
	authDelay  time.Duration

	mu        sync.Mutex
	authUsers []string
}

func newFakePool(t *testing.T, mode fakePoolMode) *fakePool {
	t.Helper()
	coinb1, en1, en2Size, head, coinb3, _ := evidence04Parts(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	p := &fakePool{
		t: t, ln: ln, mode: mode,
		coinb1Base: coinb1, en1: en1, en2Size: en2Size, head: head, coinb3: coinb3,
		poolScript: append([]byte{0x76, 0xa9, 0x14}, append(testHash(0xee), 0x88, 0xac)...),
	}
	p.height.Store(975663)
	go p.serve()

	orig := stratumHost
	stratumHost = ln.Addr().String()
	t.Cleanup(func() {
		ln.Close()
		stratumHost = orig
	})
	return p
}

func (p *fakePool) serve() {
	for {
		conn, err := p.ln.Accept()
		if err != nil {
			return
		}
		p.conns.Add(1)
		go p.handle(conn)
	}
}

// coinb1 is the base coinb1 with the BIP34 height push rewritten.
func (p *fakePool) coinb1() []byte {
	c := append([]byte(nil), p.coinb1Base...)
	h := p.height.Load()
	c[43], c[44], c[45] = byte(h), byte(h>>8), byte(h>>16)
	return c
}

// coinb2For mirrors __generate_userwb: a decodable address is paid its own
// script, anything else falls back to the pool script.
func (p *fakePool) coinb2For(authUser string) []byte {
	token := authUser
	if i := strings.IndexAny(token, "._"); i >= 0 {
		token = token[:i]
	}
	script := p.poolScript
	if _, typ, hash, err := cashaddr.Decode(token); err == nil {
		script, _ = cashaddr.OutputScript(typ, hash)
	}
	out := append([]byte(nil), p.head...)
	out = append(out, byte(len(script)))
	out = append(out, script...)
	return append(out, p.coinb3...)
}

func (p *fakePool) handle(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	send := func(v any) {
		b, _ := json.Marshal(v)
		conn.Write(append(b, '\n'))
	}
	for scanner.Scan() {
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params []any  `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &req) != nil {
			continue
		}
		switch req.Method {
		case "mining.subscribe":
			send(map[string]any{"id": req.ID, "error": nil,
				"result": []any{[]any{[]any{"mining.notify", "1"}}, hex.EncodeToString(p.en1), p.en2Size}})
		case "mining.authorize":
			user, _ := req.Params[0].(string)
			p.mu.Lock()
			p.authUsers = append(p.authUsers, user)
			p.mu.Unlock()
			if p.authDelay > 0 {
				time.Sleep(p.authDelay)
			}
			notify := map[string]any{"id": nil, "method": "mining.notify", "params": []any{
				"6ae73fcb000007b9", strings.Repeat("00", 32),
				hex.EncodeToString(p.coinb1()), hex.EncodeToString(p.coinb2For(user)),
				[]any{}, "20000000", "180204a1", "6a00e8c4", true,
			}}
			reply := map[string]any{"id": req.ID, "result": p.mode != refuseAuth, "error": nil}
			send(map[string]any{"id": nil, "method": "mining.set_difficulty", "params": []any{42}})
			if p.mode == authBeforeNotify {
				send(reply)
				send(notify)
			} else {
				send(notify)
				send(reply)
			}
			// Then silence, as an idle pool until its next broadcast.
		}
	}
}

func (p *fakePool) users() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.authUsers...)
}

// useIsolatedTemplateState isolates every piece of /coinbase global state a
// template test touches.
func useIsolatedTemplateState(t *testing.T) {
	t.Helper()
	useIsolatedCoinbaseState(t)
	origTemplates, origIdentity, origPrefix := coinbaseTemplates, coinbaseTemplateIdentity, cashaddrPrefix
	coinbaseTemplates = newCoinbaseTemplateCache()
	coinbaseTemplateIdentity = ""
	cashaddrPrefix = "bitcoincash"
	t.Cleanup(func() {
		coinbaseTemplates, coinbaseTemplateIdentity, cashaddrPrefix = origTemplates, origIdentity, origPrefix
	})
}

func getCoinbase(t *testing.T, user string, height *int) (int, CoinbaseResponse) {
	t.Helper()
	url := "/coinbase?user=" + user
	if height != nil {
		url += fmt.Sprintf("&candidate_height=%d", *height)
	}
	rec := httptest.NewRecorder()
	handleCoinbase(rec, httptest.NewRequest(http.MethodGet, url, nil))
	var resp CoinbaseResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (%s)", err, rec.Body.String())
	}
	return rec.Code, resp
}

// ---------------------------------------------------------------------------
// I-03 — the probe returns the job it already holds
// ---------------------------------------------------------------------------

func TestCoinbaseProbeUsesJobHeldBeforeAuthReply(t *testing.T) {
	useIsolatedTemplateState(t)
	pool := newFakePool(t, notifyBeforeAuth)
	addr := testAddr(0x11)

	start := time.Now()
	result := fetchCoinbaseFromStratum(addr)
	elapsed := time.Since(start)

	if elapsed >= time.Second {
		t.Fatalf("probe took %s; the held job must be used at the auth reply", elapsed)
	}
	if result.status != http.StatusOK || result.response.Error != "" {
		t.Fatalf("status %d error %q", result.status, result.response.Error)
	}
	if got := *result.response.BlockHeight; got != 975663 {
		t.Errorf("height = %d", got)
	}
	if len(result.response.Outputs) != 2 {
		t.Errorf("outputs = %d, want 2", len(result.response.Outputs))
	}
	if pool.conns.Load() != 1 {
		t.Errorf("connections = %d", pool.conns.Load())
	}
}

func TestCoinbaseProbeUsesFirstJobAfterAuthReply(t *testing.T) {
	useIsolatedTemplateState(t)
	newFakePool(t, authBeforeNotify)

	start := time.Now()
	result := fetchCoinbaseFromStratum(testAddr(0x12))
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Fatalf("probe took %s", elapsed)
	}
	if result.response.Error != "" || result.response.CoinbaseHex == "" {
		t.Fatalf("error %q", result.response.Error)
	}
}

func TestCoinbaseProbeRefusedAuthNeverReturnsHeldJob(t *testing.T) {
	useIsolatedTemplateState(t)
	newFakePool(t, refuseAuth)
	addr := testAddr(0x13)

	start := time.Now()
	result := fetchCoinbaseFromStratum(addr)
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Fatalf("refusal took %s", elapsed)
	}
	if result.status != http.StatusOK {
		t.Errorf("status = %d, want 200 as before", result.status)
	}
	want := "Pool did not authorise " + addr + "." + ProbeWorkerSuffix
	if !strings.HasPrefix(result.response.Error, want) {
		t.Errorf("error = %q, want prefix %q", result.response.Error, want)
	}
	if result.response.CoinbaseHex != "" || result.response.Outputs != nil {
		t.Errorf("refused auth returned a proof: %+v", result.response)
	}
}

// ---------------------------------------------------------------------------
// I-04 — structural split
// ---------------------------------------------------------------------------

func TestCoinbaseTemplateEvidence04RebuildsExactly(t *testing.T) {
	tx := readHexFixture(t, "coinbase-evidence04.hex")
	coinb1, en1, en2, coinb2 := splitOnChainCoinbase(t, tx, 4)
	if hex.EncodeToString(en1) != "4042e76a" || hex.EncodeToString(en2) != "0000000000000000" {
		t.Fatalf("en1 %x en2 %x", en1, en2)
	}

	tmpl, err := newCoinbaseTemplate(jobFromParts(coinb1, en1, len(en2), coinb2), "bitcoincash", time.Now())
	if err != nil {
		t.Fatalf("template: %v", err)
	}
	if tmpl.height != 975663 {
		t.Errorf("height = %d", tmpl.height)
	}

	h160, _ := hex.DecodeString("26ec87c179dea02b35d86d9167dd4e46514a3f38")
	script, _ := cashaddr.OutputScript(cashaddr.TypeP2PKH, h160)
	rebuilt := tmpl.assemble(script)
	if !bytes.Equal(rebuilt, tx) {
		t.Fatalf("rebuilt\n%x\nwant\n%x", rebuilt, tx)
	}

	// Another hash160 changes exactly those 20 bytes.
	other := testHash(0x5a)
	otherScript, _ := cashaddr.OutputScript(cashaddr.TypeP2PKH, other)
	swapped := tmpl.assemble(otherScript)
	if len(swapped) != len(tx) {
		t.Fatalf("length changed: %d vs %d", len(swapped), len(tx))
	}
	hashAt := bytes.Index(tx, h160)
	var diffs []int
	for i := range tx {
		if tx[i] != swapped[i] {
			diffs = append(diffs, i)
		}
	}
	for _, i := range diffs {
		if i < hashAt || i >= hashAt+20 {
			t.Errorf("byte %d changed outside the hash160 at %d..%d", i, hashAt, hashAt+19)
		}
	}
	if !bytes.Equal(swapped[hashAt:hashAt+20], other) {
		t.Errorf("hash160 not swapped in")
	}
}

func TestCoinbaseTemplateOnChain975630RebuildsExactly(t *testing.T) {
	tx := readHexFixture(t, "coinbase-975630.hex")
	if !bytes.Contains(tx, []byte("/BlockSniper.ai/")) {
		t.Fatal("fixture is not a block from this pool")
	}
	coinb1, en1, en2, coinb2 := splitOnChainCoinbase(t, tx, 4)
	if len(en2) != 8 {
		t.Fatalf("en2 size = %d", len(en2))
	}

	head, finderScript, coinb3, err := splitCoinb2(coinb1, len(en1), len(en2), coinb2)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	wantScript, _ := hex.DecodeString("76a9145bd09fcaf09bcfb7e825bc62d486054b8e57fefe88ac")
	if !bytes.Equal(finderScript, wantScript) {
		t.Fatalf("finder script %x", finderScript)
	}
	tmpl, err := newCoinbaseTemplate(jobFromParts(coinb1, en1, len(en2), coinb2), "bitcoincash", time.Now())
	if err != nil {
		t.Fatalf("template: %v", err)
	}
	if tmpl.height != 975630 {
		t.Errorf("height = %d", tmpl.height)
	}
	if !bytes.Equal(tmpl.coinb2Head, head) || !bytes.Equal(tmpl.coinb3, coinb3) {
		t.Fatal("template parts differ from the split")
	}

	// The finder's script, reached through its CashAddr like a request would.
	addr := encodeTestCashAddr("bitcoincash", cashaddr.TypeP2PKH, wantScript[3:23])
	_, typ, hash, err := cashaddr.Decode(addr)
	if err != nil {
		t.Fatalf("decode finder address: %v", err)
	}
	script, _ := cashaddr.OutputScript(typ, hash)

	var rebuilt []byte
	rebuilt = append(rebuilt, tmpl.coinb1...)
	rebuilt = append(rebuilt, en1...)
	rebuilt = append(rebuilt, en2...) // the finder's own extranonce2
	rebuilt = append(rebuilt, tmpl.coinb2Head...)
	rebuilt = append(rebuilt, byte(len(script)))
	rebuilt = append(rebuilt, script...)
	rebuilt = append(rebuilt, tmpl.coinb3...)
	if !bytes.Equal(rebuilt, tx) {
		t.Fatalf("rebuilt\n%x\nwant\n%x", rebuilt, tx)
	}
}

func TestSplitCoinb2DustSingleOutput(t *testing.T) {
	coinb1, en1, en2Size, _, _, _ := evidence04Parts(t)
	sig := append([]byte{37}, []byte("/BlockSniper.ai/ AI Smart Solo Mining")...)
	head := append(append([]byte(nil), sig...), 0xff, 0xff, 0xff, 0xff, 0x01)
	head = append(head, 0x9b, 0xa2, 0x43, 0x12, 0, 0, 0, 0) // full coinbasevalue
	script, _ := cashaddr.OutputScript(cashaddr.TypeP2SH, testHash(0x21))
	coinb2 := append(append(append(append([]byte(nil), head...), byte(len(script))), script...), 0, 0, 0, 0)

	gotHead, gotScript, coinb3, err := splitCoinb2(coinb1, len(en1), en2Size, coinb2)
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	if !bytes.Equal(gotHead, head) || !bytes.Equal(gotScript, script) || !bytes.Equal(coinb3, []byte{0, 0, 0, 0}) {
		t.Fatalf("head %x script %x coinb3 %x", gotHead, gotScript, coinb3)
	}

	tmpl, err := newCoinbaseTemplate(jobFromParts(coinb1, en1, en2Size, coinb2), "bitcoincash", time.Now())
	if err != nil {
		t.Fatalf("template: %v", err)
	}
	outputs, total := parseCoinbaseOutputs(hex.EncodeToString(tmpl.assemble(script)))
	if len(outputs) != 1 || total != 0x1243a29b {
		t.Errorf("outputs %d total %d", len(outputs), total)
	}
}

func TestSplitCoinb2RejectsMalformed(t *testing.T) {
	tx := readHexFixture(t, "coinbase-evidence04.hex")
	coinb1, en1, en2, coinb2 := splitOnChainCoinbase(t, tx, 4)
	cases := map[string][]byte{
		"truncated locktime": coinb2[:len(coinb2)-1],
		"extra tail bytes":   append(append([]byte(nil), coinb2...), 0),
		"no sequence":        append(append([]byte(nil), coinb2[:38]...), coinb2[39:]...),
	}
	for name, c := range cases {
		if _, _, _, err := splitCoinb2(coinb1, len(en1), len(en2), c); err == nil {
			t.Errorf("%s: split accepted it", name)
		}
	}
}

// ---------------------------------------------------------------------------
// I-05 — /coinbase from the per-height template
// ---------------------------------------------------------------------------

func TestCoinbaseSplicedEqualsProbe(t *testing.T) {
	useIsolatedTemplateState(t)
	newFakePool(t, notifyBeforeAuth)
	height := 975663

	for _, addr := range []string{
		testAddr(0x31),
		encodeTestCashAddr("bitcoincash", cashaddr.TypeP2SH, testHash(0x32)),
	} {
		probe := fetchCoinbaseFromStratum(addr)
		spliced := fetchCoinbaseProof(addr, &height)
		if probe.response.Error != "" || spliced.response.Error != "" {
			t.Fatalf("errors: probe %q spliced %q", probe.response.Error, spliced.response.Error)
		}
		// Timestamp dates each answer's job fetch; both ran within this test.
		if d := probe.response.Timestamp - spliced.response.Timestamp; d < -1 || d > 1 {
			t.Errorf("timestamps %d vs %d", probe.response.Timestamp, spliced.response.Timestamp)
		}
		spliced.response.Timestamp = probe.response.Timestamp
		if !reflect.DeepEqual(probe, spliced) {
			t.Errorf("%s:\nprobe   %+v\nspliced %+v", addr, probe, spliced)
		}
	}
}

func TestCoinbaseTemplateProbesAsPoolAddress(t *testing.T) {
	useIsolatedTemplateState(t)
	pool := newFakePool(t, notifyBeforeAuth)
	poolAddr := testAddr(0xee)
	coinbaseTemplateIdentity = templateProbeIdentity(strings.TrimPrefix(poolAddr, "bitcoincash:"))
	if coinbaseTemplateIdentity != poolAddr {
		t.Fatalf("identity = %q, want %q", coinbaseTemplateIdentity, poolAddr)
	}

	height := 975663
	code, resp := getCoinbase(t, testAddr(0x41), &height)
	if code != http.StatusOK || resp.Error != "" {
		t.Fatalf("status %d error %q", code, resp.Error)
	}
	want := []string{poolAddr + "." + ProbeWorkerSuffix}
	if got := pool.users(); !reflect.DeepEqual(got, want) {
		t.Errorf("authorised %v, want %v", got, want)
	}
	// The requester's own script still sits in output 0.
	_, _, hash, _ := cashaddr.Decode(testAddr(0x41))
	if resp.Outputs[0].Address != encodeLegacyAddress(hash) {
		t.Errorf("output 0 %+v does not pay the requester", resp.Outputs[0])
	}
}

func TestTemplateProbeIdentity(t *testing.T) {
	useIsolatedTemplateState(t)
	addr := testAddr(0x51)
	cases := map[string]string{
		"":                           "",
		addr:                         addr,
		strings.ToUpper(addr):        addr,
		"1BvBMSEYstWetqTFn5Au4m4GFg": "",
		encodeTestCashAddr("bchreg", 0, testHash(1)): "",
	}
	for in, want := range cases {
		if got := templateProbeIdentity(in); got != want {
			t.Errorf("templateProbeIdentity(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCoinbaseNewHeightConcurrentAddressesOneProbe(t *testing.T) {
	useIsolatedTemplateState(t)
	pool := newFakePool(t, notifyBeforeAuth)
	pool.authDelay = 50 * time.Millisecond
	height := 975663
	if code, resp := getCoinbase(t, testAddr(0x60), &height); code != http.StatusOK || resp.Error != "" {
		t.Fatalf("warm-up: %d %q", code, resp.Error)
	}

	pool.height.Store(975664)
	pool.conns.Store(0)
	next := 975664
	const n = 16
	var wg sync.WaitGroup
	errs := make(chan string, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(seed byte) {
			defer wg.Done()
			code, resp := getCoinbase(t, testAddr(seed), &next)
			if code != http.StatusOK || resp.Error != "" || resp.BlockHeight == nil || *resp.BlockHeight != next {
				errs <- fmt.Sprintf("%d %q %v", code, resp.Error, resp.BlockHeight)
			}
		}(byte(0x70 + i))
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		t.Error(e)
	}
	if got := pool.conns.Load(); got != 1 {
		t.Errorf("connections for %d addresses at a new height = %d, want 1", n, got)
	}
}

func TestCoinbaseUnseenAddressAtCachedHeightNoProbe(t *testing.T) {
	useIsolatedTemplateState(t)
	pool := newFakePool(t, notifyBeforeAuth)
	height := 975663
	getCoinbase(t, testAddr(0x80), &height)
	pool.conns.Store(0)

	start := time.Now()
	unseen := testAddr(0x81)
	code, resp := getCoinbase(t, unseen, &height)
	if code != http.StatusOK || resp.Error != "" {
		t.Fatalf("status %d error %q", code, resp.Error)
	}
	if got := pool.conns.Load(); got != 0 {
		t.Errorf("connections = %d, want 0", got)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("took %s", elapsed)
	}
	for _, u := range pool.users() {
		if strings.HasPrefix(u, unseen) {
			t.Errorf("pool saw the unseen address: %s", u)
		}
	}
	// Bare and worker-suffixed spellings splice too.
	_, _, hash, _ := cashaddr.Decode(unseen)
	for _, u := range []string{strings.TrimPrefix(unseen, "bitcoincash:"), unseen + ".rig1", unseen + "_rig2"} {
		if code, resp := getCoinbase(t, u, &height); code != http.StatusOK || resp.Outputs[0].Address != encodeLegacyAddress(hash) {
			t.Errorf("%s: %d %+v", u, code, resp)
		}
	}
	if got := pool.conns.Load(); got != 0 {
		t.Errorf("connections after spellings = %d, want 0", got)
	}
}

func TestCoinbaseFallsBackToProbe(t *testing.T) {
	useIsolatedTemplateState(t)
	pool := newFakePool(t, notifyBeforeAuth)
	height := 975663
	getCoinbase(t, testAddr(0x90), &height) // template cached at 975663
	pool.conns.Store(0)

	cases := []struct {
		name, user string
		height     *int
	}{
		{"undecodable username", "plainminer", &height},
		{"bad checksum", testAddr(0x91)[:len(testAddr(0x91))-1] + "q", &height},
		{"wrong network", encodeTestCashAddr("bchreg", 0, testHash(0x92)), &height},
	}
	for i, c := range cases {
		code, resp := getCoinbase(t, c.user, c.height)
		if code != http.StatusOK || resp.Error != "" {
			t.Errorf("%s: %d %q", c.name, code, resp.Error)
		}
		if got := pool.conns.Load(); got != int32(i+1) {
			t.Errorf("%s: connections = %d, want a per-user probe", c.name, got)
		}
		users := pool.users()
		if last := users[len(users)-1]; !strings.HasPrefix(last, canonicalAuthUser(c.user)+".") {
			t.Errorf("%s: probe authorised %q", c.name, last)
		}
	}
}

func TestCoinbaseStaleCandidateHeightKeepsProbeBehaviour(t *testing.T) {
	useIsolatedTemplateState(t)
	pool := newFakePool(t, notifyBeforeAuth)
	height := 975663
	getCoinbase(t, testAddr(0xa0), &height)
	pool.conns.Store(0)

	stale := height - 1
	code, resp := getCoinbase(t, testAddr(0xa1), &stale)
	if code != http.StatusOK || resp.Error != "" {
		t.Fatalf("status %d error %q", code, resp.Error)
	}
	// As before: a per-user probe, answering with the pool's current height.
	if got := pool.conns.Load(); got != 1 {
		t.Errorf("connections = %d, want 1 per-user probe", got)
	}
	if *resp.BlockHeight != height {
		t.Errorf("height = %d, want the pool's %d", *resp.BlockHeight, height)
	}
}

func TestCoinbaseLegacyCallRefreshesAfterUpdateInterval(t *testing.T) {
	useIsolatedTemplateState(t)
	pool := newFakePool(t, notifyBeforeAuth)

	getCoinbase(t, testAddr(0xb0), nil)
	getCoinbase(t, testAddr(0xb1), nil)
	if got := pool.conns.Load(); got != 1 {
		t.Fatalf("fresh template: connections = %d, want 1", got)
	}

	coinbaseTemplates.mu.Lock()
	coinbaseTemplates.tmpl.fetchedAt = time.Now().Add(-templateMaxAge())
	coinbaseTemplates.lastRefresh = time.Time{}
	coinbaseTemplates.mu.Unlock()

	pool.height.Store(975664)
	_, resp := getCoinbase(t, testAddr(0xb2), nil)
	if got := pool.conns.Load(); got != 2 {
		t.Errorf("aged template: connections = %d, want 2", got)
	}
	if *resp.BlockHeight != 975664 {
		t.Errorf("height = %d", *resp.BlockHeight)
	}
}

func TestCoinbaseNoTemplateFallsBackToProbe(t *testing.T) {
	useIsolatedTemplateState(t)
	newFakePool(t, notifyBeforeAuth)
	coinbaseTemplates.fetch = func(string) (*coinbaseTemplate, error) {
		return nil, fmt.Errorf("pool unreachable")
	}
	height := 975663
	code, resp := getCoinbase(t, testAddr(0xc0), &height)
	if code != http.StatusOK || resp.Error != "" || *resp.BlockHeight != height {
		t.Fatalf("status %d error %q", code, resp.Error)
	}
}
