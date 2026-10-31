package ui_test

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/exp/golden"
	"github.com/charmbracelet/x/exp/teatest/v2"

	"cashstratumapi/cmd/cashtop/internal/demo"
	"cashstratumapi/cmd/cashtop/internal/termcap"
	"cashstratumapi/cmd/cashtop/internal/ui"
)

var fixedNow = time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)

const goldenW, goldenH = 120, 36

func utf8Env(k string) string {
	switch k {
	case "LANG":
		return "en_US.UTF-8"
	case "TERM":
		return "xterm-256color"
	}
	return ""
}

func envFrom(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func newModel(o demo.Options, caps termcap.Caps) *ui.Model {
	o.Seed = 1
	o.Now = fixedNow
	return ui.New(demo.New(o), ui.Options{Caps: caps, Now: func() time.Time { return fixedNow }})
}

func key(s string) tea.KeyPressMsg {
	switch s {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	}
	r := []rune(s)
	return tea.KeyPressMsg{Code: r[0], Text: s}
}

// runTea drives the model through a real program at the golden size and
// returns the final screen with escapes stripped.
func runTea(t *testing.T, m *ui.Model, keys ...string) string {
	t.Helper()
	tm := teatest.NewTestModel(t, m,
		teatest.WithInitialTermSize(goldenW, goldenH),
		teatest.WithProgramOptions(tea.WithColorProfile(colorprofile.Ascii)))
	for _, k := range keys {
		tm.Send(key(k))
	}
	if err := tm.Quit(); err != nil {
		t.Fatal(err)
	}
	fm := tm.FinalModel(t, teatest.WithFinalTimeout(5*time.Second)).(*ui.Model)
	return ansi.Strip(fm.Render())
}

// drive feeds messages straight into the model, running returned commands
// that complete immediately (drill-down lookups).
func drive(m *ui.Model, w, h int, keys ...string) *ui.Model {
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	for _, k := range keys {
		_, cmd := m.Update(key(k))
		run(m, cmd)
	}
	return m
}

func run(m *ui.Model, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			run(m, c)
		}
	case nil:
	default:
		_, next := m.Update(msg)
		run(m, next)
	}
}

func checkScreen(t *testing.T, s string, w, h int) {
	t.Helper()
	lines := strings.Split(s, "\n")
	if len(lines) != h {
		t.Fatalf("got %d lines, want %d", len(lines), h)
	}
	for i, l := range lines {
		if got := ansi.StringWidth(l); got != w {
			t.Fatalf("line %d is %d cells wide, want %d: %q", i, got, w, l)
		}
	}
}

func TestGoldenTabs(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{})
	cases := []struct {
		name string
		opt  demo.Options
		keys []string
	}{
		{"Dashboard", demo.Options{}, nil},
		{"Workers", demo.Options{}, []string{"2"}},
		{"Blocks", demo.Options{}, []string{"3", "j"}},
		{"Feed", demo.Options{}, []string{"4"}},
		{"Network", demo.Options{}, []string{"5"}},
		{"DashboardZeroBlocks", demo.Options{NoBlocks: true}, nil},
		{"BlocksZeroBlocks", demo.Options{NoBlocks: true}, []string{"3"}},
		{"DashboardZeroWorkers", demo.Options{NoWorkers: true, NoBlocks: true}, nil},
		{"WorkersZeroWorkers", demo.Options{NoWorkers: true}, []string{"2"}},
		{"Help", demo.Options{}, []string{"?"}},
		{"SearchWorker", demo.Options{}, []string{"/", "r", "i", "g", "0", "3", "enter"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := runTea(t, newModel(c.opt, caps), c.keys...)
			checkScreen(t, out, goldenW, goldenH)
			golden.RequireEqual(t, out)
		})
	}
}

func TestGoldenDrillDown(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{})
	m := drive(newModel(demo.Options{}, caps), goldenW, goldenH, "3", "enter")
	out := ansi.Strip(m.Render())
	checkScreen(t, out, goldenW, goldenH)
	if !strings.Contains(out, "WIN") {
		t.Fatal("winning share not highlighted")
	}
	golden.RequireEqual(t, out)
}

