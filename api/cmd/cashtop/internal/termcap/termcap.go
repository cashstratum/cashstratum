// Package termcap detects what the host terminal can render and builds the
// escape sequences cashtop emits for notifications.
package termcap

import (
	"io"
	"strings"
	"sync/atomic"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

// Glyphs selects the character repertoire used for drawing.
type Glyphs int

const (
	UTF8 Glyphs = iota
	ASCII
)

// Terminal identifies the terminal emulator when it is known.
type Terminal int

const (
	Unknown Terminal = iota
	ITerm2
	WezTerm
	Kitty
	Ghostty
	VTE
	AppleTerminal
	Other
)

func (t Terminal) String() string {
	switch t {
	case ITerm2:
		return "iTerm2"
	case WezTerm:
		return "WezTerm"
	case Kitty:
		return "kitty"
	case Ghostty:
		return "Ghostty"
	case VTE:
		return "VTE"
	case AppleTerminal:
		return "Apple Terminal"
	case Other:
		return "other"
	}
	return "unknown"
}

// Notify is the notification mode.
type Notify string

const (
	NotifyAuto   Notify = "auto"
	NotifyBell   Notify = "bell"
	NotifyOSC9   Notify = "osc9"
	NotifyOSC777 Notify = "osc777"
	NotifyOff    Notify = "off"
)

// ParseNotify validates a --notify value; empty means auto.
func ParseNotify(s string) (Notify, bool) {
	switch n := Notify(strings.ToLower(strings.TrimSpace(s))); n {
	case "":
		return NotifyAuto, true
	case NotifyAuto, NotifyBell, NotifyOSC9, NotifyOSC777, NotifyOff:
		return n, true
	}
	return NotifyAuto, false
}

// Flags are the command-line switches that influence detection.
type Flags struct {
	ASCII   bool
	NoColor bool
	Quiet   bool
	Notify  string
}

// Caps is the detected capability set.
type Caps struct {
	Glyphs   Glyphs
	NoColor  bool
	Terminal Terminal
	InTmux   bool
	Notify   Notify
	Quiet    bool
}

// Detect inspects the environment (via getenv) and flags.
func Detect(getenv func(string) string, f Flags) Caps {
	c := Caps{Glyphs: UTF8, Notify: NotifyAuto, Quiet: f.Quiet}
	if n, ok := ParseNotify(f.Notify); ok {
		c.Notify = n
	}

	term := getenv("TERM")
	locale := ""
	for _, k := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if v := getenv(k); v != "" {
			locale = strings.ToLower(v)
			break
		}
	}
	if !strings.Contains(locale, "utf-8") && !strings.Contains(locale, "utf8") {
		c.Glyphs = ASCII
	}
	if term == "" || term == "linux" || term == "dumb" || f.ASCII || getenv("CASHTOP_ASCII") == "1" {
		c.Glyphs = ASCII
	}

	c.NoColor = f.NoColor || getenv("NO_COLOR") != "" || term == "dumb"
	c.InTmux = getenv("TMUX") != ""
	c.Terminal = identifyEnv(getenv)
	return c
}

func identifyEnv(getenv func(string) string) Terminal {
	if strings.EqualFold(getenv("LC_TERMINAL"), "iTerm2") {
		return ITerm2
	}
	switch strings.ToLower(getenv("TERM_PROGRAM")) {
	case "iterm.app":
		return ITerm2
	case "wezterm":
		return WezTerm
	case "ghostty":
		return Ghostty
	case "apple_terminal":
		return AppleTerminal
	case "kitty":
		return Kitty
	}
	for _, k := range []string{"WEZTERM_EXECUTABLE", "WEZTERM_PANE", "WEZTERM_UNIX_SOCKET"} {
		if getenv(k) != "" {
			return WezTerm
		}
	}
	if getenv("KITTY_WINDOW_ID") != "" {
		return Kitty
	}
	for _, k := range []string{"GHOSTTY_RESOURCES_DIR", "GHOSTTY_BIN_DIR"} {
		if getenv(k) != "" {
			return Ghostty
		}
	}
	if getenv("VTE_VERSION") != "" {
		return VTE
	}
	return Unknown
}

// WithXTVersion upgrades the terminal identity from an XTVERSION reply such
// as "iTerm2 3.5.0" or "WezTerm 20240203". An unrecognised non-empty reply
// still marks the terminal as identified (Other); tmux replies are ignored
// because they describe the multiplexer, not the outer terminal.
func (c Caps) WithXTVersion(v string) Caps {
	l := strings.ToLower(strings.TrimSpace(v))
	switch {
	case l == "":
		return c
	case strings.HasPrefix(l, "tmux"), strings.HasPrefix(l, "screen"):
		return c
	case strings.HasPrefix(l, "iterm2"):
		c.Terminal = ITerm2
	case strings.HasPrefix(l, "wezterm"):
		c.Terminal = WezTerm
	case strings.HasPrefix(l, "kitty"):
		c.Terminal = Kitty
	case strings.HasPrefix(l, "ghostty"):
		c.Terminal = Ghostty
	case strings.HasPrefix(l, "vte"), strings.HasPrefix(l, "gnome-terminal"):
		c.Terminal = VTE
	default:
		if c.Terminal == Unknown {
			c.Terminal = Other
		}
	}
	return c
}

// Identified reports whether the terminal emulator is known.
func (c Caps) Identified() bool { return c.Terminal != Unknown }

// ProgramOptions returns the bubbletea options derived from the capabilities.
func ProgramOptions(c Caps, env []string) []tea.ProgramOption {
	opts := []tea.ProgramOption{tea.WithFPS(4)}
	if env != nil {
		opts = append(opts, tea.WithEnvironment(env))
	}
	if c.NoColor {
		opts = append(opts, tea.WithColorProfile(colorprofile.Ascii))
	}
	return opts
}

// CountingWriter counts bytes written to the wrapped writer.
type CountingWriter struct {
	W io.Writer
	n atomic.Int64
}

func (c *CountingWriter) Write(p []byte) (int, error) {
	if c.W == nil {
		c.n.Add(int64(len(p)))
		return len(p), nil
	}
	n, err := c.W.Write(p)
	c.n.Add(int64(n))
	return n, err
}

// Bytes returns the total bytes written so far.
func (c *CountingWriter) Bytes() int64 { return c.n.Load() }

// Reset zeroes the counter.
func (c *CountingWriter) Reset() { c.n.Store(0) }
