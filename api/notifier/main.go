// blocksniper-notifier is a standalone daemon that watches the local
// ckpool/node install for block activity and reports it to Laravel over the
// webhook defined in the notifier integration contract.
//
// This binary wires two event sources -- the ZMQ hashblock subscriber
// (network.block: SUB socket -> RPC enrichment) and the
// log tailer (pool.block.submitted/pool.block/pool.block.rejected)
// -- plus a heartbeat into one Deliverer: a bounded, drop-oldest queue feeding
// a single HMAC-signing/retrying sender goroutine (see deliver.go).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cashstratumapi/internal/ckconf"
)

// eventSink is how a built event leaves the pipeline -- Deliverer.Enqueue
// in production. A function value rather than a fixed call site so
// zmqsub.go/tail.go/heartbeat.go stay decoupled from the delivery
// implementation.
type eventSink func(event any)

// config is the sliver of process configuration read from the environment.
type config struct {
	notifyURL    string
	notifySecret string
	confPath     string
	logPath      string
	dryRun       bool
}

func getEnvWithFallback(primary, fallback string) string {
	if val := os.Getenv(primary); val != "" {
		return val
	}
	return os.Getenv(fallback)
}

// loadConfig reads env vars into a config. confPath, when conf path
// is unset, is resolved by the caller via ckconf.Candidates(logPath) --
// kept out of this function so the candidate-selection logic (which needs
// to stat the filesystem) stays in main rather than in this pure read.
func loadConfig() config {
	dryRun := true
	if v, ok := os.LookupEnv("NOTIFY_DRY_RUN"); ok && v != "" {
		dryRun = v != "false" && v != "0"
	}
	return config{
		notifyURL:    getEnvWithFallback("CASHSTRATUM_NOTIFY_URL", "BLOCKSNIPER_NOTIFY_URL"),
		notifySecret: getEnvWithFallback("CASHSTRATUM_NOTIFY_SECRET", "BLOCKSNIPER_NOTIFY_SECRET"),
		confPath:     getEnvWithFallback("CASHSTRATUM_CONF_PATH", "CKPOOL_CONF_PATH"),
		logPath:      getEnvWithFallback("CASHSTRATUM_LOG_PATH", "CKPOOL_LOG_PATH"),
		dryRun:       dryRun,
	}
}

