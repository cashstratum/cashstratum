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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
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
