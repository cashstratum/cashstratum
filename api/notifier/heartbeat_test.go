package main

import (
	"context"
	"encoding/json"
	"sort"
	"testing"
	"time"
)

func fixtureHeartbeatSource(now time.Time, startedAt time.Time) HeartbeatSource {
	lastNetwork := now.Add(-30 * time.Second)
	lastPool := now.Add(-90 * time.Second)
	return HeartbeatSource{
		StartedAt:          startedAt,
		ZmqConnected:       func() bool { return true },
		LogTailHealthy:     func() bool { return true },
		LastNetworkBlockAt: func() *time.Time { return &lastNetwork },
		LastPoolBlockAt:    func() *time.Time { return &lastPool },
		EventsSent:         func() int64 { return 142 },
		EventsDropped:      func() int64 { return 0 },
		Now:                func() time.Time { return now },
	}
}

// TestNewHeartbeatEvent_ContractFieldSet is a golden key-set check against
// contract §4.5's example body -- every documented field, exact json name,
// nothing extra.
func TestNewHeartbeatEvent_ContractFieldSet(t *testing.T) {
	startedAt := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	now := startedAt.Add(24 * time.Hour)
	src := fixtureHeartbeatSource(now, startedAt)

	evt := newHeartbeatEvent(src)
	raw, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	want := []string{
		"event", "event_id", "emitted_at", "pool", "notifier_version",
		"uptime_s", "zmq_connected", "log_tail_healthy",
		"last_network_block_at", "last_pool_block_at",
		"events_sent", "events_dropped",
	}

	gotKeys := make([]string, 0, len(got))
	for k := range got {
		gotKeys = append(gotKeys, k)
	}
	sort.Strings(gotKeys)
	sort.Strings(want)

	if len(gotKeys) != len(want) {
		t.Fatalf("key set = %v, want %v", gotKeys, want)
	}
	for i := range gotKeys {
		if gotKeys[i] != want[i] {
			t.Fatalf("key set = %v, want %v", gotKeys, want)
		}
	}

	if string(got["event"]) != `"notifier.heartbeat"` {
		t.Errorf("event = %s, want \"notifier.heartbeat\"", got["event"])
	}
}

func TestNewHeartbeatEvent_FieldValues(t *testing.T) {
	startedAt := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	now := startedAt.Add(90 * time.Second)
	src := fixtureHeartbeatSource(now, startedAt)

	evt := newHeartbeatEvent(src)

	if evt.UptimeS != 90 {
		t.Errorf("UptimeS = %d, want 90", evt.UptimeS)
	}
	if !evt.ZmqConnected {
		t.Errorf("ZmqConnected = false, want true")
	}
	if !evt.LogTailHealthy {
		t.Errorf("LogTailHealthy = false, want true")
	}
	if evt.EventsSent != 142 {
		t.Errorf("EventsSent = %d, want 142", evt.EventsSent)
	}
	if evt.EventsDropped != 0 {
		t.Errorf("EventsDropped = %d, want 0", evt.EventsDropped)
	}
	wantNetwork := formatEmittedAt(now.Add(-30 * time.Second))
	if evt.LastNetworkBlockAt == nil || *evt.LastNetworkBlockAt != wantNetwork {
		t.Errorf("LastNetworkBlockAt = %v, want %q", evt.LastNetworkBlockAt, wantNetwork)
	}
	wantPool := formatEmittedAt(now.Add(-90 * time.Second))
	if evt.LastPoolBlockAt == nil || *evt.LastPoolBlockAt != wantPool {
		t.Errorf("LastPoolBlockAt = %v, want %q", evt.LastPoolBlockAt, wantPool)
	}
}

func TestNewHeartbeatEvent_NullTimestampsUntilFirstBlockSeen(t *testing.T) {
	startedAt := time.Date(2026, 8, 30, 0, 0, 0, 0, time.UTC)
	now := startedAt.Add(5 * time.Second)
	src := HeartbeatSource{
		StartedAt:          startedAt,
		ZmqConnected:       func() bool { return false },
		LogTailHealthy:     func() bool { return false },
		LastNetworkBlockAt: func() *time.Time { return nil },
		LastPoolBlockAt:    func() *time.Time { return nil },
		EventsSent:         func() int64 { return 0 },
		EventsDropped:      func() int64 { return 0 },
		Now:                func() time.Time { return now },
	}

	evt := newHeartbeatEvent(src)
	if evt.LastNetworkBlockAt != nil {
		t.Errorf("LastNetworkBlockAt = %v, want nil", *evt.LastNetworkBlockAt)
	}
	if evt.LastPoolBlockAt != nil {
		t.Errorf("LastPoolBlockAt = %v, want nil", *evt.LastPoolBlockAt)
	}
	if evt.ZmqConnected {
		t.Errorf("ZmqConnected = true, want false")
	}
	if evt.LogTailHealthy {
		t.Errorf("LogTailHealthy = true, want false -- tailer disabled/unhealthy must report false, not panic")
	}

	raw, _ := json.Marshal(evt)
	var got map[string]json.RawMessage
	json.Unmarshal(raw, &got)
	if string(got["last_network_block_at"]) != "null" {
		t.Errorf("last_network_block_at = %s, want null", got["last_network_block_at"])
	}
	if string(got["last_pool_block_at"]) != "null" {
		t.Errorf("last_pool_block_at = %s, want null", got["last_pool_block_at"])
	}
}

func TestRunHeartbeat_FirstFireThenInterval(t *testing.T) {
	startedAt := time.Now()
	src := HeartbeatSource{
		StartedAt:          startedAt,
		ZmqConnected:       func() bool { return true },
		LogTailHealthy:     func() bool { return true },
		LastNetworkBlockAt: func() *time.Time { return nil },
		LastPoolBlockAt:    func() *time.Time { return nil },
		EventsSent:         func() int64 { return 0 },
		EventsDropped:      func() int64 { return 0 },
	}

	events := make(chan any, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go runHeartbeat(ctx, src, func(event any) { events <- event }, 10*time.Millisecond, 20*time.Millisecond)

	deadline := time.After(2 * time.Second)
	got := 0
	for got < 3 {
		select {
		case evt := <-events:
			if _, ok := evt.(HeartbeatEvent); !ok {
				t.Fatalf("event type = %T, want HeartbeatEvent", evt)
			}
			got++
		case <-deadline:
			t.Fatalf("timed out waiting for heartbeats, got %d", got)
		}
	}
}

func TestRunHeartbeat_StopsOnContextCancel(t *testing.T) {
	src := HeartbeatSource{
		StartedAt:          time.Now(),
		ZmqConnected:       func() bool { return true },
		LogTailHealthy:     func() bool { return true },
		LastNetworkBlockAt: func() *time.Time { return nil },
		LastPoolBlockAt:    func() *time.Time { return nil },
		EventsSent:         func() int64 { return 0 },
		EventsDropped:      func() int64 { return 0 },
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		runHeartbeat(ctx, src, func(event any) {}, time.Hour, time.Hour)
		close(done)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatalf("runHeartbeat did not return after ctx cancellation")
	}
}
