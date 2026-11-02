// Package cashaddr decodes CashAddr payment addresses and builds the
// matching output scripts. It depends on the standard library only.
package cashaddr

import (
	"errors"
	"strings"
)

// Address types (the type bits of the version byte).
const (
	TypeP2PKH byte = 0
	TypeP2SH  byte = 8
)

// DefaultPrefix is assumed when an address carries no prefix.
const DefaultPrefix = "bitcoincash"

const charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

const (
	hashLen     = 20
	payloadVals = 34 // 21 bytes (version + hash160) in 5-bit groups
	checksumLen = 8
)

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

// Decode parses a CashAddr string. The prefix is optional (bare addresses use
// "bitcoincash"); the string must be all lower or all upper case. It returns
// the lower-case prefix, the address type and the 20-byte hash.
func Decode(addr string) (prefix string, typ byte, hash []byte, err error) {
	lower, upper := strings.ToLower(addr), strings.ToUpper(addr)
	if addr != lower && addr != upper {
		return "", 0, nil, errors.New("cashaddr: mixed case")
	}
	addr = lower
	prefix, body := DefaultPrefix, addr
	if i := strings.IndexByte(addr, ':'); i >= 0 {
		prefix, body = addr[:i], addr[i+1:]
		if prefix == "" {
			return "", 0, nil, errors.New("cashaddr: empty prefix")
		}
	}
	if len(body) != payloadVals+checksumLen {
		return "", 0, nil, errors.New("cashaddr: bad length")
	}
	vals := make([]byte, 0, len(prefix)+1+len(body))
	for i := 0; i < len(prefix); i++ {
		vals = append(vals, prefix[i]&0x1f)
	}
	vals = append(vals, 0)
	data := make([]byte, 0, len(body))
	for i := 0; i < len(body); i++ {
		k := strings.IndexByte(charset, body[i])
		if k < 0 {
			return "", 0, nil, errors.New("cashaddr: invalid character")
		}
		data = append(data, byte(k))
	}
	if polymod(append(vals, data...)) != 0 {
		return "", 0, nil, errors.New("cashaddr: bad checksum")
	}
	data = data[:payloadVals]
	if data[payloadVals-1]&0x03 != 0 {
		return "", 0, nil, errors.New("cashaddr: non-zero padding")
	}
	raw := make([]byte, 0, 21)
	acc, bits := 0, 0
	for _, d := range data {
		acc = acc<<5 | int(d)
		bits += 5
		if bits >= 8 {
			bits -= 8
			raw = append(raw, byte(acc>>bits))
			acc &= (1 << bits) - 1
		}
	}
	if len(raw) != 21 {
		return "", 0, nil, errors.New("cashaddr: bad payload length")
	}
	version := raw[0]
	if version&0x80 != 0 {
		return "", 0, nil, errors.New("cashaddr: reserved version bit set")
	}
	if version&0x07 != 0 {
		return "", 0, nil, errors.New("cashaddr: unsupported hash size")
	}
	typ = version & 0x78
	if typ != TypeP2PKH && typ != TypeP2SH {
		return "", 0, nil, errors.New("cashaddr: unsupported address type")
	}
	return prefix, typ, raw[1:], nil
}

// OutputScript returns the locking script paying to hash for the given type.
func OutputScript(typ byte, hash []byte) ([]byte, error) {
	if len(hash) != hashLen {
		return nil, errors.New("cashaddr: hash must be 20 bytes")
	}
	switch typ {
	case TypeP2PKH:
		s := append([]byte{0x76, 0xa9, 0x14}, hash...)
		return append(s, 0x88, 0xac), nil
	case TypeP2SH:
		s := append([]byte{0xa9, 0x14}, hash...)
		return append(s, 0x87), nil
	}
	return nil, errors.New("cashaddr: unsupported address type")
}
