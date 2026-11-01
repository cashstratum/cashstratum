package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"cashstratumapi/internal/poolevents"
)

// recordedRequest captures what the httptest server actually received, for
// tests that need to inspect headers/body independently of the Deliverer.
type recordedRequest struct {
	headers http.Header
	body    []byte
}

func recordingServer(t *testing.T, status int) (*httptest.Server, *[]recordedRequest) {
	t.Helper()
	var mu sync.Mutex
	var reqs []recordedRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		reqs = append(reqs, recordedRequest{headers: r.Header.Clone(), body: body})
		mu.Unlock()
		w.WriteHeader(status)
	}))
	return srv, &reqs
}

func snapshotRequests(reqs *[]recordedRequest) []recordedRequest {
	// Tests only read this after the relevant HTTP calls have already
	// completed (synchronously, from deliverWithRetry), so no lock is
	// needed at the read site.
	cp := make([]recordedRequest, len(*reqs))
	copy(cp, *reqs)
	return cp
}

func TestDeliver_SignatureVerifiesIndependently(t *testing.T) {
	srv, reqs := recordingServer(t, 200)
	defer srv.Close()

	secret := "test-secret"
	d := newDelivererWithClock(srv.URL, secret, false, nil, nil)
	evt := newNetworkBlockEvent("deadbeef")

	d.deliverWithRetry(context.Background(), evt)

	got := snapshotRequests(reqs)
	if len(got) != 1 {
		t.Fatalf("got %d requests, want 1", len(got))
	}
	r := got[0]

	timestamp := r.headers.Get("X-Blocksniper-Timestamp")
	sigHeader := r.headers.Get("X-Blocksniper-Signature")
	if timestamp == "" || sigHeader == "" {
		t.Fatalf("missing timestamp/signature headers: %+v", r.headers)
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(r.body)
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if sigHeader != want {
		t.Fatalf("X-Blocksniper-Signature = %q, want %q (independently computed)", sigHeader, want)
	}
}

func TestDeliver_BodyBytesSignedMatchBodyBytesReceived(t *testing.T) {
	srv, reqs := recordingServer(t, 200)
	defer srv.Close()

	d := newDelivererWithClock(srv.URL, "secret", false, nil, nil)
	evt := newNetworkBlockEvent("cafebabe")

	wantBody, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	d.deliverWithRetry(context.Background(), evt)

	got := snapshotRequests(reqs)
	if len(got) != 1 {
		t.Fatalf("got %d requests, want 1", len(got))
	}
	if string(got[0].body) != string(wantBody) {
		t.Fatalf("received body = %s, want %s", got[0].body, wantBody)
	}
}

func TestDeliver_AllSixHeadersPresentOnRealSend(t *testing.T) {
	srv, reqs := recordingServer(t, 204)
	defer srv.Close()

	d := newDelivererWithClock(srv.URL, "secret", false, nil, nil)
	evt := newNetworkBlockEvent("deadbeef")
	body, _ := json.Marshal(evt)
	var envFields envelopeFields
	json.Unmarshal(body, &envFields)

	d.deliverWithRetry(context.Background(), evt)

	got := snapshotRequests(reqs)
	if len(got) != 1 {
		t.Fatalf("got %d requests, want 1", len(got))
	}
	h := got[0].headers

	checks := map[string]string{
		"Content-Type":            "application/json; charset=utf-8",
		"User-Agent":              "cashstratum-notifier/1",
		"X-Blocksniper-Event":     envFields.Event,
		"X-Blocksniper-Delivery":  envFields.EventID,
		"X-CashStratum-Event":     envFields.Event,
		"X-CashStratum-Delivery":  envFields.EventID,
	}
	for name, want := range checks {
		if got := h.Get(name); got != want {
			t.Errorf("header %s = %q, want %q", name, got, want)
		}
	}
	if h.Get("X-Blocksniper-Timestamp") == "" {
		t.Errorf("X-Blocksniper-Timestamp missing")
	}
	if h.Get("X-CashStratum-Timestamp") == "" {
		t.Errorf("X-CashStratum-Timestamp missing")
	}
	sig := h.Get("X-Blocksniper-Signature")
	if len(sig) < len("sha256=")+1 || sig[:7] != "sha256=" {
		t.Errorf("X-Blocksniper-Signature = %q, want sha256=<hex>", sig)
	}
	sigCS := h.Get("X-CashStratum-Signature")
	if len(sigCS) < len("sha256=")+1 || sigCS[:7] != "sha256=" {
		t.Errorf("X-CashStratum-Signature = %q, want sha256=<hex>", sigCS)
	}
}

func TestDeliver_500RetriesThreeTimesThenDrops(t *testing.T) {
	var count atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	// Near-zero retry delays -- no real 1s/4s/12s sleeps in a test run.
	d := newDelivererWithClock(srv.URL, "secret", false, nil, []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond})
	evt := newNetworkBlockEvent("deadbeef")

	d.deliverWithRetry(context.Background(), evt)

	if got := count.Load(); got != 4 {
		t.Fatalf("server received %d requests, want 4 (1 initial + 3 retries)", got)
	}
	if got := d.EventsDropped(); got != 1 {
		t.Fatalf("EventsDropped() = %d, want 1", got)
	}
	if got := d.EventsSent(); got != 0 {
		t.Fatalf("EventsSent() = %d, want 0", got)
	}
}