func TestGoldenHeightSearch(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{})
	p := demo.New(demo.Options{Seed: 1, Now: fixedNow})
	h := p.Snapshot().Blocks[2].Height
	keys := []string{"/"}
	for _, r := range itoa(h) {
		keys = append(keys, string(r))
	}
	m := drive(newModel(demo.Options{}, caps), goldenW, goldenH, append(keys, "enter")...)
	out := ansi.Strip(m.Render())
	if m.Tab() != ui.TabBlocks || !strings.Contains(out, "Height "+itoa(h)) || !strings.Contains(out, "WIN") {
		t.Fatalf("height search did not open the drill-down:\n%s", out)
	}
}

func TestGoldenAddress(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{})
	m := drive(newModel(demo.Options{}, caps), goldenW, goldenH, "2", "enter")
	out := ansi.Strip(m.Render())
	checkScreen(t, out, goldenW, goldenH)
	golden.RequireEqual(t, out)
}

func TestGoldenTooSmall(t *testing.T) {
	m := drive(newModel(demo.Options{}, termcap.Detect(utf8Env, termcap.Flags{})), 70, 20)
	out := ansi.Strip(m.Render())
	checkScreen(t, out, 70, 20)
	golden.RequireEqual(t, out)
}

func itoa(n int) string {
	var b []byte
	for n > 0 || len(b) == 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// TestASCIIOnly covers LANG=C and TERM=linux: no byte above 0x7F may be
// emitted on any screen.
func TestASCIIOnly(t *testing.T) {
	envs := map[string]map[string]string{
		"LANG=C":     {"LANG": "C", "TERM": "xterm-256color"},
		"TERM=linux": {"LANG": "en_US.UTF-8", "TERM": "linux"},
	}
	screens := [][]string{nil, {"2"}, {"3"}, {"4"}, {"5"}, {"?"}, {"3", "enter"}, {"2", "enter"}, {"/", "x"}}
	for name, env := range envs {
		caps := termcap.Detect(envFrom(env), termcap.Flags{})
		if caps.Glyphs != termcap.ASCII {
			t.Fatalf("%s: glyphs not ASCII", name)
		}
		for _, opt := range []demo.Options{{}, {NoBlocks: true, NoWorkers: true}} {
			for _, keys := range screens {
				m := drive(newModel(opt, caps), goldenW, goldenH, keys...)
				out := m.Render()
				for i := 0; i < len(out); i++ {
					if out[i] > 0x7F {
						t.Fatalf("%s keys %v: non-ASCII byte 0x%02x at %d in %q", name, keys, out[i], i, ansi.Strip(out))
					}
				}
			}
		}
	}
}

// TestThemesSameText checks colour never changes the text: the stripped
// cashgreen screen equals the stripped mono screen.
func TestThemesSameText(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{})
	mono := caps
	mono.NoColor = true
	for _, keys := range [][]string{nil, {"2"}, {"3"}, {"4"}, {"5"}} {
		a := ansi.Strip(drive(newModel(demo.Options{}, caps), goldenW, goldenH, keys...).Render())
		b := ansi.Strip(drive(newModel(demo.Options{}, mono), goldenW, goldenH, keys...).Render())
		// The footer names the theme; everything above it must match.
		a, b = a[:strings.LastIndexByte(a, '\n')], b[:strings.LastIndexByte(b, '\n')]
		if a != b {
			t.Fatalf("keys %v: themes differ in text", keys)
		}
	}
	if v := newModel(demo.Options{}, mono).View(); v.BackgroundColor != nil {
		t.Fatal("mono must not set a background colour")
	}
}

func TestResizeDebounce(t *testing.T) {
	m := drive(newModel(demo.Options{}, termcap.Detect(utf8Env, termcap.Flags{})), goldenW, goldenH)
	_, c1 := m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	_, c2 := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	if w := ansi.StringWidth(strings.SplitN(m.Render(), "\n", 2)[0]); w != goldenW {
		t.Fatalf("resize applied before the debounce: width %d", w)
	}
	start := time.Now()
	run(m, c1) // superseded: no change
	if w := ansi.StringWidth(strings.SplitN(m.Render(), "\n", 2)[0]); w != goldenW {
		t.Fatalf("stale resize applied: width %d", w)
	}
	run(m, c2)
	if d := time.Since(start); d < 50*time.Millisecond || d > 2*time.Second {
		t.Fatalf("debounce took %v", d)
	}
	if w := ansi.StringWidth(strings.SplitN(m.Render(), "\n", 2)[0]); w != 100 {
		t.Fatalf("resize not applied: width %d", w)
	}
}

