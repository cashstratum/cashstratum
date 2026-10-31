// Package poolevents defines the pool event wire format -- the common
// envelope every event carries plus the typed payloads -- and the
// classifier that turns ckpool.log lines into those typed events.
//
// The JSON field names and field order of every type in this file are a
// receiver contract: webhook receivers reject an unexpected body as a
// terminal error, so they must not change.
package poolevents

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"time"
)

// NotifierVersion is the semver of the notifier build, sent as
// notifier_version on every event so a receiver can tell which build
// produced a given payload.
const NotifierVersion = "1.0.0"

// PoolName returns the configured pool identifier, defaulting to
// "blocksniper" so receivers expecting that identifier continue to work
// without disruption. Configurable via the NOTIFIER_POOL_NAME environment
// variable.
func PoolName() string {
	if name := os.Getenv("NOTIFIER_POOL_NAME"); name != "" {
		return name
	}
	return "blocksniper"
}

// Envelope carries the five fields common to every event.
// Typed payloads embed it and add their own fields via json.Marshal on the
// concrete struct -- Go's embedding + struct tags flattens the fields into
// one JSON object, matching the wire format exactly.
type Envelope struct {
	Event           string `json:"event"`
	EventID         string `json:"event_id"`
	EmittedAt       string `json:"emitted_at"`
	Pool            string `json:"pool"`
	NotifierVersion string `json:"notifier_version"`
}

// NewEnvelope builds the common envelope for a freshly observed event.
// emittedAt is "now" in the caller's sense of "now" -- the instant the
// event was observed, not when it will eventually be sent.
func NewEnvelope(event string) Envelope {
	return Envelope{
		Event:           event,
		EventID:         NewUUIDv7(),
		EmittedAt:       FormatEmittedAt(time.Now().UTC()),
		Pool:            PoolName(),
		NotifierVersion: NotifierVersion,
	}
}