func TestDeliver_401NoRetry(t *testing.T) {
	var count atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	d := newDelivererWithClock(srv.URL, "secret", false, nil, []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond})
	evt := newNetworkBlockEvent("deadbeef")

	d.deliverWithRetry(context.Background(), evt)

	if got := count.Load(); got != 1 {
		t.Fatalf("server received %d requests, want 1 (401 is terminal, never retried)", got)
	}
	if got := d.EventsSent(); got != 0 {
		t.Fatalf("EventsSent() = %d, want 0", got)
	}
}

func TestDeliver_EventIDAndTimestampIdenticalAcrossRetries(t *testing.T) {
	srv, reqs := recordingServer(t, 500)
	defer srv.Close()

	d := newDelivererWithClock(srv.URL, "secret", false, nil, []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond})
	evt := newNetworkBlockEvent("deadbeef")

	d.deliverWithRetry(context.Background(), evt)

	got := snapshotRequests(reqs)
	if len(got) != 4 {
		t.Fatalf("got %d requests, want 4", len(got))
	}

	firstDelivery := got[0].headers.Get("X-Blocksniper-Delivery")
	firstTimestamp := got[0].headers.Get("X-Blocksniper-Timestamp")
	if firstDelivery == "" || firstTimestamp == "" {
		t.Fatalf("first attempt missing delivery/timestamp headers")
	}
	for i, r := range got {
		if d := r.headers.Get("X-Blocksniper-Delivery"); d != firstDelivery {
			t.Errorf("attempt %d: X-Blocksniper-Delivery = %q, want %q", i+1, d, firstDelivery)
		}
		if ts := r.headers.Get("X-Blocksniper-Timestamp"); ts != firstTimestamp {
			t.Errorf("attempt %d: X-Blocksniper-Timestamp = %q, want %q", i+1, ts, firstTimestamp)
		}
	}
}

func TestDeliver_QueueDropsOldestWhenFull(t *testing.T) {
	d := newDelivererWithClock("http://127.0.0.1:0", "secret", true, nil, nil)

	const total = 65
	for i := 0; i < total; i++ {
		d.Enqueue(newNetworkBlockEvent("hash-" + strconv.Itoa(i)))
	}

	if got := d.queueLen(); got != deliverQueueCapacity {
		t.Fatalf("queueLen() = %d, want %d", got, deliverQueueCapacity)
	}
	if got := d.EventsDropped(); got != total-deliverQueueCapacity {
		t.Fatalf("EventsDropped() = %d, want %d", got, total-deliverQueueCapacity)
	}

	queued := d.queuedEvents()
	first := queued[0].(NetworkBlockEvent)
	last := queued[len(queued)-1].(NetworkBlockEvent)
	// The oldest (hash-0) must have been shed; the survivors are a
	// contiguous FIFO tail ending at the newest (hash-64).
	if first.Hash == "hash-0" {
		t.Fatalf("oldest event (hash-0) is still queued, want it dropped")
	}
	if last.Hash != "hash-64" {
		t.Fatalf("newest queued event Hash = %q, want hash-64", last.Hash)
	}
	if first.Hash != "hash-1" {
		t.Fatalf("oldest surviving event Hash = %q, want hash-1 (FIFO drop-oldest)", first.Hash)
	}
}