// resolveConfPath returns the ckpool.conf path to read: the explicit
// CKPOOL_CONF_PATH override when set, otherwise the first of ckconf's
// candidate locations (derived from CKPOOL_LOG_PATH) that exists on disk.
func resolveConfPath(cfg config) string {
	if cfg.confPath != "" {
		return cfg.confPath
	}
	for _, candidate := range ckconf.Candidates(cfg.logPath) {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	// Nothing found -- return the most specific candidate anyway so the
	// "unreadable" log message downstream names a real path rather than
	// an empty string.
	candidates := ckconf.Candidates(cfg.logPath)
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

func main() {
	cfg := loadConfig()

	if cfg.notifyURL == "" || cfg.notifySecret == "" {
		if !cfg.dryRun {
			fmt.Fprintln(os.Stderr, "cashstratum-notifier: CASHSTRATUM_NOTIFY_URL (or BLOCKSNIPER_NOTIFY_URL) and CASHSTRATUM_NOTIFY_SECRET (or BLOCKSNIPER_NOTIFY_SECRET) are required unless NOTIFY_DRY_RUN=true")
			os.Exit(1)
		}
		log.Printf("notifier: CASHSTRATUM_NOTIFY_URL/BLOCKSNIPER_NOTIFY_URL not set -- running in dry-run mode, events are logged only")
	}

	confPath := resolveConfPath(cfg)
	log.Printf("notifier: using conf at %s", confPath)

	conf := readConf(confPath)

	zmqEndpoint, err := ckconf.ResolveZMQEndpoint(conf)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cashstratum-notifier: %v\n", err)
		os.Exit(1)
	}
	log.Printf("notifier: subscribing to ZMQ hashblock at %s", zmqEndpoint)

	btcdRPC := ckconf.LoadBtcdRPC(confPath)
	rpcClient := newNodeRPCClient(btcdRPC)

	if cfg.dryRun {
		log.Printf("notifier: NOTIFY_DRY_RUN=true -- deliveries are logged as \"WOULD POST\", nothing is sent to %s", safeURL(cfg.notifyURL))
	}

	// deliverer owns the bounded, drop-oldest delivery queue and the
	// HMAC-signing/retrying sender goroutine (see deliver.go). sink is
	// its Enqueue method -- the only thing zmqsub/tail/heartbeat know
	// about delivery is that it exists.
	deliverer := NewDeliverer(cfg.notifyURL, cfg.notifySecret, cfg.dryRun)
	sink := eventSink(deliverer.Enqueue)

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	go deliverer.Run(ctx)

	// events is a small hand-off channel between the two producers and
	// this goroutine's dispatch loop -- distinct from the Deliverer's own
	// bounded queue, since a NetworkBlockEvent still needs its RPC
	// enrichment (up to 2s) before it is ready to hand to sink.
	events := make(chan any, 16)

	sub := NewZMQSubscriber(zmqEndpoint, func(hashHex string) {
		select {
		case events <- newNetworkBlockEvent(hashHex):
		default:
			log.Printf("notifier: events channel full, dropping network.block for %s", hashHex)
		}
	})
	go sub.Run(ctx)

	// tailer is nil in ZMQ-only mode (CKPOOL_LOG_PATH unset); Healthy()
	// on a nil *LogTailer would panic, so the heartbeat source below
	// guards on this instead of calling tailer.Healthy directly.
	var tailer *LogTailer
	if cfg.logPath != "" {
		tailer = NewLogTailer(cfg.logPath, func(event any) {
			select {
			case events <- event:
			default:
				log.Printf("notifier: events channel full, dropping pool event")
			}
		})
		go tailer.Run(ctx)
	} else {
		log.Printf("notifier: CKPOOL_LOG_PATH not set -- running ZMQ-only, pool.block* events disabled")
	}

	heartbeatSource := HeartbeatSource{
		StartedAt:          time.Now(),
		ZmqConnected:       sub.Connected,
		LogTailHealthy:     func() bool { return tailer != nil && tailer.Healthy() },
		LastNetworkBlockAt: deliverer.LastNetworkBlockAt,
		LastPoolBlockAt:    deliverer.LastPoolBlockAt,
		EventsSent:         deliverer.EventsSent,
		EventsDropped:      deliverer.EventsDropped,
	}
	go RunHeartbeat(ctx, heartbeatSource, sink)

	for {
		select {
		case <-ctx.Done():
			log.Printf("notifier: shutting down")
			return
		case raw := <-events:
			switch evt := raw.(type) {
			case NetworkBlockEvent:
				sink(enrichNetworkBlock(ctx, rpcClient, evt))
			default:
				sink(evt)
			}
		}
	}
}

// readConf loads and parses ckpool.conf, degrading to an empty Conf (never
// nil) on any read/parse failure -- ResolveZMQEndpoint then produces a
// clear "no endpoint configured" error rather than this function needing
// its own separate failure path.
func readConf(confPath string) ckconf.Conf {
	data, err := os.ReadFile(confPath)
	if err != nil {
		log.Printf("notifier: ckpool.conf unreadable at %s (%v)", confPath, err)
		return ckconf.Conf{}
	}
	var conf ckconf.Conf
	if err := json.Unmarshal(data, &conf); err != nil {
		log.Printf("notifier: ckpool.conf at %s is not parseable JSON (%v)", confPath, err)
		return ckconf.Conf{}
	}
	return conf
}

// safeURL returns url unless it's empty, in which case it returns a
// placeholder -- used only for a dry-run log line, never for anything that
// could carry a credential (BLOCKSNIPER_NOTIFY_URL itself carries none).
func safeURL(url string) string {
	if url == "" {
		return "(no BLOCKSNIPER_NOTIFY_URL set)"
	}
	return url
}
