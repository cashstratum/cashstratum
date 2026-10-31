package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
)

const dash = "-"

// fmtSI formats v with a decimal SI suffix and three significant figures.
func fmtSI(v float64) string {
	if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return "0"
	}
	units := []string{"", "K", "M", "G", "T", "P", "E", "Z"}
	i := 0
	for v >= 1000 && i < len(units)-1 {
		v /= 1000
		i++
	}
	var s string
	switch {
	case v >= 100:
		s = strconv.FormatFloat(v, 'f', 0, 64)
	case v >= 10:
		s = strconv.FormatFloat(v, 'f', 1, 64)
	default:
		s = strconv.FormatFloat(v, 'f', 2, 64)
	}
	return s + units[i]
}

func fmtHash(h float64) string {
	if h <= 0 {
		return dash
	}
	return fmtSI(h) + "H/s"
}

func fmtDiff(d float64) string {
	if d <= 0 {
		return dash
	}
	return fmtSI(d)
}

func fmtPct(p float64) string {
	switch {
	case math.IsNaN(p) || math.IsInf(p, 0):
		return dash
	case p >= 1000:
		return strconv.FormatFloat(p, 'f', 0, 64) + "%"
	case p >= 10:
		return strconv.FormatFloat(p, 'f', 1, 64) + "%"
	case p >= 1:
		return strconv.FormatFloat(p, 'f', 2, 64) + "%"
	}
	return fmtSig(p) + "%"
}

// fmtOfNet formats a share's size as a percent of network difficulty for the
// "of net" columns. It always ends in %, stays within a narrow cell, and
// reads "<0.001%" below that floor rather than clipping the sign.
func fmtOfNet(p float64) string {
	switch {
	case math.IsNaN(p) || math.IsInf(p, 0):
		return dash
	case p < 0.001:
		return "<0.001%"
	case p >= 1000:
		return strconv.FormatFloat(p, 'f', 0, 64) + "%"
	case p >= 10:
		return strconv.FormatFloat(p, 'f', 1, 64) + "%"
	case p >= 1:
		return strconv.FormatFloat(p, 'f', 2, 64) + "%"
	}
	s := strconv.FormatFloat(p, 'f', 4, 64)
	s = strings.TrimSuffix(strings.TrimRight(s, "0"), ".")
	return s + "%"
}

// fmtSig keeps three significant figures of a small value without an
// exponent, so long odds read as 0.00161 rather than 0.00.
func fmtSig(v float64) string {
	if v <= 0 || math.IsNaN(v) {
		return "0"
	}
	if v >= 1 {
		return strconv.FormatFloat(v, 'f', 2, 64)
	}
	prec := 2 - int(math.Floor(math.Log10(v)))
	if prec > 12 {
		return "0"
	}
	return strconv.FormatFloat(v, 'f', prec, 64)
}

func fmtInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// fmtDur renders a duration with its two most significant units.
func fmtDur(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	if d >= time.Duration(math.MaxInt64) {
		return "never"
	}
	days := int64(d / (24 * time.Hour))
	h := int64(d/time.Hour) % 24
	m := int64(d/time.Minute) % 60
	s := int64(d/time.Second) % 60
	switch {
	case days >= 365:
		return fmt.Sprintf("%dy %dd", days/365, days%365)
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, h)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}

// fmtCoarse is fmtDur at minute granularity, for values that should not
// repaint every second.
func fmtCoarse(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1m"
	case d < time.Hour:
		return itoa(int(d/time.Minute)) + "m"
	}
	return fmtDur(d)
}

func ago(now, t time.Time) string {
	if t.IsZero() {
		return dash
	}
	return fmtDur(now.Sub(t)) + " ago"
}

func fmtBCH(sats int64) string {
	if sats <= 0 {
		return dash
	}
	return strconv.FormatFloat(float64(sats)/1e8, 'f', 4, 64) + " BCH"
}

// shortHash keeps the head and tail of a long identifier.
func shortHash(s string, keep int, ell string) string {
	if len(s) <= 2*keep+1 {
		return s
	}
	return s[:keep] + ell + s[len(s)-keep:]
}

// shortAddr drops the CashAddr prefix and middle.
func shortAddr(s, ell string) string {
	if i := strings.IndexByte(s, ':'); i >= 0 {
		s = s[i+1:]
	}
	return shortHash(s, 6, ell)
}

// shortWorker shortens an "<address>.<rig>" finder to "<short>.<rig>".
func shortWorker(s, ell string) string {
	addr, rig, ok := strings.Cut(s, ".")
	if ok && len(addr) > 20 {
		return shortAddr(addr, ell) + "." + rig
	}
	if len(s) > 24 {
		return shortAddr(s, ell)
	}
	return s
}

// fit pads or truncates s (which may hold escapes) to exactly w cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	sw := ansi.StringWidth(s)
	if sw > w {
		s = ansi.Truncate(s, w, "")
		sw = ansi.StringWidth(s)
	}
	return s + strings.Repeat(" ", w-sw)
}

// right right-aligns s in w cells.
func right(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw >= w {
		return fit(s, w)
	}
	return strings.Repeat(" ", w-sw) + s
}

func itoa(n int) string { return strconv.Itoa(n) }

// coarseAgo renders an age at coarse resolution, or a dash for an unknown
// (zero) time.
func coarseAgo(now, t time.Time) string {
	if t.IsZero() {
		return dash
	}
	return fmtCoarse(now.Sub(t))
}

// fmtStamp renders a UTC minute stamp, or a dash for an unknown time.
func fmtStamp(t time.Time) string {
	if t.IsZero() {
		return dash
	}
	return t.UTC().Format("2006-01-02 15:04")
}
