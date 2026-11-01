// The delivery pipeline: a bounded, drop-oldest queue feeding a single
// sender goroutine that HMAC-signs and POSTs each event to Laravel's
// webhook per the integration contract (§1 transport, §2 signing, §6
// response handling). Producers (zmqsub, tail, heartbeat) call Enqueue and
// never block; the network/retry machinery all lives here.
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
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// deliverQueueCapacity is the bounded queue's size (decision N-8). Enqueue
// past this drops the OLDEST queued event to make room -- a producer must
// never block, and a stale event is worth less than a fresh one once the
// pipe is badly backed up.
const deliverQueueCapacity = 64

const (
	// deliverConnectTimeout and deliverTotalTimeout implement contract §1's
	// "3s connect + 5s total per attempt": the Dialer bounds how long TCP
	// connect may take, the client's own Timeout bounds the whole
	// round trip (connect + TLS + write + read).
	deliverConnectTimeout = 3 * time.Second
	deliverTotalTimeout   = 5 * time.Second
)

// deliverRetryDelays is the retry ladder from contract §1: 3 retries after
// the first attempt, at 1s/4s/12s, then the event is dropped and logged.
var deliverRetryDelays = []time.Duration{1 * time.Second, 4 * time.Second, 12 * time.Second}

// envelopeFields is the sliver of a marshalled event body this package
// needs for the X-Blocksniper-Event/X-Blocksniper-Delivery headers.
// Reading it back out of the already-marshalled JSON (rather than adding an
// accessor method to every event type) guarantees the headers can never
// drift from what the body actually says, by construction.
type envelopeFields struct {
	Event   string `json:"event"`
	EventID string `json:"event_id"`
}

func extractEnvelopeFields(body []byte) (envelopeFields, error) {
	var f envelopeFields
	err := json.Unmarshal(body, &f)
	return f, err
}

// Deliverer owns the bounded queue, HMAC signing, and the retry loop.
// Enqueue is safe for concurrent producers; exactly one goroutine (started
// via Run) drains the queue and does all networking.
type Deliverer struct {
	url    string
	secret string
	dryRun bool

	client *http.Client

	now         func() time.Time
	retryDelays []time.Duration

	mu        sync.Mutex
	queue     []any
	inflight  any
	queuePath string

	notify chan struct{}

	eventsSent    atomic.Int64
	eventsDropped atomic.Int64

	tsMu               sync.Mutex
	lastNetworkBlockAt *time.Time
	lastPoolBlockAt    *time.Time
}

// NewDeliverer builds a production Deliverer: real clock, real retry
// ladder, an http.Client matching contract §1's timeouts.
func NewDeliverer(url, secret string, dryRun bool) *Deliverer {
	return newDelivererWithClock(url, secret, dryRun, time.Now, deliverRetryDelays)
}

// newDelivererWithClock is the flexible constructor tests use to inject a
// fake clock and near-zero retry delays (no real 17s sleeps in a test run).
func newDelivererWithClock(url, secret string, dryRun bool, now func() time.Time, retryDelays []time.Duration) *Deliverer {
	if now == nil {
		now = time.Now
	}
	return &Deliverer{
		url:    url,
		secret: secret,
		dryRun: dryRun,
		client: &http.Client{
			Timeout: deliverTotalTimeout,
			Transport: &http.Transport{
				DialContext: (&net.Dialer{Timeout: deliverConnectTimeout}).DialContext,
			},
		},
		now:         now,
		retryDelays: retryDelays,
		notify:      make(chan struct{}, 1),
	}
}

// EventsSent/EventsDropped/LastNetworkBlockAt/LastPoolBlockAt are read by
// the heartbeat sink (contract §4.5's events_sent/events_dropped/
// last_network_block_at/last_pool_block_at).
func (d *Deliverer) EventsSent() int64    { return d.eventsSent.Load() }
func (d *Deliverer) EventsDropped() int64 { return d.eventsDropped.Load() }

func (d *Deliverer) LastNetworkBlockAt() *time.Time {
	d.tsMu.Lock()
	defer d.tsMu.Unlock()
	return d.lastNetworkBlockAt
}

func (d *Deliverer) LastPoolBlockAt() *time.Time {
	d.tsMu.Lock()
	defer d.tsMu.Unlock()
	return d.lastPoolBlockAt
}

