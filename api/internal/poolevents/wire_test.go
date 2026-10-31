package poolevents

import (
	"encoding/json"
	"testing"
)

// TestPayloads_ExactBytes pins every payload's marshalled bytes, including
// field order: webhook receivers verify the signed body and reject a
// changed shape as a terminal error.
func TestPayloads_ExactBytes(t *testing.T) {
	env := func(event string) Envelope {
		return Envelope{
			Event:           event,
			EventID:         "018f3c9e-7a41-7b02-9c33-2d5e8f1a4b60",
			EmittedAt:       "2026-08-30T23:26:27.191Z",
			Pool:            "blocksniper",
			NotifierVersion: "1.0.0",
		}
	}
	const head = `"event_id":"018f3c9e-7a41-7b02-9c33-2d5e8f1a4b60","emitted_at":"2026-08-30T23:26:27.191Z","pool":"blocksniper","notifier_version":"1.0.0"`
	height, blockTime := int64(968432), int64(1756598780)
	worker, reason := "rig", "inconclusive-not-best-prevblk"

	cases := []struct {
		name string
		v    any
		want string
	}{
		{"network.block", NetworkBlockEvent{Envelope: env("network.block"), Hash: "00ab", Height: &height, BlockTime: &blockTime, Enriched: true, Source: "zmq"},
			`{"event":"network.block",` + head + `,"hash":"00ab","height":968432,"block_time":1756598780,"enriched":true,"source":"zmq"}`},
		{"network.block unenriched", NetworkBlockEvent{Envelope: env("network.block"), Hash: "00ab", Source: "zmq"},
			`{"event":"network.block",` + head + `,"hash":"00ab","height":null,"block_time":null,"enriched":false,"source":"zmq"}`},
		{"pool.block.submitted", PoolBlockSubmittedEvent{Envelope: env("pool.block.submitted"), SolveID: "s1"},
			`{"event":"pool.block.submitted",` + head + `,"solve_id":"s1"}`},
		{"pool.block", PoolBlockEvent{Envelope: env("pool.block"), SolveID: "s1", Height: 968432, WorkerName: "addr.rig", Username: "addr", Worker: &worker},
			`{"event":"pool.block",` + head + `,"solve_id":"s1","height":968432,"workername":"addr.rig","username":"addr","worker":"rig"}`},
		{"pool.block.rejected", PoolBlockRejectedEvent{Envelope: env("pool.block.rejected"), SolveID: "s1", Height: 968432, Reason: &reason},
			`{"event":"pool.block.rejected",` + head + `,"solve_id":"s1","height":968432,"reason":"inconclusive-not-best-prevblk"}`},
		{"notifier.heartbeat", HeartbeatEvent{Envelope: env("notifier.heartbeat"), UptimeS: 60, ZmqConnected: true, LogTailHealthy: true, EventsSent: 3},
			`{"event":"notifier.heartbeat",` + head + `,"uptime_s":60,"zmq_connected":true,"log_tail_healthy":true,"last_network_block_at":null,"last_pool_block_at":null,"events_sent":3,"events_dropped":0}`},
	}
	for _, c := range cases {
		raw, err := json.Marshal(c.v)
		if err != nil {
			t.Fatalf("%s: json.Marshal() error = %v", c.name, err)
		}
		if string(raw) != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, raw, c.want)
		}
	}
}