// TestIdleByteBudget runs the real renderer for a few seconds of 250 ms
// ticks with no data change and checks the bytes written stay under 1 KB/s.
func TestIdleByteBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("runs for several seconds")
	}
	for _, motion := range []bool{true, false} {
		cw := &termcap.CountingWriter{}
		caps := termcap.Detect(utf8Env, termcap.Flags{})
		m := ui.New(demo.New(demo.Options{Seed: 1, Now: time.Now()}),
			ui.Options{Caps: caps, Motion: motion, PollInterval: 500 * time.Millisecond})
		opts := append(termcap.ProgramOptions(caps, []string{"TERM=xterm-256color"}),
			tea.WithOutput(cw), tea.WithInput(nil), tea.WithoutSignals(),
			tea.WithWindowSize(goldenW, goldenH), tea.WithColorProfile(colorprofile.TrueColor))
		p := tea.NewProgram(m, opts...)
		done := make(chan error, 1)
		go func() { _, err := p.Run(); done <- err }()

		waitUntil(time.Now().Add(time.Second))
		first := cw.Bytes()
		cw.Reset()
		const window = 3 * time.Second
		waitUntil(time.Now().Add(window))
		idle := cw.Bytes()
		p.Quit()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		rate := float64(idle) / window.Seconds()
		t.Logf("motion=%v first frame %d B, idle %d B over %v = %.0f B/s", motion, first, idle, window, rate)
		if first == 0 {
			t.Fatal("nothing was rendered")
		}
		if rate >= 1024 {
			t.Fatalf("idle output %.0f B/s, budget 1024 B/s", rate)
		}
	}
}

func waitUntil(deadline time.Time) {
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
}

func typeCmd(c string) []string {
	keys := []string{":"}
	for _, r := range c {
		keys = append(keys, string(r))
	}
	return append(keys, "enter")
}

func TestColonCommands(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{Quiet: true})
	for _, opt := range []demo.Options{{}, {NoBlocks: true}} {
		m := newModel(opt, caps)
		before := drive(m, goldenW, goldenH).Render()
		m.Update(key(":"))
		if !strings.Contains(ansi.Strip(m.Render()), " : _") {
			t.Fatal("no command prompt")
		}
		m.Update(key("esc"))
		drive(m, goldenW, goldenH, typeCmd("test-celebrate")...)
		if m.Render() == before {
			t.Fatalf("%+v: :test-celebrate did not take over the screen", opt)
		}
	}

	m := drive(newModel(demo.Options{}, caps), goldenW, goldenH, typeCmd("frobnicate")...)
	if !strings.Contains(ansi.Strip(m.Render()), "unknown command :frobnicate") {
		t.Fatal("unknown command not reported")
	}
	m = drive(newModel(demo.Options{}, caps), goldenW, goldenH, ":", "q")
	_, cmd := m.Update(key("enter"))
	if cmd == nil {
		t.Fatal(":q returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal(":q does not quit")
	}
}

func TestReplayBlock(t *testing.T) {
	p := demo.New(demo.Options{Seed: 1, Now: fixedNow})
	if b := ui.ReplayBlock(p.Snapshot(), fixedNow); b.Height != p.Snapshot().Blocks[0].Height {
		t.Fatalf("replay block %d", b.Height)
	}
	e := demo.New(demo.Options{Seed: 1, Now: fixedNow, NoBlocks: true}).Snapshot()
	if b := ui.ReplayBlock(e, fixedNow); b.Height != e.Pool.Height || !b.Time.Equal(fixedNow) {
		t.Fatalf("stand-in block %+v", b)
	}
}

func TestGoldenDashboard80x24(t *testing.T) {
	caps := termcap.Detect(utf8Env, termcap.Flags{})
	m := drive(newModel(demo.Options{}, caps), 80, 24)
	out := ansi.Strip(m.Render())
	checkScreen(t, out, 80, 24)
	golden.RequireEqual(t, out)
}
