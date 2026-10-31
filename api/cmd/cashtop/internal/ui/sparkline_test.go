package ui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
)

func TestPackBraille(t *testing.T) {
	cases := []struct {
		l, r int
		want rune
	}{
		{0, 0, 0x2800}, {4, 4, 0x28FF}, {1, 1, 0x28C0}, {2, 0, 0x2844}, {0, 3, 0x28B0}, {9, -1, 0x2847},
	}
	for _, c := range cases {
		if got := packBraille(c.l, c.r); got != c.want {
			t.Errorf("packBraille(%d,%d) = %U, want %U", c.l, c.r, got, c.want)
		}
	}
}

func TestSparklineLevels(t *testing.T) {
	got := levels([]float64{0, 5, 10})
	if got[0] != 1 || got[2] != 4 {
		t.Fatalf("levels = %v", got)
	}
	for _, v := range levels([]float64{7, 7, 7}) {
		if v != 3 && v != 2 {
			t.Fatalf("flat series level %d", v)
		}
	}
}

func TestSparklineGolden(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ascii bool
	}{{"Braille", false}, {"ASCII", true}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := testCtx(tc.ascii, false, 0)
			out := renderSparkline(ctx, ctx.Snap.Hashrates, 12)
			if w := ansi.StringWidth(out); w != 12 {
				t.Fatalf("width %d", w)
			}
			if tc.ascii {
				noHighBytes(t, out)
			}
			golden.RequireEqual(t, ansi.Strip(out))
		})
	}
}

func TestSparklineBreathingOnlyWithMotion(t *testing.T) {
	still := testCtx(false, false, 0)
	a := renderSparkline(still, still.Snap.Hashrates, 12)
	still.Frame = 1
	if b := renderSparkline(still, still.Snap.Hashrates, 12); a != b {
		t.Fatal("static sparkline changed with the frame")
	}
	if renderSparkline(testCtx(false, true, 0), nil, 5) != "" {
		t.Fatal("empty series must render nothing")
	}
}
