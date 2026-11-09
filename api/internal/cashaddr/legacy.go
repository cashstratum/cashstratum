package cashaddr

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"math/big"
	"strings"
)

// TestnetPrefix is the CashAddr prefix of testnet (and regtest legacy)
// addresses.
const TestnetPrefix = "bchtest"

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// Encode builds the lower-case CashAddr string "prefix:payload" for an
// address type and a 20-byte hash.
func Encode(prefix string, typ byte, hash []byte) (string, error) {
	if len(hash) != hashLen {
		return "", errors.New("cashaddr: hash must be 20 bytes")
	}
	if typ != TypeP2PKH && typ != TypeP2SH {
		return "", errors.New("cashaddr: unsupported address type")
	}
	prefix = strings.ToLower(prefix)
	if prefix == "" {
		return "", errors.New("cashaddr: empty prefix")
	}
	raw := append([]byte{typ}, hash...)
	data := make([]byte, 0, payloadVals)
	acc, bits := 0, 0
	for _, b := range raw {
		acc = acc<<8 | int(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			data = append(data, byte(acc>>bits)&0x1f)
		}
		acc &= (1 << bits) - 1
	}
	if bits > 0 {
		data = append(data, byte(acc<<(5-bits))&0x1f)
	}
	vals := make([]byte, 0, len(prefix)+1+len(data)+checksumLen)
	for i := 0; i < len(prefix); i++ {
		vals = append(vals, prefix[i]&0x1f)
	}
	vals = append(vals, 0)
	vals = append(vals, data...)
	vals = append(vals, make([]byte, checksumLen)...)
	chk := polymod(vals)
	var sb strings.Builder
	sb.Grow(len(prefix) + 1 + len(data) + checksumLen)
	sb.WriteString(prefix)
	sb.WriteByte(':')
	for _, d := range data {
		sb.WriteByte(charset[d])
	}
	for i := 0; i < checksumLen; i++ {
		sb.WriteByte(charset[(chk>>(5*(checksumLen-1-i)))&0x1f])
	}
	return sb.String(), nil
}

// DecodeLegacy parses a Base58Check legacy address. Version bytes 0x00/0x05
// map to the mainnet prefix, 0x6f/0xc4 to the testnet prefix; any other
// version is rejected.
func DecodeLegacy(addr string) (prefix string, typ byte, hash []byte, err error) {
	if addr == "" || len(addr) > 64 {
		return "", 0, nil, errors.New("cashaddr: bad legacy length")
	}
	n := new(big.Int)
	radix := big.NewInt(58)
	for i := 0; i < len(addr); i++ {
		k := strings.IndexByte(base58Alphabet, addr[i])
		if k < 0 {
			return "", 0, nil, errors.New("cashaddr: invalid base58 character")
		}
		n.Mul(n, radix)
		n.Add(n, big.NewInt(int64(k)))
	}
	body := n.Bytes()
	zeros := 0
	for zeros < len(addr) && addr[zeros] == '1' {
		zeros++
	}
	raw := append(make([]byte, zeros), body...)
	if len(raw) != 1+hashLen+4 {
		return "", 0, nil, errors.New("cashaddr: bad legacy payload length")
	}
	first := sha256.Sum256(raw[:1+hashLen])
	second := sha256.Sum256(first[:])
	if !bytes.Equal(second[:4], raw[1+hashLen:]) {
		return "", 0, nil, errors.New("cashaddr: bad legacy checksum")
	}
	switch raw[0] {
	case 0x00:
		prefix, typ = DefaultPrefix, TypeP2PKH
	case 0x05:
		prefix, typ = DefaultPrefix, TypeP2SH
	case 0x6f:
		prefix, typ = TestnetPrefix, TypeP2PKH
	case 0xc4:
		prefix, typ = TestnetPrefix, TypeP2SH
	default:
		return "", 0, nil, errors.New("cashaddr: unsupported legacy version")
	}
	return prefix, typ, append([]byte(nil), raw[1:1+hashLen]...), nil
}