// SetQueuePath configures an optional on-disk JSON file to persist pending
// deliveries across process restarts, and loads any events already stored at
// path. Writes use atomic write-temp-and-rename (path+".tmp" -> path, 0600).
func (d *Deliverer) SetQueuePath(path string) error {
	d.mu.Lock()
	d.queuePath = path
	if path == "" {
		d.mu.Unlock()
		return nil
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		_ = os.MkdirAll(dir, 0o750)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if len(d.queue) > 0 || d.inflight != nil {
				d.persistLocked()
			}
			d.mu.Unlock()
			return nil
		}
		d.mu.Unlock()
		log.Printf("deliver: failed to read persisted queue from %s: %v", path, err)
		return err
	}

	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		if len(d.queue) > 0 || d.inflight != nil {
			d.persistLocked()
		}
		d.mu.Unlock()
		return nil
	}

	var rawItems []json.RawMessage
	if err := json.Unmarshal(trimmed, &rawItems); err != nil {
		d.mu.Unlock()
		log.Printf("deliver: failed to parse persisted queue from %s: %v", path, err)
		return err
	}

	var loaded []any
	for _, raw := range rawItems {
		evt, err := unmarshalQueuedEvent(raw)
		if err != nil {
			log.Printf("deliver: skipping unreadable persisted event in %s: %v", path, err)
			continue
		}
		d.restoreTimestamps(evt)
		loaded = append(loaded, evt)
	}

	if len(loaded) > 0 {
		merged := append(loaded, d.queue...)
		if len(merged) > deliverQueueCapacity {
			droppedCount := len(merged) - deliverQueueCapacity
			merged = merged[droppedCount:]
			d.eventsDropped.Add(int64(droppedCount))
		}
		d.queue = merged
		d.persistLocked()
	} else if len(d.queue) > 0 || d.inflight != nil {
		d.persistLocked()
	}
	hasItems := len(d.queue) > 0
	d.mu.Unlock()

	if hasItems {
		select {
		case d.notify <- struct{}{}:
		default:
		}
	}
	return nil
}

// unmarshalQueuedEvent decodes a persisted JSON event back into its concrete
// event struct (preserving type assertions and timestamp tracking) or falls
// back to json.RawMessage for unknown event types.
func unmarshalQueuedEvent(raw json.RawMessage) (any, error) {
	fields, err := extractEnvelopeFields(raw)
	if err != nil {
		return nil, err
	}
	switch fields.Event {
	case "network.block":
		var evt NetworkBlockEvent
		if err := json.Unmarshal(raw, &evt); err != nil {
			return nil, err
		}
		return evt, nil
	case "pool.block.submitted":
		var evt PoolBlockSubmittedEvent
		if err := json.Unmarshal(raw, &evt); err != nil {
			return nil, err
		}
		return evt, nil
	case "pool.block":
		var evt PoolBlockEvent
		if err := json.Unmarshal(raw, &evt); err != nil {
			return nil, err
		}
		return evt, nil
	case "pool.block.rejected":
		var evt PoolBlockRejectedEvent
		if err := json.Unmarshal(raw, &evt); err != nil {
			return nil, err
		}
		return evt, nil
	case "notifier.heartbeat":
		var evt HeartbeatEvent
		if err := json.Unmarshal(raw, &evt); err != nil {
			return nil, err
		}
		return evt, nil
	default:
		cp := make(json.RawMessage, len(raw))
		copy(cp, raw)
		return cp, nil
	}
}

// persistLocked writes the current in-flight + queued events to d.queuePath
// using atomic write-temp-and-rename (0600). Caller must hold d.mu.
func (d *Deliverer) persistLocked() {
	if d.queuePath == "" {
		return
	}
	capHint := len(d.queue)
	if d.inflight != nil {
		capHint++
	}
	items := make([]any, 0, capHint)
	if d.inflight != nil {
		items = append(items, d.inflight)
	}
	items = append(items, d.queue...)

	data, err := json.Marshal(items)
	if err != nil {
		log.Printf("deliver: failed to marshal queue for %s: %v", d.queuePath, err)
		return
	}
	tmpPath := d.queuePath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		log.Printf("deliver: failed to write temp queue file %s: %v", tmpPath, err)
		return
	}
	if err := os.Rename(tmpPath, d.queuePath); err != nil {
		log.Printf("deliver: failed to rename temp queue file %s -> %s: %v", tmpPath, d.queuePath, err)
	}
}

