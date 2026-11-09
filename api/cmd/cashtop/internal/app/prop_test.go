package app

import (
	"os"
	"path/filepath"
	"testing"

	"cashstratumapi/cmd/cashtop/internal/ui"
)

func TestIsProp(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]bool{
		// No btcsolo key: the daemon takes solo mode from -B, so it is not required.
		`{"poolfee": 50, "pooladdress": "bitcoincash:qqpayroll"}`:                   true,
		`{"btcsolo": true, "poolfee": 50, "pooladdress": "bitcoincash:qqpayroll"}`:  true,
		`{"btcsolo": false, "poolfee": 50, "pooladdress": "bitcoincash:qqpayroll"}`: true,
		`{"poolfee": 50}`:                                        false,
		`{"poolfee": 50, "pooladdress": ""}`:                     false,
		`{"poolfee": 2, "pooladdress": "bitcoincash:qqpayroll"}`: false,
		`{"btcsolo": true}`:                                      false,
		`not json`:                                               false,
	}
	i := 0
	for body, want := range cases {
		p := filepath.Join(dir, "c"+string(rune('a'+i))+".conf")
		i++
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if got := IsProp(p); got != want {
			t.Errorf("%s: got %v want %v", body, got, want)
		}
	}
	if IsProp(filepath.Join(dir, "missing.conf")) {
		t.Error("an unreadable conf is not a pro-rata pool")
	}
}

func TestApplyProp(t *testing.T) {
	s := ui.Snapshot{Round: ui.RoundInfo{Total: 1000, Miners: []ui.RoundMiner{
		{Address: "a", Diff: 600}, {Address: "b", Diff: 395}, {Address: "c", Diff: 5},
	}}}
	applyProp(&s, 312_500_000)
	p := s.Prop
	if !p.On || p.Reward != 312_500_000 || p.MinersPot != 150_000_000 || p.FinderBonus != 156_250_000 {
		t.Fatalf("%+v", p)
	}
	if p.BelowMinimum != 1 || s.Round.Miners[2].Eligible || s.Round.Miners[2].EstSats != 0 {
		t.Fatalf("sub-1%% address: %+v", s.Round.Miners[2])
	}
	if !s.Round.Miners[0].Eligible || s.Round.Miners[0].EstSats+s.Round.Miners[1].EstSats > p.MinersPot {
		t.Fatalf("%+v", s.Round.Miners)
	}

	var unknown ui.Snapshot
	unknown.Round.Miners = []ui.RoundMiner{{Address: "a", Diff: 1}}
	applyProp(&unknown, 0)
	if !unknown.Prop.On || unknown.Prop.Reward != 0 || unknown.Round.Miners[0].EstSats != 0 {
		t.Fatalf("%+v", unknown)
	}
}
