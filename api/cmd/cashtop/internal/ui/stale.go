package ui

import (
	"strings"
	"time"
)

// stale reports whether the pool is known not to be running. Its last
// figures stay in the snapshot but are rendered as unknown.
func (c *Context) stale() bool { return c.Snap != nil && c.Snap.Pool.Stale }

// hash formats a hashrate, as a dash while the pool is not running.
func (c *Context) hash(v float64) string {
	if c.stale() {
		return dash
	}
	return fmtHash(v)
}

// count formats a live count, as a dash while the pool is not running.
func (c *Context) count(n int) string {
	if c.stale() {
		return dash
	}
	return itoa(n)
}

// classify is Classify, except that no worker is live while the pool is down.
func (c *Context) classify(w WorkerRow) WorkerState {
	if c.stale() {
		return StateOff
	}
	return Classify(w, c.Now)
}

// countStates tallies the workers by state under classify.
func (c *Context) countStates(ws []WorkerRow) (live, stale, off int) {
	for _, w := range ws {
		switch c.classify(w) {
		case StateLive:
			live++
		case StateStale:
			stale++
		default:
			off++
		}
	}
	return
}

// staleBanner is the alert line shown under the header while the pool is not
// running. The "log idle" reason carries the time of the last activity.
func staleBanner(ctx *Context, w int) string {
	p := ctx.Snap.Pool
	sep := " " + ctx.sym.Sep + " "
	var parts []string
	for _, r := range strings.Split(p.StaleReason, "; ") {
		if r == "" {
			continue
		}
		if r == "log idle" && !p.LastActivity.IsZero() {
			r += " since " + p.LastActivity.UTC().Format("15:04:05") + " UTC (" + ageSince(ctx.Now, p.LastActivity) + " ago)"
		}
		parts = append(parts, r)
	}
	text := " POOL NOT RUNNING"
	if len(parts) > 0 {
		text += sep + strings.Join(parts, sep)
	}
	return ctx.Theme.Alert.Render(fit(text, w))
}

func ageSince(now, t time.Time) string {
	if d := now.Sub(t); d >= time.Minute {
		return fmtCoarse(d)
	}
	return fmtDur(now.Sub(t))
}