// Enqueue hands event to the delivery queue. Never blocks: when the queue
// is already at deliverQueueCapacity, the oldest queued event is dropped
// (incrementing eventsDropped) to make room for this one.
func (d *Deliverer) Enqueue(event any) {
	d.trackTimestamps(event)

	d.mu.Lock()
	if len(d.queue) >= deliverQueueCapacity {
		dropped := d.queue[0]
		d.queue = d.queue[1:]
		d.eventsDropped.Add(1)
		log.Printf("deliver: queue full (%d), dropping oldest queued event (%s)", deliverQueueCapacity, eventLabel(dropped))
	}
	d.queue = append(d.queue, event)
	d.persistLocked()
	d.mu.Unlock()

	select {
	case d.notify <- struct{}{}:
	default:
	}
}

// queueLen and queuedEvents are test-only introspection helpers (same
// package, no need to export).
func (d *Deliverer) queueLen() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.queue)
}

func (d *Deliverer) queuedEvents() []any {
	d.mu.Lock()
	defer d.mu.Unlock()
	cp := make([]any, len(d.queue))
	copy(cp, d.queue)
	return cp
}

func (d *Deliverer) dequeue() (any, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.queue) == 0 {
		return nil, false
	}
	item := d.queue[0]
	d.queue = d.queue[1:]
	d.inflight = item
	return item, true
}

func (d *Deliverer) finishInflight(completed bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !completed && d.inflight != nil {
		requeued := make([]any, 0, 1+len(d.queue))
		requeued = append(requeued, d.inflight)
		requeued = append(requeued, d.queue...)
		if len(requeued) > deliverQueueCapacity {
			requeued = requeued[:deliverQueueCapacity]
		}
		d.queue = requeued
	}
	d.inflight = nil
	d.persistLocked()
}

// trackTimestamps updates lastNetworkBlockAt/lastPoolBlockAt at the moment
// an event enters the pipeline -- "observed", matching the same notion
// used for Envelope.EmittedAt -- rather than at eventual (and possibly
// failed) delivery time.
func (d *Deliverer) trackTimestamps(event any) {
	switch event.(type) {
	case NetworkBlockEvent:
		t := d.now()
		d.tsMu.Lock()
		d.lastNetworkBlockAt = &t
		d.tsMu.Unlock()
	case PoolBlockEvent:
		t := d.now()
		d.tsMu.Lock()
		d.lastPoolBlockAt = &t
		d.tsMu.Unlock()
	}
}

func (d *Deliverer) restoreTimestamps(event any) {
	switch evt := event.(type) {
	case NetworkBlockEvent:
		if t, err := time.Parse(time.RFC3339Nano, evt.EmittedAt); err == nil {
			d.tsMu.Lock()
			if d.lastNetworkBlockAt == nil || t.After(*d.lastNetworkBlockAt) {
				d.lastNetworkBlockAt = &t
			}
			d.tsMu.Unlock()
		}
	case PoolBlockEvent:
		if t, err := time.Parse(time.RFC3339Nano, evt.EmittedAt); err == nil {
			d.tsMu.Lock()
			if d.lastPoolBlockAt == nil || t.After(*d.lastPoolBlockAt) {
				d.lastPoolBlockAt = &t
			}
			d.tsMu.Unlock()
		}
	}
}

// Run drains the queue until ctx is cancelled. Exactly one goroutine should
// call this -- delivery order and the retry ladder both assume a single
// in-flight send at a time.
func (d *Deliverer) Run(ctx context.Context) {
	for {
		item, ok := d.dequeue()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-d.notify:
				continue
			}
		}
		completed := d.deliverWithRetry(ctx, item)
		d.finishInflight(completed)
		if ctx.Err() != nil {
			return
		}
	}
}

