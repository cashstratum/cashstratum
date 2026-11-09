package cashaddr

import "testing"

// Vectors from the CashAddr specification.
func TestDecodeLegacyMatchesSpecVectors(t *testing.T) {
	cases := []struct{ legacy, cash string }{
		{"1BpEi6DfDAUFd7GtittLSdBeYJvcoaVggu", "bitcoincash:qpm2qsznhks23z7629mms6s4cwef74vcwvy22gdx6a"},
		{"1KXrWXciRDZUpQwQmuM1DbwsKDLYAYsVLR", "bitcoincash:qr95sy3j9xwd2ap32xkykttr4cvcu7as4y0qverfuy"},
		{"16w1D5WRVKJuZUsSRzdLp9w3YGcgoxDXb", "bitcoincash:qqq3728yw0y47sqn6l2na30mcw6zm78dzqre909m2r"},
		{"3CWFddi6m4ndiGyKqzYvsFYagqDLPVMTzC", "bitcoincash:ppm2qsznhks23z7629mms6s4cwef74vcwvn0h829pq"},
		{"3LDsS579y7sruadqu11beEJoTjdFiFCdX4", "bitcoincash:pr95sy3j9xwd2ap32xkykttr4cvcu7as4yc93ky28e"},
	}
	for _, c := range cases {
		p, typ, h, err := DecodeLegacy(c.legacy)
		if err != nil {
			t.Fatalf("%s: %v", c.legacy, err)
		}
		got, err := Encode(p, typ, h)
		if err != nil || got != c.cash {
			t.Fatalf("%s: got %q (%v), want %q", c.legacy, got, err, c.cash)
		}
		if _, typ2, h2, err := Decode(got); err != nil || typ2 != typ || string(h2) != string(h) {
			t.Fatalf("%s: round trip failed: %v", got, err)
		}
	}
}

func TestDecodeLegacyRejectsDamage(t *testing.T) {
	for _, s := range []string{"", "1BpEi6DfDAUFd7GtittLSdBeYJvcoaVggv", "0OIl", "qpm2qsznhks23z7629mms6s4cwef74vcwvy22gdx6a", "1111"} {
		if _, _, _, err := DecodeLegacy(s); err == nil {
			t.Fatalf("%q decoded", s)
		}
	}
}

func TestEncodeRejectsBadInput(t *testing.T) {
	if _, err := Encode("bitcoincash", TypeP2PKH, make([]byte, 19)); err == nil {
		t.Fatal("short hash encoded")
	}
	if _, err := Encode("bitcoincash", 3, make([]byte, 20)); err == nil {
		t.Fatal("bad type encoded")
	}
	if _, err := Encode("", TypeP2PKH, make([]byte, 20)); err == nil {
		t.Fatal("empty prefix encoded")
	}
}