func TestDeliver_QueueEnqueueNeverBlocks(t *testing.T) {
	d := newDelivererWithClock("http://127.0.0.1:0", "secret", true, nil, nil)

	done := make(chan struct{})
	go func() {
		for i := 0; i < 200; i++ {
			d.Enqueue(newNetworkBlockEvent("hash-" + strconv.Itoa(i)))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Enqueue() blocked -- producer must never block even far past queue capacity")
	}
}

func TestDeliver_DryRunSendsNoHTTPRequests(t *testing.T) {
	srv, reqs := recordingServer(t, 200)
	defer srv.Close()

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	d := newDelivererWithClock(srv.URL, "test-secret", true, nil, nil)
	evt := newNetworkBlockEvent("deadbeef")

	d.deliverWithRetry(context.Background(), evt)

	if got := len(snapshotRequests(reqs)); got != 0 {
		t.Fatalf("dry-run made %d HTTP requests, want 0", got)
	}

	out := buf.String()
	if !strings.Contains(out, "WOULD POST") {
		t.Fatalf("log output missing \"WOULD POST\": %s", out)
	}
	if !strings.Contains(out, srv.URL) {
		t.Fatalf("log output missing the target URL: %s", out)
	}
	if !strings.Contains(out, "X-Blocksniper-Signature") {
		t.Fatalf("log output missing the signature header line: %s", out)
	}
	if strings.Contains(out, "test-secret") {
		t.Fatalf("log output leaked the raw secret: %s", out)
	}
	if !strings.Contains(out, evt.Hash) {
		t.Fatalf("log output missing the event body: %s", out)
	}
}

func TestDeliver_PersistedQueueSurvivesRestartAndDeliversWhenOnline(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "notifier.state.queue.json")

	var online atomic.Bool
	var mu sync.Mutex
	var delivered []recordedRequest
	attempt1Seen := make(chan struct{}, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !online.Load() {
			select {
			case attempt1Seen <- struct{}{}:
			default:
			}
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		mu.Lock()
		delivered = append(delivered, recordedRequest{headers: r.Header.Clone(), body: body})
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	// Phase 1: Target webhook is offline (503). Enqueue 2 events into d1,
	// let the first attempt fail and enter retry backoff, then cancel ctx1
	// to simulate a notifier restart during an outage.
	d1 := newDelivererWithClock(srv.URL, "persist-secret", false, nil, []time.Duration{500 * time.Millisecond, 500 * time.Millisecond})
	if err := d1.SetQueuePath(queuePath); err != nil {
		t.Fatalf("d1.SetQueuePath() error = %v", err)
	}

	evt1 := newNetworkBlockEvent("hash-persisted-1")
	rig := "rig1"
	evt2 := poolevents.NewPoolBlockEvent("solve-persisted-2", 900100, "addr.rig1", "addr", &rig)
	d1.Enqueue(evt1)
	d1.Enqueue(evt2)

	ctx1, cancel1 := context.WithCancel(context.Background())
	done1 := make(chan struct{})
	go func() {
		d1.Run(ctx1)
		close(done1)
	}()

	select {
	case <-attempt1Seen:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for d1 first delivery attempt")
	}

	cancel1()
	select {
	case <-done1:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for d1.Run to stop")
	}

	// Verify the queue file exists on disk with mode 0600, no .tmp file
	// remains, and both events (in-flight evt1 + queued evt2) are persisted.
	info, err := os.Stat(queuePath)
	if err != nil {
		t.Fatalf("os.Stat(%s) error = %v", queuePath, err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("queue file permissions = %04o, want 0600", perm)
	}
	if _, err := os.Stat(queuePath + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file %s.tmp should not exist after atomic rename", queuePath)
	}

	rawPersisted, err := os.ReadFile(queuePath)
	if err != nil {
		t.Fatalf("os.ReadFile(%s) error = %v", queuePath, err)
	}
	var persistedSlice []json.RawMessage
	if err := json.Unmarshal(rawPersisted, &persistedSlice); err != nil {
		t.Fatalf("json.Unmarshal(persisted) error = %v", err)
	}
	if len(persistedSlice) != 2 {
		t.Fatalf("persisted queue has %d items, want 2: %s", len(persistedSlice), rawPersisted)
	}

	// Phase 2: Target webhook comes back online. Create a new Deliverer d2
	// pointing at the same queuePath and verify both events are delivered
	// in FIFO order and the persisted queue file is emptied.
	online.Store(true)

	d2 := newDelivererWithClock(srv.URL, "persist-secret", false, nil, []time.Duration{5 * time.Millisecond})
	if err := d2.SetQueuePath(queuePath); err != nil {
		t.Fatalf("d2.SetQueuePath() error = %v", err)
	}
	if got := d2.queueLen(); got != 2 {
		t.Fatalf("d2.queueLen() after load = %d, want 2", got)
	}

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	done2 := make(chan struct{})
	go func() {
		d2.Run(ctx2)
		close(done2)
	}()

	deadline := time.Now().Add(2 * time.Second)
	for d2.EventsSent() < 2 {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for d2 to deliver 2 persisted events (sent=%d)", d2.EventsSent())
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel2()
	<-done2

	mu.Lock()
	gotReqs := append([]recordedRequest(nil), delivered...)
	mu.Unlock()

	if len(gotReqs) != 2 {
		t.Fatalf("webhook received %d online deliveries, want 2", len(gotReqs))
	}
	if got := gotReqs[0].headers.Get("X-CashStratum-Delivery"); got != evt1.EventID {
		t.Errorf("delivery[0] EventID = %q, want %q", got, evt1.EventID)
	}
	if got := gotReqs[1].headers.Get("X-CashStratum-Delivery"); got != evt2.EventID {
		t.Errorf("delivery[1] EventID = %q, want %q", got, evt2.EventID)
	}

	// Verify persisted queue file is now emptied ([]).
	afterBytes, err := os.ReadFile(queuePath)
	if err != nil {
		t.Fatalf("os.ReadFile(%s) after drain error = %v", queuePath, err)
	}
	var afterSlice []json.RawMessage
	if err := json.Unmarshal(afterBytes, &afterSlice); err != nil {
		t.Fatalf("json.Unmarshal(afterBytes) error = %v", err)
	}
	if len(afterSlice) != 0 {
		t.Fatalf("persisted queue after delivery has %d items, want 0 (%s)", len(afterSlice), afterBytes)
	}
}

func TestDeliver_PersistQueueRoundTripsAllEventTypes(t *testing.T) {
	dir := t.TempDir()
	queuePath := filepath.Join(dir, "queue.json")

	d1 := newDelivererWithClock("http://127.0.0.1:0", "secret", true, nil, nil)
	if err := d1.SetQueuePath(queuePath); err != nil {
		t.Fatalf("d1.SetQueuePath() error = %v", err)
	}

	rig := "worker1"
	reason := "high-hash"
	netEvt := newNetworkBlockEvent("000000000000000001")
	subEvt := poolevents.NewPoolBlockSubmittedEvent("solve-123")
	poolEvt := poolevents.NewPoolBlockEvent("solve-123", 888888, "user.worker1", "user", &rig)
	rejEvt := poolevents.NewPoolBlockRejectedEvent("solve-456", 888889, &reason)

	d1.Enqueue(netEvt)
	d1.Enqueue(subEvt)
	d1.Enqueue(poolEvt)
	d1.Enqueue(rejEvt)

	d2 := newDelivererWithClock("http://127.0.0.1:0", "secret", true, nil, nil)
	if err := d2.SetQueuePath(queuePath); err != nil {
		t.Fatalf("d2.SetQueuePath() error = %v", err)
	}

	queued := d2.queuedEvents()
	if len(queued) != 4 {
		t.Fatalf("len(queued) = %d, want 4", len(queued))
	}
	if got, ok := queued[0].(NetworkBlockEvent); !ok || got.Hash != netEvt.Hash || got.EventID != netEvt.EventID {
		t.Errorf("queued[0] = %#v, want NetworkBlockEvent %#v", queued[0], netEvt)
	}
	if got, ok := queued[1].(PoolBlockSubmittedEvent); !ok || got.SolveID != subEvt.SolveID {
		t.Errorf("queued[1] = %#v, want PoolBlockSubmittedEvent %#v", queued[1], subEvt)
	}
	if got, ok := queued[2].(PoolBlockEvent); !ok || got.Height != poolEvt.Height || got.Username != poolEvt.Username {
		t.Errorf("queued[2] = %#v, want PoolBlockEvent %#v", queued[2], poolEvt)
	}
	if got, ok := queued[3].(PoolBlockRejectedEvent); !ok || got.Height != rejEvt.Height || got.Reason == nil || *got.Reason != reason {
		t.Errorf("queued[3] = %#v, want PoolBlockRejectedEvent %#v", queued[3], rejEvt)
	}
	if d2.LastNetworkBlockAt() == nil {
		t.Errorf("d2.LastNetworkBlockAt() = nil after restoring NetworkBlockEvent")
	}
	if d2.LastPoolBlockAt() == nil {
		t.Errorf("d2.LastPoolBlockAt() = nil after restoring PoolBlockEvent")
	}
}

func TestLoadConfig_QueuePathFromEnvAndStateDirectory(t *testing.T) {
	t.Setenv("CASHSTRATUM_NOTIFY_QUEUE_PATH", "")
	t.Setenv("BLOCKSNIPER_NOTIFY_QUEUE_PATH", "")
	t.Setenv("NOTIFY_QUEUE_PATH", "")
	t.Setenv("STATE_DIRECTORY", "/var/lib/cashstratum-notifier")

	cfg := loadConfig()
	if want := filepath.Join("/var/lib/cashstratum-notifier", "queue.json"); cfg.queuePath != want {
		t.Fatalf("loadConfig().queuePath with STATE_DIRECTORY = %q, want %q", cfg.queuePath, want)
	}

	t.Setenv("CASHSTRATUM_NOTIFY_QUEUE_PATH", "/custom/path/queue.json")
	cfg = loadConfig()
	if cfg.queuePath != "/custom/path/queue.json" {
		t.Fatalf("loadConfig().queuePath with explicit env = %q, want /custom/path/queue.json", cfg.queuePath)
	}
}
