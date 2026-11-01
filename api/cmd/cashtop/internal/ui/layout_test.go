package ui

import "testing"

func TestGrowFlex(t *testing.T) {
	cols := []column{
		{title: "a", width: 5, prio: 0},
		{title: "b", width: 10, prio: 1, flex: 30},
		{title: "c", width: 4, prio: 2},
	}
	all := []bool{true, true, true}
	widths := func(c []column) (o []int) {
		for _, x := range c {
			o = append(o, x.width)
		}
		return
	}
	eq := func(got, want []int) bool {
		for i := range want {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}
	// 5+1 + 10+1 + 4+1 = 22 used.
	if got := widths(growFlex(cols, all, 22)); !eq(got, []int{5, 10, 4}) {
		t.Fatalf("no spare room changed widths: %v", got)
	}
	if got := widths(growFlex(cols, all, 30)); !eq(got, []int{5, 18, 4}) {
		t.Fatalf("flex did not absorb spare room: %v", got)
	}
	if got := widths(growFlex(cols, all, 200)); !eq(got, []int{5, 30, 4}) {
		t.Fatalf("flex grew past its maximum: %v", got)
	}
	if got := widths(growFlex(cols, all, 10)); !eq(got, []int{5, 10, 4}) {
		t.Fatalf("flex squeezed columns: %v", got)
	}
	if got := widths(growFlex(cols, []bool{true, false, true}, 200)); !eq(got, []int{5, 10, 4}) {
		t.Fatalf("dropped flex column grew: %v", got)
	}
	plain := []column{{width: 5}, {width: 6}}
	if got := widths(growFlex(plain, []bool{true, true}, 200)); !eq(got, []int{5, 6}) {
		t.Fatalf("table without a flex column changed: %v", got)
	}
}
