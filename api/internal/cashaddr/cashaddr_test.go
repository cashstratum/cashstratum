package cashaddr

import (
	"bytes"
	"encoding/hex"
	"math/rand"
	"strings"
	"testing"

	"cashstratumapi/internal/fixturegen"
)

// encode is an independent test-side encoder supporting both address types.
func encode(prefix string, typ byte, hash []byte) string {
	raw := append([]byte{typ}, hash...)
	var data []byte
	acc, bits := 0, 0
	for _, b := range raw {
		acc = acc<<8 | int(b)
		bits += 8
		for bits >= 5 {
			bits -= 5
			data = append(data, byte(acc>>bits)&0x1f)
		}
	}
	if bits > 0 {
		data = append(data, byte(acc<<(5-bits))&0x1f)
	}
	var v []byte
	for i := 0; i < len(prefix); i++ {
		v = append(v, prefix[i]&0x1f)
	}
	v = append(v, 0)
	v = append(v, data...)
	v = append(v, make([]byte, 8)...)
	chk := polymod(v)
	out := prefix + ":"
	for _, d := range data {
		out += string(charset[d])
	}
	for i := 7; i >= 0; i-- {
		out += string(charset[byte(chk>>(5*uint(i)))&0x1f])
	}
	return out
}

func TestRoundTripFixturegen(t *testing.T) {
	rng := rand.New(rand.NewSource(17))
	for i := 0; i < 50; i++ {
		var h [20]byte
		rng.Read(h[:])
		full := fixturegen.EncodeCashAddr(h)
		bare := strings.TrimPrefix(full, "bitcoincash:")
		for name, a := range map[string]string{
			"prefixed": full, "bare": bare,
			"upper": strings.ToUpper(full), "upper-bare": strings.ToUpper(bare),
		} {
			p, typ, hash, err := Decode(a)
			if err != nil || p != "bitcoincash" || typ != TypeP2PKH || !bytes.Equal(hash, h[:]) {
				t.Fatalf("%s %q: got %q %d %x %v", name, a, p, typ, hash, err)
			}
		}
	}
}

func TestKnownHash160Script(t *testing.T) {
	hb, _ := hex.DecodeString("26ec87c179dea02b35d86d9167dd4e46514a3f38")
	var h [20]byte
	copy(h[:], hb)
	_, typ, hash, err := Decode(fixturegen.EncodeCashAddr(h))
	if err != nil {
		t.Fatal(err)
	}
	s, err := OutputScript(typ, hash)
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(s); got != "76a91426ec87c179dea02b35d86d9167dd4e46514a3f3888ac" {
		t.Fatalf("script %s", got)
	}
}

func TestSpecVectors(t *testing.T) {
	cases := []struct {
		addr string
		typ  byte
		hash string
	}{
		{"bitcoincash:qpm2qsznhks23z7629mms6s4cwef74vcwvy22gdx6a", TypeP2PKH, "76a04053bda0a88bda5177b86a15c3b29f559873"},
		{"bitcoincash:ppm2qsznhks23z7629mms6s4cwef74vcwvn0h829pq", TypeP2SH, "76a04053bda0a88bda5177b86a15c3b29f559873"},
	}
	for _, c := range cases {
		_, typ, hash, err := Decode(c.addr)
		if err != nil || typ != c.typ || hex.EncodeToString(hash) != c.hash {
			t.Fatalf("%s: %d %x %v", c.addr, typ, hash, err)
		}
	}
}

func TestP2SHAndOtherPrefix(t *testing.T) {
	hash, _ := hex.DecodeString("0102030405060708090a0b0c0d0e0f1011121314")
	for _, pfx := range []string{"bitcoincash", "bchtest", "bchreg"} {
		for _, typ := range []byte{TypeP2PKH, TypeP2SH} {
			p, gt, gh, err := Decode(encode(pfx, typ, hash))
			if err != nil || p != pfx || gt != typ || !bytes.Equal(gh, hash) {
				t.Fatalf("%s/%d: %q %d %x %v", pfx, typ, p, gt, gh, err)
			}
		}
	}
	s, err := OutputScript(TypeP2SH, hash)
	if err != nil || len(s) != 23 || s[0] != 0xa9 || s[1] != 0x14 || s[22] != 0x87 {
		t.Fatalf("p2sh script %x %v", s, err)
	}
	s, _ = OutputScript(TypeP2PKH, hash)
	if len(s) != 25 {
		t.Fatalf("p2pkh len %d", len(s))
	}
}

func TestRejections(t *testing.T) {
	hash, _ := hex.DecodeString("26ec87c179dea02b35d86d9167dd4e46514a3f38")
	good := encode("bitcoincash", TypeP2PKH, hash)
	body := strings.TrimPrefix(good, "bitcoincash:")
	flipped := []byte(body)
	if flipped[5] == 'q' {
		flipped[5] = 'p'
	} else {
		flipped[5] = 'q'
	}
	cases := map[string]string{
		"flipped char": string(flipped),
		"mixed case":   "bitcoincash:" + strings.ToUpper(body[:3]) + body[3:],
		"too short":    body[:len(body)-1],
		"too long":     body + "q",
		"invalid char": body[:4] + "b" + body[5:],
		"empty":        "",
		"empty prefix": ":" + body,
		"wrong prefix": "bchtest:" + body,
		"bad type":     encode("bitcoincash", 0x10, hash),
		"reserved bit": encode("bitcoincash", 0x80, hash),
		"bad size":     encode("bitcoincash", 0x01, hash),
		"short hash":   encode("bitcoincash", TypeP2PKH, hash[:19]),
	}
	for name, a := range cases {
		if _, _, _, err := Decode(a); err == nil {
			t.Errorf("%s: %q accepted", name, a)
		}
	}
	if _, err := OutputScript(TypeP2PKH, hash[:19]); err == nil {
		t.Error("short hash accepted")
	}
	if _, err := OutputScript(0x10, hash); err == nil {
		t.Error("bad type accepted")
	}
}
