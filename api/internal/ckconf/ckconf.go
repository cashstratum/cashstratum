// Package ckconf reads the sliver of ckpool.conf that ckpool-api and its
// sibling tools (e.g. the block notifier) need: the node RPC endpoint(s),
// the mining.notify update interval, and the ZMQ block-notification
// endpoint. It is a pure reader -- ckpool.conf itself remains owned by
// ckpool.
package ckconf

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// DefaultUpdateInterval mirrors ckpool's own default (src/ckpool.c) for how
// often it broadcasts a fresh mining.notify job on an otherwise idle pool,
// used when ckpool.conf has no update_interval key. Never hardcode a
// deployment-specific value here -- LoadUpdateInterval reads the real one
// from ckpool.conf when it is set.
const DefaultUpdateInterval = 30 * time.Second

// BtcdEndpoint is one node RPC endpoint as ckpool.conf declares it. ckpool
// needs these credentials to build work at all, so on any host running a pool
// they are already present, already correct, and already rotated in lockstep
// with the node -- which is why this API reads them from there rather than
// keeping a second copy of the password in its own environment file.
type BtcdEndpoint struct {
	URL  string `json:"url"`
	Auth string `json:"auth"`
	Pass string `json:"pass"`
	// ZmqNotify is this node's ZMQ block-notification endpoint
	// (ckpool.conf's per-btcd "zmqnotify" key), e.g. "tcp://127.0.0.1:28332".
	// ckpool itself prefers this over the top-level zmqblock key when both
	// are present -- see ResolveZMQEndpoint.
	ZmqNotify string `json:"zmqnotify"`
}

// Conf is the sliver of ckpool.conf this API cares about. Everything
// else in that file belongs to the pool.
type Conf struct {
	Btcd []BtcdEndpoint `json:"btcd"`
	// UpdateInterval is how often (seconds) ckpool broadcasts a fresh
	// mining.notify job on an idle pool. A pointer so an absent key (nil)
	// is distinguishable from an explicit 0 -- both fall back to
	// DefaultUpdateInterval in LoadUpdateInterval, but for different reasons.
	UpdateInterval *int `json:"update_interval"`
	// ZmqBlock is the pool-wide fallback ZMQ block-notification endpoint
	// (ckpool.conf's top-level "zmqblock" key), used when no btcd entry
	// carries its own zmqnotify. See ResolveZMQEndpoint.
	ZmqBlock string `json:"zmqblock"`
}

// Candidates lists where configuration files (cashstratum.conf or ckpool.conf)
// are looked for when conf path is unset, most specific first. The pool is conventionally
// run with its conf beside its logdir (conf at <pool>/cashstratum.conf, logs at
// <pool>/logs/), which is what the first candidate encodes; the second covers
// a layout that keeps them together. ckpool.conf variants are checked as fallback.
func Candidates(logFile string) []string {
	logDir := filepath.Dir(logFile)
	parentDir := filepath.Dir(logDir)
	return []string{
		filepath.Join(parentDir, "cashstratum.conf"),
		filepath.Join(logDir, "cashstratum.conf"),
		filepath.Join(parentDir, "ckpool.conf"),
		filepath.Join(logDir, "ckpool.conf"),
	}
}

// LoadBtcdRPC returns the first usable node RPC endpoint from ckpool.conf, or
// nil if there is none.
//
// Every failure here is non-fatal by design: the API's job is reporting on a
// pool, and it must still start and serve logs on a host where the conf is
// unreadable (a different service account), absent, or node-less. /stats
// degrades to its log-scraped block height instead.
func LoadBtcdRPC(confPath string) *BtcdEndpoint {
	data, err := os.ReadFile(confPath)
	if err != nil {
		log.Printf("Node RPC: ckpool.conf unreadable at %s (%v) -- /stats will fall back to log-scraped height", confPath, err)
		return nil
	}
	var conf Conf
	if err := json.Unmarshal(data, &conf); err != nil {
		log.Printf("Node RPC: ckpool.conf at %s is not parseable JSON (%v) -- falling back to log-scraped height", confPath, err)
		return nil
	}
	for i := range conf.Btcd {
		if conf.Btcd[i].URL != "" {
			// Deliberately logs the URL and never Auth or Pass: this line
			// goes to the journal, which is far more widely readable than
			// the 0640 conf the credentials came from.
			log.Printf("Node RPC: using %s from %s", conf.Btcd[i].URL, confPath)
			return &conf.Btcd[i]
		}
	}
	log.Printf("Node RPC: no btcd entry with a url in %s -- falling back to log-scraped height", confPath)
	return nil
}

// LoadUpdateInterval returns how often ckpool broadcasts a mining.notify job
// on an otherwise idle pool, read from the same ckpool.conf LoadBtcdRPC
// reads -- reusing that file rather than a second, separately-maintained
// source. An absent key, an unreadable file, unparseable JSON, or a
// non-positive value all fall back to DefaultUpdateInterval (ckpool's own
// default): this must never be a value this API invents for one deployment.
func LoadUpdateInterval(confPath string) time.Duration {
	data, err := os.ReadFile(confPath)
	if err != nil {
		return DefaultUpdateInterval
	}
	var conf Conf
	if err := json.Unmarshal(data, &conf); err != nil {
		return DefaultUpdateInterval
	}
	if conf.UpdateInterval == nil || *conf.UpdateInterval <= 0 {
		return DefaultUpdateInterval
	}
	return time.Duration(*conf.UpdateInterval) * time.Second
}

// ResolveZMQEndpoint returns the ZMQ block-notification endpoint to connect
// to, mirroring ckpool's own precedence in src/stratifier.c:8883-8888: the
// first non-empty per-btcd zmqnotify wins over the top-level zmqblock
// fallback. This is load-bearing, not cosmetic -- production's ckpool.conf
// carries only btcd[0].zmqnotify and has no top-level zmqblock key at all,
// while other deployments may carry only zmqblock (or, transitionally,
// both). Returns an error naming both keys if neither is set.
func ResolveZMQEndpoint(conf Conf) (string, error) {
	for i := range conf.Btcd {
		if conf.Btcd[i].ZmqNotify != "" {
			return conf.Btcd[i].ZmqNotify, nil
		}
	}
	if conf.ZmqBlock != "" {
		return conf.ZmqBlock, nil
	}
	return "", fmt.Errorf("ckpool.conf has no ZMQ block-notification endpoint: checked btcd[].zmqnotify and top-level zmqblock, neither set")
}