// deliverWithRetry marshals event once, signs it once (timestamp fixed at
// first-attempt time per contract §1: "does not change on retry"), and
// then attempts delivery per the retry ladder. In dry-run mode it logs the
// would-be request and returns without any network I/O. Returns false only
// when delivery was aborted by ctx cancellation before reaching a terminal
// outcome, allowing Run to keep the event in the persisted queue.
func (d *Deliverer) deliverWithRetry(ctx context.Context, event any) bool {
	body, err := json.Marshal(event)
	if err != nil {
		log.Printf("deliver: failed to marshal event, dropping: %v", err)
		d.eventsDropped.Add(1)
		return true
	}
	fields, err := extractEnvelopeFields(body)
	if err != nil {
		log.Printf("deliver: failed to read envelope fields from marshalled event, dropping: %v", err)
		d.eventsDropped.Add(1)
		return true
	}

	timestamp := strconv.FormatInt(d.now().Unix(), 10)
	signature := d.sign(timestamp, body)

	if d.dryRun {
		log.Printf(
			"deliver: WOULD POST %s\n"+
				"  Content-Type: application/json; charset=utf-8\n"+
				"  User-Agent: cashstratum-notifier/1\n"+
				"  X-Blocksniper-Event: %s\n"+
				"  X-Blocksniper-Delivery: %s\n"+
				"  X-Blocksniper-Timestamp: %s\n"+
				"  X-Blocksniper-Signature: sha256=%s\n"+
				"  X-CashStratum-Event: %s\n"+
				"  X-CashStratum-Delivery: %s\n"+
				"  X-CashStratum-Timestamp: %s\n"+
				"  X-CashStratum-Signature: sha256=%s\n"+
				"  body: %s",
			d.url, fields.Event, fields.EventID, timestamp, signature,
			fields.Event, fields.EventID, timestamp, signature, body,
		)
		return true
	}

	delays := append([]time.Duration{0}, d.retryDelays...)
	for i, delay := range delays {
		if delay > 0 {
			select {
			case <-ctx.Done():
				return false
			case <-time.After(delay):
			}
		}

		status, sendErr := d.send(ctx, fields, timestamp, signature, body)
		if sendErr != nil && ctx.Err() != nil {
			return false
		}

		switch {
		case sendErr == nil && status >= 200 && status < 300:
			d.eventsSent.Add(1)
			return true

		case sendErr == nil && status >= 400 && status < 500:
			// Terminal per contract §6: bad signature/timestamp (401)
			// or malformed body (422) are never retried.
			log.Printf("deliver: %s %s got HTTP %d, terminal, not retried", fields.Event, fields.EventID, status)
			return true

		case i == len(delays)-1:
			// Retries exhausted -- 5xx, timeout, or transport error on
			// the final attempt.
			d.eventsDropped.Add(1)
			log.Printf("deliver: %s %s exhausted retries, dropping (last error: status=%d err=%v)", fields.Event, fields.EventID, status, sendErr)
			return true

		default:
			log.Printf("deliver: %s %s attempt %d failed (status=%d err=%v), retrying", fields.Event, fields.EventID, i+1, status, sendErr)
		}
	}
	return true
}

// send issues one HTTP POST attempt. status is 0 when the request never
// got a response at all (timeout, connection refused, DNS failure, ...) --
// callers treat that the same as a 5xx: retryable.
func (d *Deliverer) send(ctx context.Context, fields envelopeFields, timestamp, signature string, body []byte) (status int, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "cashstratum-notifier/1")
	req.Header.Set("X-Blocksniper-Event", fields.Event)
	req.Header.Set("X-Blocksniper-Delivery", fields.EventID)
	req.Header.Set("X-Blocksniper-Timestamp", timestamp)
	req.Header.Set("X-Blocksniper-Signature", "sha256="+signature)
	req.Header.Set("X-CashStratum-Event", fields.Event)
	req.Header.Set("X-CashStratum-Delivery", fields.EventID)
	req.Header.Set("X-CashStratum-Timestamp", timestamp)
	req.Header.Set("X-CashStratum-Signature", "sha256="+signature)

	resp, err := d.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	// Drain (bounded) so the connection can be reused; the body content
	// itself is never needed here.
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))

	return resp.StatusCode, nil
}

// sign computes hex(HMAC_SHA256(secret, timestamp + "." + body)) per
// contract §2. The secret is never logged or returned -- only this digest.
func (d *Deliverer) sign(timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(d.secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// eventLabel is a small human-readable tag for log lines (queue-shed,
// etc.) -- never fatal if the event doesn't marshal cleanly, since this is
// diagnostic only.
func eventLabel(event any) string {
	body, err := json.Marshal(event)
	if err != nil {
		return "(unmarshalable event)"
	}
	fields, err := extractEnvelopeFields(body)
	if err != nil {
		return "(event with unreadable envelope)"
	}
	return fields.Event + " " + fields.EventID
}
