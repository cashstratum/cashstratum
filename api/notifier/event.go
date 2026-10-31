// cashstratum-notifier emits a small set of event types to a webhook. The
// wire format -- envelope plus typed payloads -- lives in poolevents; the
// aliases below keep the notifier's own code reading in its own terms.
package main

import (
	"time"

	"cashstratumapi/internal/poolevents"
)

type (
	Envelope                = poolevents.Envelope
	NetworkBlockEvent       = poolevents.NetworkBlockEvent
	PoolBlockSubmittedEvent = poolevents.PoolBlockSubmittedEvent
	PoolBlockEvent          = poolevents.PoolBlockEvent
	PoolBlockRejectedEvent  = poolevents.PoolBlockRejectedEvent
	HeartbeatEvent          = poolevents.HeartbeatEvent
)

func newEnvelope(event string) Envelope { return poolevents.NewEnvelope(event) }

func formatEmittedAt(t time.Time) string { return poolevents.FormatEmittedAt(t) }

func newNetworkBlockEvent(hash string) NetworkBlockEvent {
	return poolevents.NewNetworkBlockEvent(hash)
}
