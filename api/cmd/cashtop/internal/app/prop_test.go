package app

import (
	"os"
	"path/filepath"
	"testing"

	"cashstratumapi/cmd/cashtop/internal/ui"
	"cashstratumapi/internal/poolindex"
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
	applyProp(&s, 312_500_000, poolindex.DefaultMinersPotBP)
	p := s.Prop
	if !p.On || p.Reward != 312_500_000 || p.MinersPot != 150_000_000 || p.FinderBonus != 156_250_000 || p.MinersPotBP != 4800 {
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
	applyProp(&unknown, 0, poolindex.DefaultMinersPotBP)
	if !unknown.Prop.On || unknown.Prop.Reward != 0 || unknown.Round.Miners[0].EstSats != 0 {
		t.Fatalf("%+v", unknown)
	}
}

func TestApplyPropMinersPotBP(t *testing.T) {
	s := ui.Snapshot{Round: ui.RoundInfo{Total: 4, Miners: []ui.RoundMiner{
		{Address: "a", Diff: 3}, {Address: "b", Diff: 1},
	}}}
	applyProp(&s, 100_000_000, 4600)
	if s.Prop.MinersPot != 46_000_000 || s.Prop.MinersPotBP != 4600 || s.Prop.FinderBonus != 50_000_000 {
		t.Fatalf("%+v", s.Prop)
	}
	if s.Round.Miners[0].EstSats != 34_500_000 || s.Round.Miners[1].EstSats != 11_500_000 {
		t.Fatalf("%+v", s.Round.Miners)
	}
}

func TestMinersPotBPConf(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		body    string
		want    int64
		wantErr bool
	}{
		{`{"poolfee": 50, "pooladdress": "bitcoincash:qqpayroll"}`, 4800, false},
		{`{"poolfee": 50, "pooladdress": "bitcoincash:qqpayroll", "miners_pot_bp": 4600}`, 4600, false},
		{`{"miners_pot_bp": 5000}`, 5000, false},
		{`{"miners_pot_bp": 4600.0}`, 4600, false},
		{`{"miners_pot_bp": 0}`, 4800, true},
		{`{"miners_pot_bp": -10}`, 4800, true},
		{`{"miners_pot_bp": 5001}`, 4800, true},
		{`{"miners_pot_bp": 46.5}`, 4800, true},
		{`{"miners_pot_bp": "4600"}`, 4800, false}, // not a number: the conf does not parse as this shape
		{`not json`, 4800, false},
	}
	for i, c := range cases {
		p := filepath.Join(dir, "c"+string(rune('a'+i))+".conf")
		if err := os.WriteFile(p, []byte(c.body), 0o600); err != nil {
			t.Fatal(err)
		}
		got, err := MinersPotBP(p)
		if got != c.want || (err != nil) != c.wantErr {
			t.Errorf("%s: got %d, %v; want %d, error %v", c.body, got, err, c.want, c.wantErr)
		}
	}
	if got, err := MinersPotBP(filepath.Join(dir, "missing.conf")); got != 4800 || err != nil {
		t.Errorf("missing conf: %d %v", got, err)
	}
}
