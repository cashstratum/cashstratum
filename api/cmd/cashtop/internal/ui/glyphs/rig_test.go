package glyphs

import "testing"

func TestInfer(t *testing.T) {
	cases := []struct {
		ua, name string
		want     Class
	}{
		{"bmminer/2.0.0", "u.rig01", ASIC},
		{"bitaxe/BM1370/v2.4.2", "", Axe},
		{"NiceHash/1.0.0", "", Rental},
		{"", "user.nerdaxe1", Axe},
		{"cpuminer/2.5.1", "", Software},
		{"", "user.rig01", Unknown},
	}
	for _, c := range cases {
		if got := Infer(c.ua, c.name); got != c.want {
			t.Errorf("Infer(%q,%q) = %v, want %v", c.ua, c.name, got, c.want)
		}
	}
	if Badge(ASIC, false) != "◆" || Badge(Rental, false) != "◇" || Badge(ASIC, true) != "*" || Badge(Unknown, true) != "o" {
		t.Error("badges")
	}
}
