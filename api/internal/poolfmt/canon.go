package poolfmt

import (
	"strings"

	"cashstratumapi/internal/cashaddr"
)

// CanonicalAddress is the single spelling used to key per-address data: the
// bare, lower-case CashAddr payload, with any network prefix dropped and a
// legacy Base58 address converted. Two spellings of one address (prefixed or
// not, upper or lower case, legacy or CashAddr) map to the same string.
//
// A string that is not a decodable address comes back as NormalizeAddress
// returns it, so plain usernames keep their own identity.
func CanonicalAddress(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, cand := range []string{s, strings.ToLower(s)} {
		if _, _, _, err := cashaddr.Decode(cand); err == nil {
			return bareBody(strings.ToLower(cand))
		}
	}
	if prefix, typ, hash, err := cashaddr.DecodeLegacy(NormalizeAddress(s)); err == nil {
		if enc, err := cashaddr.Encode(prefix, typ, hash); err == nil {
			return bareBody(enc)
		}
	}
	return NormalizeAddress(s)
}

func bareBody(addr string) string {
	if i := strings.LastIndexByte(addr, ':'); i >= 0 {
		return addr[i+1:]
	}
	return addr
}
