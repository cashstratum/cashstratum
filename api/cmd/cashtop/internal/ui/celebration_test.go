package ui

import (
	"os"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"cashstratumapi/cmd/cashtop/internal/termcap"
)

func celebCtx(ascii, motion bool) *Context {
	caps := termcap.Caps{NoColor: true}
	if ascii {
		caps.Glyphs = termcap.ASCII
	}
	return &Context{
		Snap:   &Snapshot{Blocks: make([]BlockRow, 41), Workers: []WorkerRow{{Name: "bch1qexample.rig1", UserAgent: "bitaxe/ultra"}}},
		Now:    time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC),
		Caps:   caps,
		Theme:  Mono(),
		Motion: motion,
		sym:    symbolsFor(caps),
	}
}

func celebBlock() BlockFoundMsg {
	return BlockFoundMsg{Replay: true, Block: BlockRow{Height: 974153, Finder: "bch1qexample.rig1", Luck: 63.2, Reward: 3_1250_0000}}
}

func celebTick(c *celebration, ctx *Context) {
	ctx.Frame++
	c.Update(TickMsg{Frame: ctx.Frame}, ctx)
}

func checkGolden(t *testing.T, name, got string) {
	t.Helper()
	path := "testdata/" + name + ".golden"
	if os.Getenv("UPDATE_GOLDEN") != "" {
		_ = os.MkdirAll("testdata", 0o755)
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden %s (UPDATE_GOLDEN=1): %v", path, err)
	}
	if string(want) != got {
		t.Errorf("golden %s differs:\n%s", name, got)
	}
}

func TestCelebrationGolden(t *testing.T) {
	ctx := celebCtx(false, true)
	c := &celebration{}
	c.Update(celebBlock(), ctx)
	if !c.Active() {
		t.Fatal("not active")
	}
	checkGolden(t, "TestCelebrationFrame0", ansi.Strip(c.Render(ctx, 120, 36)))
	for i := 0; i < 6; i++ {
		celebTick(c, ctx)
	}
	checkGolden(t, "TestCelebrationFrame6", ansi.Strip(c.Render(ctx, 120, 36)))
}

func TestCelebrationBoundedBytes(t *testing.T) {
	ctx := celebCtx(false, true)
	c := &celebration{}
	c.Update(celebBlock(), ctx)
	total, ticks := len(c.Render(ctx, 120, 36)), 0
	for c.Active() {
		celebTick(c, ctx)
		total += len(c.Render(ctx, 120, 36))
		if ticks++; ticks > celebrationTicks+1 {
			t.Fatal("did not decay")
		}
		if len(c.particles) > maxParticles {
			t.Fatalf("particles %d over cap", len(c.particles))
		}
	}
	if ticks != celebrationTicks {
		t.Errorf("ticks = %d, want %d", ticks, celebrationTicks)
	}
	if total > 200_000 {
		t.Errorf("rendered %d bytes, cap 200000", total)
	}
}

func TestCelebrationASCIIAndStatic(t *testing.T) {
	ctx := celebCtx(true, true)
	c := &celebration{}
	c.Update(celebBlock(), ctx)
	first := c.Render(ctx, 120, 36)
	for i := 0; i < 5; i++ {
		celebTick(c, ctx)
		out := c.Render(ctx, 120, 36)
		for _, b := range []byte(out) {
			if b > 0x7F {
				t.Fatalf("non-ASCII byte in %q", out)
			}
		}
		if out != first {
			t.Fatal("static frame changed under ASCII")
		}
	}
	if !strings.Contains(first, "###") {
		t.Error("ASCII digits missing")
	}
}

func TestCelebrationKeyDismiss(t *testing.T) {
	ctx := celebCtx(false, true)
	c := &celebration{}
	c.Update(celebBlock(), ctx)
	c.Update(tea.KeyPressMsg{Code: 'x', Text: "x"}, ctx)
	if c.Active() {
		t.Error("key did not dismiss")
	}
}

func TestCelebrationDeterministic(t *testing.T) {
	run := func() string {
		ctx := celebCtx(false, true)
		c := &celebration{}
		c.Update(celebBlock(), ctx)
		for i := 0; i < 9; i++ {
			celebTick(c, ctx)
		}
		return c.Render(ctx, 120, 36)
	}
	if run() != run() {
		t.Error("non-deterministic")
	}
}
