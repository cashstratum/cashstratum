package pooltags

import (
	"encoding/hex"
	"testing"
)

func TestMatch(t *testing.T) {
	for tag, want := range map[string]string{
		"/Mined by AntPool/": "AntPool",
		"xx ViaBTC yy":       "ViaBTC",
		"solo.ckpool.org":    "CKPool",
		"/FOUNDRY USA/":      "Foundry USA",
	} {
		if got, ok := Match(tag); !ok || got != want {
			t.Errorf("Match(%q)=%q,%v want %q", tag, got, ok, want)
		}
	}
	if _, ok := Match("/some unknown miner/"); ok {
		t.Error("unknown tag matched")
	}
	if _, ok := Match(""); ok {
		t.Error("empty tag matched")
	}
}

func TestDecode(t *testing.T) {
	raw := append([]byte{0x03, 0x01, 0x02, 0xff}, []byte("/Mined by F2Pool/")...)
	raw = append(raw, 0x00, 'a', 'b', 0x00)
	raw = append(raw, []byte("tail")...)
	got := Decode(hex.EncodeToString(raw))
	if got != "/Mined by F2Pool/ tail" {
		t.Fatalf("got %q", got)
	}
	if Decode("zz") != "" || Decode("") != "" {
		t.Fatal("bad input must decode to empty")
	}
	long := hex.EncodeToString([]byte("0123456789012345678901234567890123456789012345678901234567890"))
	if len(Decode(long)) != maxTag {
		t.Fatal("tag not capped")
	}
}
