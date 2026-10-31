// Package fixturegen produces deterministic, synthetic pool data: log lines,
// per-user stats files, pool status, share logs and a complete fake pool
// directory. Every value is drawn from reserved documentation ranges or is
// derived from a fixed seed, so output is stable and never mirrors a real
// deployment. The package depends on the standard library only.
package fixturegen

import "strings"

const cashCharset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

// CashAddrPrefix is the human-readable part used for every generated address.
const CashAddrPrefix = "bitcoincash"

func polymod(v []byte) uint64 {
	c := uint64(1)
	for _, d := range v {
		c0 := byte(c >> 35)
		c = ((c & 0x07ffffffff) << 5) ^ uint64(d)
		if c0&0x01 != 0 {
			c ^= 0x98f2bc8e61
		}
		if c0&0x02 != 0 {
			c ^= 0x79b76d99e2
		}
		if c0&0x04 != 0 {
			c ^= 0xf33e5fb3c4
		}
		if c0&0x08 != 0 {
			c ^= 0xae2eabe2a8
		}
		if c0&0x10 != 0 {
			c ^= 0x1e4f43e470
		}
	}
	return c ^ 1
}

func prefixValues(prefix string) []byte {
	out := make([]byte, 0, len(prefix)+1)
	for i := 0; i < len(prefix); i++ {
		out = append(out, prefix[i]&0x1f)
	}
	return append(out, 0)
}

func to5bit(data []byte) []byte {
	var out []byte
	acc, bits := 0, 0
	for _, b := range data {
		acc = acc<<8 | int(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			out = append(out, byte(acc>>bits)&0x1f)
		}
	}
	if bits > 0 {
		out = append(out, byte(acc<<(5-bits))&0x1f)
	}
	return out
}

// EncodeCashAddr builds a P2PKH CashAddr (with prefix) from a 20-byte payload.
func EncodeCashAddr(payload [20]byte) string {
	data := to5bit(append([]byte{0x00}, payload[:]...))
	chk := polymod(append(append(prefixValues(CashAddrPrefix), data...), make([]byte, 8)...))
	var sb strings.Builder
	sb.WriteString(CashAddrPrefix)
	sb.WriteByte(':')
	for _, d := range data {
		sb.WriteByte(cashCharset[d])
	}
	for i := 7; i >= 0; i-- {
		sb.WriteByte(cashCharset[byte(chk>>(5*uint(i)))&0x1f])
	}
	return sb.String()
}

// VerifyCashAddr reports whether addr has the expected prefix and a valid
// polymod checksum.
func VerifyCashAddr(addr string) bool {
	i := strings.IndexByte(addr, ':')
	if i < 0 {
		return false
	}
	prefix, body := addr[:i], addr[i+1:]
	if len(body) < 9 {
		return false
	}
	vals := prefixValues(prefix)
	for j := 0; j < len(body); j++ {
		k := strings.IndexByte(cashCharset, body[j])
		if k < 0 {
			return false
		}
		vals = append(vals, byte(k))
	}
	return polymod(vals) == 0
}
