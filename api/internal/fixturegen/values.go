package fixturegen

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

var reservedNets = [3]string{"192.0.2", "198.51.100", "203.0.113"}

// IP returns the n-th address from the three reserved documentation ranges.
func IP(n int) string {
	if n < 0 {
		n = -n
	}
	return fmt.Sprintf("%s.%d", reservedNets[n%3], 1+(n/3)%254)
}

// Hash returns hex(sha256("fixture-N")).
func Hash(n int) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("fixture-%d", n)))
	return hex.EncodeToString(sum[:])
}

// Addr returns the n-th valid prefixed CashAddr, built from a dummy payload.
func Addr(n int) string {
	var p [20]byte
	sum := sha256.Sum256([]byte(fmt.Sprintf("fixture-addr-%d", n)))
	copy(p[:], sum[:20])
	return EncodeCashAddr(p)
}

// BareAddr returns Addr(n) without the "bitcoincash:" prefix.
func BareAddr(n int) string { return strings.TrimPrefix(Addr(n), CashAddrPrefix+":") }

// Rig returns the rig suffix for worker index w ("rig01", "rig02", ...).
func Rig(w int) string { return fmt.Sprintf("rig%02d", w+1) }

// Worker returns "user<u+1>.rig<w+1>".
func Worker(u, w int) string { return fmt.Sprintf("user%d.%s", u+1, Rig(w)) }

// FullWorker returns the prefixed address joined with the rig suffix, the form
// the pool logs as the worker of a solve.
func FullWorker(u, w int) string { return Addr(u) + "." + Rig(w) }

// SuffixString formats a hashrate in H/s the way the pool does: decimal SI
// suffix, no space, three significant figures, trailing zeros dropped.
func SuffixString(h float64) string {
	if h < 1 {
		return "0"
	}
	units := []string{"", "K", "M", "G", "T", "P", "E"}
	i := 0
	for h >= 1000 && i < len(units)-1 {
		h /= 1000
		i++
	}
	var s string
	switch {
	case h >= 100:
		s = fmt.Sprintf("%.0f", h)
	case h >= 10:
		s = fmt.Sprintf("%.1f", h)
	default:
		s = fmt.Sprintf("%.2f", h)
	}
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s + units[i]
}
