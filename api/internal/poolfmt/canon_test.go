package poolfmt

import "testing"

func TestCanonicalAddressMergesSpellings(t *testing.T) {
	const want = "qpm2qsznhks23z7629mms6s4cwef74vcwvy22gdx6a"
	for _, s := range []string{
		want,
		"bitcoincash:" + want,
		"BITCOINCASH:QPM2QSZNHKS23Z7629MMS6S4CWEF74VCWVY22GDX6A",
		"QPM2QSZNHKS23Z7629MMS6S4CWEF74VCWVY22GDX6A",
		"qpm2qsznhks23z7629mms6s4cwef74vcwvy22gdX6a", // mixed case
		"1BpEi6DfDAUFd7GtittLSdBeYJvcoaVggu",         // legacy
		" " + want + " ",
	} {
		if got := CanonicalAddress(s); got != want {
			t.Fatalf("CanonicalAddress(%q) = %q, want %q", s, got, want)
		}
	}
}

func TestCanonicalAddressKeepsNonAddresses(t *testing.T) {
	for in, want := range map[string]string{
		"":                 "",
		"Bob":              "Bob",
		"bitcoincash:junk": "junk",
		"bchreg:qqq3728yw0y47sqn6l2na30mcw6zm78dzqre909m2r": "qqq3728yw0y47sqn6l2na30mcw6zm78dzqre909m2r",
	} {
		if got := CanonicalAddress(in); got != want {
			t.Fatalf("CanonicalAddress(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCanonicalAddressIsIdempotent(t *testing.T) {
	for _, s := range []string{"1BpEi6DfDAUFd7GtittLSdBeYJvcoaVggu", "bchtest:qpm2qsznhks23z7629mms6s4cwef74vcwvy22gdx6a", "Bob"} {
		c := CanonicalAddress(s)
		if CanonicalAddress(c) != c {
			t.Fatalf("not idempotent for %q: %q -> %q", s, c, CanonicalAddress(c))
		}
	}
}
