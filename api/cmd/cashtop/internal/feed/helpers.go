package feed

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// awkNum converts like awk's string-to-number: the longest numeric prefix,
// zero when there is none.
func awkNum(s string) float64 {
	s = strings.TrimLeft(s, " \t")
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			digits++
		}
	}
	if digits == 0 {
		return 0
	}
	end := i
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		k := j
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if k > j {
			end = k
		}
	}
	v, err := strconv.ParseFloat(s[:end], 64)
	if err != nil && !math.IsInf(v, 0) {
		return 0
	}
	return v
}

// jget extracts one value from a flat JSON-ish string without decoding it.
func jget(s, key string) string {
	needle := `"` + key + `":`
	i := strings.Index(s, needle)
	if i < 0 {
		return ""
	}
	rest := s[i+len(needle):]
	if strings.HasPrefix(rest, `"`) {
		rest = rest[1:]
		j := strings.IndexByte(rest, '"')
		if j < 0 {
			return ""
		}
		return rest[:j]
	}
	if j := strings.IndexAny(rest, ",}"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// si formats a number with K/M/G/T/P/E/Z suffixes; "" becomes "-".
func si(s string) string {
	if s == "" {
		return "-"
	}
	v := awkNum(s)
	const units = "KMGTPEZ"
	if v < 1000 {
		return strconv.FormatInt(int64(v), 10)
	}
	i := 0
	for v >= 1000 && i < 7 {
		v /= 1000
		i++
	}
	switch {
	case v < 10:
		return fmt.Sprintf("%.2f%c", v, units[i-1])
	case v < 100:
		return fmt.Sprintf("%.1f%c", v, units[i-1])
	}
	return fmt.Sprintf("%.0f%c", v, units[i-1])
}

// luck renders the round-effort percentage; "" renders nothing.
func luck(s string) string {
	if s == "" {
		return ""
	}
	v := awkNum(s)
	switch {
	case v < 10:
		return fmt.Sprintf("  luck %.2f%%", v)
	case v < 1000:
		return fmt.Sprintf("  luck %.1f%%", v)
	}
	return fmt.Sprintf("  luck %.0f%%", v)
}

// ParseGroups reads a monitor groups file: "address label [colour]" per line,
// "#" comments, optional network prefix. Colour is a name or 0-255 number.
func ParseGroups(text string) map[string]Group {
	out := map[string]Group{}
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		f := strings.Fields(t)
		g := Group{}
		if len(f) >= 2 {
			g.Label = f[1]
		}
		if len(f) >= 3 {
			g.Colour = colourCode(f[2])
		}
		out[noprefix(f[0])] = g
	}
	return out
}

func colourCode(name string) string {
	if name != "" && strings.Trim(name, "0123456789") == "" {
		return "38;5;" + name
	}
	switch name {
	case "red":
		return "31"
	case "green":
		return "32"
	case "yellow":
		return "33"
	case "blue":
		return "34"
	case "magenta":
		return "35"
	case "cyan":
		return "36"
	case "white":
		return "37"
	}
	return ""
}

// capMap is a string-keyed map that evicts its oldest entries past a bound.
type capMap[V any] struct {
	max   int
	m     map[string]V
	order []string
}

func newCapMap[V any](max int) *capMap[V] {
	return &capMap[V]{max: max, m: map[string]V{}}
}

func (c *capMap[V]) lookup(k string) (V, bool) { v, ok := c.m[k]; return v, ok }

func (c *capMap[V]) get(k string) V { return c.m[k] }

func (c *capMap[V]) del(k string) { delete(c.m, k) }

func (c *capMap[V]) set(k string, v V) {
	if _, ok := c.m[k]; !ok {
		c.order = append(c.order, k)
		for len(c.m) >= c.max && len(c.order) > 0 {
			old := c.order[0]
			c.order = c.order[1:]
			delete(c.m, old)
		}
		if len(c.order) > 2*c.max {
			seen := make(map[string]bool, len(c.m))
			kept := c.order[:0:0]
			for _, key := range c.order {
				if _, ok := c.m[key]; ok && !seen[key] {
					seen[key] = true
					kept = append(kept, key)
				}
			}
			c.order = kept
		}
	}
	c.m[k] = v
}