// FormatEmittedAt renders emitted_at as RFC3339 with millisecond precision
// in UTC, e.g. "2026-08-30T23:26:27.191Z" -- matching the contract example
// byte-for-byte (including the "Z" suffix rather than "+00:00").
func FormatEmittedAt(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// NetworkBlockEvent is §4.1 -- any new block seen on the network, sourced
// from ZMQ hashblock. hash is the only field ZMQ itself provides; height and
// block_time come from an RPC enrichment step that may fail, in which case
// Enriched is false and both are left nil rather than suppressing the event.
type NetworkBlockEvent struct {
	Envelope
	Hash      string `json:"hash"`
	Height    *int64 `json:"height"`
	BlockTime *int64 `json:"block_time"`
	Enriched  bool   `json:"enriched"`
	Source    string `json:"source"`
}

// NewNetworkBlockEvent builds an unenriched §4.1 event for a freshly seen
// block hash. Callers run enrichment and set Height/BlockTime/Enriched
// afterwards; the event is emitted either way.
func NewNetworkBlockEvent(hash string) NetworkBlockEvent {
	return NetworkBlockEvent{
		Envelope: NewEnvelope("network.block"),
		Hash:     hash,
		Source:   "zmq",
	}
}

// PoolBlockSubmittedEvent is §4.2 -- "BLOCK ACCEPTED!" in ckpool.log, the
// earliest possible "we may have one" signal. It carries no height, hash, or
// worker: ckpool does not have them at this point in its own code.
// SolveID is minted by the classifier and carried onto whichever terminal
// event follows (§5); for a submitted event SolveID == EventID.
type PoolBlockSubmittedEvent struct {
	Envelope
	SolveID string `json:"solve_id"`
}

// NewPoolBlockSubmittedEvent builds a §4.2 event. id is minted by the
// caller and used for both EventID and SolveID -- the contract requires
// SolveID == EventID for a submitted event, so this overrides the EventID
// NewEnvelope would otherwise mint on its own rather than generating two
// different ids and reconciling them.
func NewPoolBlockSubmittedEvent(id string) PoolBlockSubmittedEvent {
	env := NewEnvelope("pool.block.submitted")
	env.EventID = id
	return PoolBlockSubmittedEvent{Envelope: env, SolveID: id}
}

// PoolBlockEvent is §4.3 -- our block was confirmed by the node ("Solved
// and confirmed block %d by %s" in ckpool.log). There is deliberately no
// hash field: ckpool's log line does not contain one.
type PoolBlockEvent struct {
	Envelope
	SolveID    string  `json:"solve_id"`
	Height     int64   `json:"height"`
	WorkerName string  `json:"workername"`
	Username   string  `json:"username"`
	Worker     *string `json:"worker"`
}

// NewPoolBlockEvent builds a §4.3 event. workername ships byte-identical to
// what ckpool logged; username/worker are a split of it (see
// SplitWorkerName).
func NewPoolBlockEvent(solveID string, height int64, workername, username string, worker *string) PoolBlockEvent {
	return PoolBlockEvent{
		Envelope:   NewEnvelope("pool.block"),
		SolveID:    solveID,
		Height:     height,
		WorkerName: workername,
		Username:   username,
		Worker:     worker,
	}
}

// PoolBlockRejectedEvent is §4.4 -- we submitted, the node refused it
// ("Submitted, but had block %d rejected" in ckpool.log). Reason is nil
// unless ckpool also logged the node's own submitblock rejection string.
type PoolBlockRejectedEvent struct {
	Envelope
	SolveID string  `json:"solve_id"`
	Height  int64   `json:"height"`
	Reason  *string `json:"reason"`
}

// NewPoolBlockRejectedEvent builds a §4.4 event. reason is nil unless
// ckpool also logged a "SUBMIT BLOCK RETURNED: ..." line shortly before
// this rejection (see ReasonWindow).
func NewPoolBlockRejectedEvent(solveID string, height int64, reason *string) PoolBlockRejectedEvent {
	return PoolBlockRejectedEvent{
		Envelope: NewEnvelope("pool.block.rejected"),
		SolveID:  solveID,
		Height:   height,
		Reason:   reason,
	}
}

// HeartbeatEvent is §4.5 -- sent every 5 minutes regardless of block
// activity, exercising the identical delivery path as real events so a
// heartbeat arriving proves the whole pipe end to end.
type HeartbeatEvent struct {
	Envelope
	UptimeS            int64   `json:"uptime_s"`
	ZmqConnected       bool    `json:"zmq_connected"`
	LogTailHealthy     bool    `json:"log_tail_healthy"`
	LastNetworkBlockAt *string `json:"last_network_block_at"`
	LastPoolBlockAt    *string `json:"last_pool_block_at"`
	EventsSent         int64   `json:"events_sent"`
	EventsDropped      int64   `json:"events_dropped"`
}

// NewUUIDv7 generates an RFC 9562 UUIDv7: a 48-bit big-endian Unix
// millisecond timestamp, a 4-bit version field (0111), 12 bits of random
// "rand_a", the 2-bit variant field (10), and 62 bits of random "rand_b".
// Implemented locally rather than pulling a second dependency for one
// function -- the format is small and stable.
func NewUUIDv7() string {
	var u [16]byte

	ms := uint64(time.Now().UnixMilli())
	u[0] = byte(ms >> 40)
	u[1] = byte(ms >> 32)
	u[2] = byte(ms >> 24)
	u[3] = byte(ms >> 16)
	u[4] = byte(ms >> 8)
	u[5] = byte(ms)

	// The remaining 10 bytes (rand_a's 12 bits live in u[6:8], rand_b's 62
	// bits in u[8:16]) start as pure randomness; the version/variant bits
	// below then overwrite their fixed positions.
	randBuf := make([]byte, 10)
	if _, err := rand.Read(randBuf); err != nil {
		// crypto/rand.Read on a supported platform does not fail in
		// practice; a zeroed tail still yields a structurally valid
		// (if less random) UUIDv7 rather than a panic.
		for i := range randBuf {
			randBuf[i] = 0
		}
	}
	copy(u[6:16], randBuf)

	// Version: top nibble of byte 6 becomes 0111 (7).
	u[6] = (u[6] & 0x0F) | 0x70
	// Variant: top two bits of byte 8 become 10.
	u[8] = (u[8] & 0x3F) | 0x80

	return formatUUID(u)
}

// formatUUID renders 16 raw bytes as the canonical
// 8-4-4-4-12 hyphenated hex UUID string.
func formatUUID(u [16]byte) string {
	buf := make([]byte, 36)
	hex.Encode(buf[0:8], u[0:4])
	buf[8] = '-'
	hex.Encode(buf[9:13], u[4:6])
	buf[13] = '-'
	hex.Encode(buf[14:18], u[6:8])
	buf[18] = '-'
	hex.Encode(buf[19:23], u[8:10])
	buf[23] = '-'
	hex.Encode(buf[24:36], u[10:16])
	return string(buf)
}
