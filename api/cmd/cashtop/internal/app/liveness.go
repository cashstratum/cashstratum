package app

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// StaleAfter is how long the log may stay silent before the pool counts as not
// running. The daemon writes its Pool and User statistics about every minute,
// so this allows two missed intervals plus slack.
const StaleAfter = 150 * time.Second

// defaultStratumPort is the port the daemon listens on when serverurl names
// none.
const defaultStratumPort = 3333

// tcpListen is the state code of a listening socket in /proc/net/tcp*.
const tcpListen = "0A"

// poolLiveness is the outcome of the staleness checks.
type poolLiveness struct {
	Stale  bool
	Since  time.Time // newest sign of log activity; zero when unknown
	Reason string
}

// noteLog records the timestamp of a parsed log line.
func (a *App) noteLog(t time.Time) {
	a.mu.Lock()
	if t.After(a.lastLog) {
		a.lastLog = t
	}
	a.mu.Unlock()
}

// liveness decides whether the pool is running. The log is the primary
// signal: the newer of the log file's mtime and the last parsed line time
// must be within StaleAfter. Where /proc/net/tcp is readable and the
// configuration names the stratum port, a missing listener also marks the
// pool stale. Nothing here opens a network connection.
func (a *App) liveness(now time.Time) poolLiveness {
	a.mu.Lock()
	since := a.lastLog
	a.mu.Unlock()
	if fi, err := os.Stat(a.logPath); err == nil && fi.ModTime().After(since) {
		since = fi.ModTime()
	}
	var reasons []string
	if !since.IsZero() && now.Sub(since) > StaleAfter {
		reasons = append(reasons, "log idle")
	}
	if len(a.ports) > 0 {
		if listening, ok := a.listeningPorts(); ok {
			up := false
			for _, p := range a.ports {
				if listening[p] {
					up = true
				}
			}
			if !up {
				reasons = append(reasons, "no listener on :"+strconv.Itoa(a.ports[0]))
			}
		}
	}
	return poolLiveness{Stale: len(reasons) > 0, Since: since.UTC(), Reason: strings.Join(reasons, "; ")}
}

// listeningPorts reads the kernel's TCP tables. ok is false when they cannot
// be read (not Linux, no /proc), in which case the signal is skipped.
func (a *App) listeningPorts() (map[int]bool, bool) {
	dir := a.cfg.ProcNet
	if dir == "" {
		if runtime.GOOS != "linux" {
			return nil, false
		}
		dir = "/proc/net"
	}
	out := map[int]bool{}
	read := false
	for _, name := range []string{"tcp", "tcp6"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			continue
		}
		read = true
		for p := range parseListen(string(data)) {
			out[p] = true
		}
	}
	return out, read
}

// parseListen returns the local ports in LISTEN state from the content of
// /proc/net/tcp or /proc/net/tcp6.
func parseListen(content string) map[int]bool {
	out := map[int]bool{}
	for _, line := range strings.Split(content, "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[3] != tcpListen {
			continue
		}
		i := strings.LastIndexByte(f[1], ':')
		if i < 0 {
			continue
		}
		if p, err := strconv.ParseUint(f[1][i+1:], 16, 16); err == nil && p > 0 {
			out[int(p)] = true
		}
	}
	return out
}

// stratumPorts reads the listening ports from the serverurl key of the pool
// configuration: a string or a list of "host:port" entries. A bare host means
// the default port. It returns nil when the file or key is unusable.
func stratumPorts(conf string) []int {
	if conf == "" {
		return nil
	}
	data, err := os.ReadFile(conf)
	if err != nil {
		return nil
	}
	var cf struct {
		ServerURL json.RawMessage `json:"serverurl"`
	}
	if json.Unmarshal(data, &cf) != nil || len(cf.ServerURL) == 0 {
		return nil
	}
	var urls []string
	var one string
	if json.Unmarshal(cf.ServerURL, &one) == nil {
		urls = []string{one}
	} else if json.Unmarshal(cf.ServerURL, &urls) != nil {
		return nil
	}
	var ports []int
	seen := map[int]bool{}
	for _, u := range urls {
		u = strings.TrimSpace(u)
		if u == "" {
			continue
		}
		port := defaultStratumPort
		if _, ps, err := net.SplitHostPort(u); err == nil {
			p, err := strconv.Atoi(ps)
			if err != nil || p <= 0 || p > 65535 {
				continue
			}
			port = p
		}
		if !seen[port] {
			seen[port] = true
			ports = append(ports, port)
		}
	}
	return ports
}
