// Package glyphs infers a rig class from a worker's useragent and name and
// draws its badge.
package glyphs

import "strings"

// Class is the kind of mining hardware behind a worker.
type Class int

const (
	Unknown  Class = iota
	ASIC           // stock or aftermarket ASIC firmware
	Axe            // open-source single-board miners
	Rental         // hashrate marketplaces
	Proxy          // stratum proxies and aggregators
	Software       // CPU/GPU software miners
)

// Tag is the short label shown next to the badge.
func (c Class) Tag() string {
	switch c {
	case ASIC:
		return "asic"
	case Axe:
		return "axe"
	case Rental:
		return "rent"
	case Proxy:
		return "proxy"
	case Software:
		return "soft"
	}
	return "?"
}

// Owned reports whether the class is hardware the miner runs directly.
// Owned rigs get the solid badge, everything else the hollow one.
func (c Class) Owned() bool { return c == ASIC || c == Axe }

var rules = []struct {
	class Class
	keys  []string
}{
	{Rental, []string{"nicehash", "miningrigrentals", "mrr", "excavator"}},
	{Axe, []string{"bitaxe", "nerdaxe", "nerdminer", "esp-miner", "nerdqaxe", "qaxe", "luckyminer"}},
	{Proxy, []string{"proxy", "braiins-farm", "ckproxy", "stratum-v1-v2", "sv2-translator"}},
	{ASIC, []string{"bmminer", "cgminer", "bosminer", "braiins", "luxminer", "luxos", "vnish", "antminer", "whatsminer", "btminer", "avalon", "iceriver", "goldshell", "innosilicon", "epic"}},
	{Software, []string{"cpuminer", "minerd", "bfgminer", "xmrig"}},
}

// Infer classifies a worker from its useragent, falling back to its name.
func Infer(useragent, worker string) Class {
	for _, s := range []string{useragent, worker} {
		l := strings.ToLower(s)
		if l == "" {
			continue
		}
		for _, r := range rules {
			for _, k := range r.keys {
				if strings.Contains(l, k) {
					return r.class
				}
			}
		}
	}
	return Unknown
}

// Badge returns the one-cell badge: a solid diamond for owned hardware, a
// hollow one otherwise; '*' and 'o' when only ASCII may be drawn.
func Badge(c Class, ascii bool) string {
	switch {
	case ascii && c.Owned():
		return "*"
	case ascii:
		return "o"
	case c.Owned():
		return "◆"
	}
	return "◇"
}
