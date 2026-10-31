package termcap

import (
	"reflect"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDetectGlyphs(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		f    Flags
		want Glyphs
	}{
		{"utf8 lang", map[string]string{"TERM": "xterm-256color", "LANG": "en_US.UTF-8"}, Flags{}, UTF8},
		{"utf8 no dash", map[string]string{"TERM": "xterm", "LANG": "C.utf8"}, Flags{}, UTF8},
		{"LANG=C", map[string]string{"TERM": "xterm", "LANG": "C"}, Flags{}, ASCII},
		{"no locale", map[string]string{"TERM": "xterm"}, Flags{}, ASCII},
		{"LC_ALL wins", map[string]string{"TERM": "xterm", "LC_ALL": "C", "LANG": "en_US.UTF-8"}, Flags{}, ASCII},
		{"LC_CTYPE before LANG", map[string]string{"TERM": "xterm", "LC_CTYPE": "en_US.UTF-8", "LANG": "C"}, Flags{}, UTF8},
		{"linux", map[string]string{"TERM": "linux", "LANG": "en_US.UTF-8"}, Flags{}, ASCII},
		{"dumb", map[string]string{"TERM": "dumb", "LANG": "en_US.UTF-8"}, Flags{}, ASCII},
		{"empty TERM", map[string]string{"LANG": "en_US.UTF-8"}, Flags{}, ASCII},
		{"flag", map[string]string{"TERM": "xterm", "LANG": "en_US.UTF-8"}, Flags{ASCII: true}, ASCII},
		{"env override", map[string]string{"TERM": "xterm", "LANG": "en_US.UTF-8", "CASHTOP_ASCII": "1"}, Flags{}, ASCII},
	}
	for _, tc := range cases {
		if got := Detect(env(tc.env), tc.f).Glyphs; got != tc.want {
			t.Errorf("%s: got %v want %v", tc.name, got, tc.want)
		}
	}
}

func TestDetectTerminalAndColor(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want Terminal
	}{
		{map[string]string{"LC_TERMINAL": "iTerm2"}, ITerm2},
		{map[string]string{"TERM_PROGRAM": "WezTerm"}, WezTerm},
		{map[string]string{"WEZTERM_PANE": "3"}, WezTerm},
		{map[string]string{"KITTY_WINDOW_ID": "1"}, Kitty},
		{map[string]string{"GHOSTTY_RESOURCES_DIR": "/x"}, Ghostty},
		{map[string]string{"VTE_VERSION": "7600"}, VTE},
		{map[string]string{"TERM_PROGRAM": "Apple_Terminal"}, AppleTerminal},
		{map[string]string{}, Unknown},
	}
	for _, tc := range cases {
		if got := Detect(env(tc.env), Flags{}).Terminal; got != tc.want {
			t.Errorf("%v: got %v want %v", tc.env, got, tc.want)
		}
	}
	if !Detect(env(map[string]string{"NO_COLOR": "1"}), Flags{}).NoColor {
		t.Error("NO_COLOR ignored")
	}
	if !Detect(env(nil), Flags{NoColor: true}).NoColor {
		t.Error("--no-color ignored")
	}
	if !Detect(env(map[string]string{"TMUX": "/tmp/t,1,0"}), Flags{}).InTmux {
		t.Error("tmux not detected")
	}
}

func TestXTVersion(t *testing.T) {
	base := Detect(env(nil), Flags{})
	for in, want := range map[string]Terminal{
		"iTerm2 3.5.0": ITerm2, "WezTerm 20240203": WezTerm, "kitty(0.32.2)": Kitty,
		"ghostty 1.0.0": Ghostty, "VTE 7600": VTE, "foot(1.16)": Other, "tmux 3.4": Unknown, "": Unknown,
	} {
		if got := base.WithXTVersion(in).Terminal; got != want {
			t.Errorf("%q: got %v want %v", in, got, want)
		}
	}
}

func TestNotifications(t *testing.T) {
	c := Caps{Notify: NotifyAuto}
	if got := Notifications(c, "Block", "x"); !reflect.DeepEqual(got, []string{"\a"}) {
		t.Errorf("auto+unknown: %q", got)
	}
	c.Quiet = true
	if got := Notifications(c, "Block", "x"); len(got) != 0 {
		t.Errorf("quiet: %q", got)
	}
	c = Caps{Notify: NotifyOff, Terminal: ITerm2}
	if got := Notifications(c, "a", "b"); len(got) != 0 {
		t.Errorf("off: %q", got)
	}
	c = Caps{Notify: NotifyAuto, Terminal: ITerm2}
	want := []string{"\a", "\x1b]9;Block: found\a", "\x1b]1337;RequestAttention=fireworks\a"}
	if got := Notifications(c, "Block", "found"); !reflect.DeepEqual(got, want) {
		t.Errorf("iterm2: %q", got)
	}
	c = Caps{Notify: NotifyOSC777, Terminal: Unknown}
	if got := Notifications(c, "T", "B"); !reflect.DeepEqual(got, []string{"\x1b]777;notify;T;B\a"}) {
		t.Errorf("osc777 forced: %q", got)
	}
	c = Caps{Notify: NotifyBell, InTmux: true, Terminal: ITerm2}
	if got := Notifications(c, "T", "B"); !reflect.DeepEqual(got, []string{"\a"}) {
		t.Errorf("bell: %q", got)
	}
}

func TestTmuxWrapExact(t *testing.T) {
	c := Caps{Notify: NotifyOSC9, InTmux: true}
	got := Notifications(c, "Hi", "")
	want := "\x1bPtmux;\x1b\x1b]9;Hi\a\x1b\\"
	if len(got) != 1 || got[0] != want {
		t.Errorf("got %q want %q", got, want)
	}
}

func TestSanitize(t *testing.T) {
	if got := Sanitize("a\x1b]0;x\x07b;c\nd"); strings.ContainsAny(got, "\x1b\x07;\n") {
		t.Errorf("unsafe: %q", got)
	}
}

func TestTmuxHintOnce(t *testing.T) {
	var shown bool
	c := Caps{InTmux: true, Notify: NotifyAuto}
	if c.TmuxHint(&shown) == "" || c.TmuxHint(&shown) != "" {
		t.Error("hint should fire exactly once")
	}
	if (Caps{Notify: NotifyAuto}).TmuxHint(new(bool)) != "" {
		t.Error("hint outside tmux")
	}
}

func TestGlyphs(t *testing.T) {
	a, u := Caps{Glyphs: ASCII}, Caps{Glyphs: UTF8}
	if a.SparkLevel(0) != '_' || a.SparkLevel(1) != '#' || u.SparkLevel(1) != '█' || u.SparkLevel(0) != '▁' {
		t.Error("spark ramp")
	}
	if a.BrailleOK() || a.BrailleFallback('⣿') != '.' || u.BrailleFallback('⣿') != '█' {
		t.Error("braille")
	}
	if a.Box().TL != "+" || u.Box().TL != "┌" {
		t.Error("box")
	}
}

func TestProgramOptionsAndWriter(t *testing.T) {
	if n := len(ProgramOptions(Caps{}, nil)); n != 1 {
		t.Errorf("opts=%d", n)
	}
	if n := len(ProgramOptions(Caps{NoColor: true}, []string{"A=1"})); n != 3 {
		t.Errorf("opts=%d", n)
	}
	w := &CountingWriter{}
	w.Write([]byte("hello"))
	w.Write([]byte("!!"))
	if w.Bytes() != 7 {
		t.Errorf("bytes=%d", w.Bytes())
	}
	w.Reset()
	if w.Bytes() != 0 {
		t.Error("reset")
	}
}
