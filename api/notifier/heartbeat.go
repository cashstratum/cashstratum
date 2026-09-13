// notifier.heartbeat (contract §4.5): a liveness event sent through the
// same enqueue/signing/delivery path as every other event -- so a
// heartbeat arriving on the Laravel side proves the whole pipe, not just
// that the process is still running.
package main

import (
	"context"
	"time"
)

const (
	// heartbeatFirstDelay is deliberately short: the first heartbeat
	// should arrive soon after a deploy/restart, proving the pipe works
	// without waiting a full interval.
	heartbeatFirstDelay = 10 * time.Second
	heartbeatInterval   = 5 * time.Minute
)

// HeartbeatSource supplies the live state a heartbeat reports. main.go
// wires each field to the relevant component (ZMQSubscriber.Connected,
// LogTailer.Healthy, the Deliverer's counters/timestamps) so this package
// stays decoupled from all of them.
type HeartbeatSource struct {
	StartedAt          time.Time
	ZmqConnected       func() bool
	LogTailHealthy     func() bool
	LastNetworkBlockAt func() *time.Time
	LastPoolBlockAt    func() *time.Time
	EventsSent         func() int64
	EventsDropped      func() int64

	// Now is injectable for tests; nil means time.Now.
	Now func() time.Time
}

func (src HeartbeatSource) now() time.Time {
	if src.Now != nil {
		return src.Now()
	}
	return time.Now()
}

// newHeartbeatEvent builds one §4.5 event from a snapshot of src's current
// state. Every documented field is populated; StartedAt in the future (a
// clock edge case) would otherwise yield a negative uptime, so that is
// clamped to 0 rather than shipped as a suspicious negative number.
func newHeartbeatEvent(src HeartbeatSource) HeartbeatEvent {
	uptime := src.now().Sub(src.StartedAt)
	if uptime < 0 {
		uptime = 0
	}

	return HeartbeatEvent{
		Envelope:           newEnvelope("notifier.heartbeat"),
		UptimeS:            int64(uptime.Seconds()),
		ZmqConnected:       src.ZmqConnected(),
		LogTailHealthy:     src.LogTailHealthy(),
		LastNetworkBlockAt: formatOptionalTime(src.LastNetworkBlockAt()),
		LastPoolBlockAt:    formatOptionalTime(src.LastPoolBlockAt()),
		EventsSent:         src.EventsSent(),
		EventsDropped:      src.EventsDropped(),
	}
}

// formatOptionalTime renders a *time.Time the same way Envelope.EmittedAt
// is rendered (RFC3339, ms, UTC), or nil when there is nothing to report
// yet (contract §4.5: "null until the first block is seen after start").
func formatOptionalTime(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := formatEmittedAt(t.UTC())
	return &s
}

// RunHeartbeat emits the first heartbeat heartbeatFirstDelay after start,
// then one every heartbeatInterval, until ctx is cancelled. enqueue is
// expected to be Deliverer.Enqueue in production -- passed as a plain
// function value so this file has no direct dependency on Deliverer.
func RunHeartbeat(ctx context.Context, src HeartbeatSource, enqueue func(event any)) {
	runHeartbeat(ctx, src, enqueue, heartbeatFirstDelay, heartbeatInterval)
}

// runHeartbeat is the interval-injectable core RunHeartbeat wraps, so
// tests can use near-zero delays instead of waiting 10s/5min for real.
func runHeartbeat(ctx context.Context, src HeartbeatSource, enqueue func(event any), firstDelay, interval time.Duration) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(firstDelay):
		enqueue(newHeartbeatEvent(src))
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			enqueue(newHeartbeatEvent(src))
		}
	}
}
