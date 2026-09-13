// ZMQ hashblock subscription: connects to the node's ZMQ block-notification
// endpoint (resolved by ckconf.ResolveZMQEndpoint) and turns each hashblock
// delivery into a hex block hash handed to a callback. Reconnects on any
// error with capped exponential backoff -- a single dropped connection must
// never end the subscription loop, since that is the notifier's only source
// of sub-second block awareness.
package main

import (
	"context"
	"encoding/hex"
	"log"
	"sync/atomic"
	"time"

	"github.com/go-zeromq/zmq4"
)

// hashblockTopic is the ZMQ PUB topic bitcoind publishes new block hashes
// on. The SUB socket subscribes to exactly this topic, nothing else.
const hashblockTopic = "hashblock"

// hashFrameLen is the length in bytes of the block hash frame within a
// hashblock delivery. A hashblock message is multi-frame: a topic frame
// ("hashblock", 9 bytes), the 32-byte hash itself, and a 4-byte
// little-endian sequence number. Selecting by length rather than position
// is deliberate -- frame order is a ZMTP/bitcoind implementation detail we
// should not hard-code as Frames[0].
const hashFrameLen = 32

const (
	zmqInitialBackoff = 1 * time.Second
	zmqMaxBackoff     = 30 * time.Second
)

// selectHashFrame picks the 32-byte hash out of a hashblock delivery's
// frames, ignoring the topic frame, the sequence-number frame, and any
// other frame that doesn't match the expected length. Returns ok=false
// (never panics) when no frame of the right length is present, which can
// legitimately happen on a malformed or unexpected delivery.
func selectHashFrame(frames [][]byte) (hash []byte, ok bool) {
	for _, f := range frames {
		if len(f) == hashFrameLen {
			return f, true
		}
	}
	return nil, false
}

// ZMQSubscriber owns the SUB socket lifecycle: dial, subscribe, receive
// loop, reconnect. onHash is called once per received block hash, hex
// encoded exactly as the raw ZMQ bytes read (bitcoind reverses the hash to
// display byte order before publishing it on ZMQ, so no further flip is
// needed -- this matches what ckpool.log shows via its own
// flip_32+__bin2hex, and what getblockheader/block explorers show).
type ZMQSubscriber struct {
	endpoint string
	onHash   func(hashHex string)

	connected atomic.Bool

	// dial/newSub are overridable for tests that want to drive runOnce
	// without a real socket; production code leaves them at their
	// zero value and getNewSub/dialSocket below supply the real thing.
	newSub func(ctx context.Context) zmq4.Socket
}

// NewZMQSubscriber builds a subscriber for the given ZMQ endpoint
// (e.g. "tcp://127.0.0.1:28332"). onHash is invoked from the subscriber's
// own goroutine -- callers that need to hand work off elsewhere should
// queue it (e.g. onto a channel) rather than block inside the callback,
// since a slow callback stalls the receive loop and risks ZMQ dropping
// messages under its high-water-mark policy.
func NewZMQSubscriber(endpoint string, onHash func(hashHex string)) *ZMQSubscriber {
	return &ZMQSubscriber{
		endpoint: endpoint,
		onHash:   onHash,
		newSub:   func(ctx context.Context) zmq4.Socket { return zmq4.NewSub(ctx) },
	}
}

// Connected reports whether the SUB socket is currently dialed and
// subscribed. Read by the heartbeat sink for notifier.heartbeat's
// zmq_connected field.
func (s *ZMQSubscriber) Connected() bool {
	return s.connected.Load()
}

// Run drives the subscribe/receive/reconnect loop until ctx is cancelled.
// It never returns on its own otherwise -- every error from a single
// connection attempt is logged and followed by a capped exponential
// backoff before retrying, rather than propagated to the caller.
func (s *ZMQSubscriber) Run(ctx context.Context) {
	backoff := zmqInitialBackoff
	for {
		if ctx.Err() != nil {
			return
		}

		connectedThisAttempt, err := s.runOnce(ctx)
		s.connected.Store(false)

		if err != nil {
			log.Printf("zmq: subscription to %s ended (%v), reconnecting in %s", s.endpoint, err, backoff)
		}

		if connectedThisAttempt {
			// A successful connect (however briefly it lasted)
			// resets the backoff -- a flapping-but-eventually-fine
			// endpoint should not be punished with an ever-growing
			// wait, only a persistently unreachable one.
			backoff = zmqInitialBackoff
		} else {
			backoff *= 2
			if backoff > zmqMaxBackoff {
				backoff = zmqMaxBackoff
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// runOnce dials once, subscribes to the hashblock topic, and receives
// messages until ctx is cancelled or Recv errors. Returns whether the dial
// itself succeeded (used by Run to decide whether to reset backoff) and any
// error encountered.
func (s *ZMQSubscriber) runOnce(ctx context.Context) (connected bool, err error) {
	sock := s.newSub(ctx)
	defer sock.Close()

	if err := sock.SetOption(zmq4.OptionSubscribe, hashblockTopic); err != nil {
		return false, err
	}
	if err := sock.Dial(s.endpoint); err != nil {
		return false, err
	}

	s.connected.Store(true)

	for {
		if ctx.Err() != nil {
			return true, nil
		}
		msg, err := sock.Recv()
		if err != nil {
			return true, err
		}
		hash, ok := selectHashFrame(msg.Frames)
		if !ok {
			// Short/oversized/unexpected frame -- log and keep
			// receiving rather than treating it as fatal.
			log.Printf("zmq: hashblock delivery with no 32-byte frame (got %d frames), ignoring", len(msg.Frames))
			continue
		}
		if s.onHash != nil {
			s.onHash(hex.EncodeToString(hash))
		}
	}
}
