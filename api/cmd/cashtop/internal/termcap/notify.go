package termcap

import "strings"

const (
	esc = "\x1b"
	bel = "\x07"
)

// BEL returns the terminal bell.
func BEL() string { return bel }

// Sanitize strips control characters and the ';' separator so text cannot
// terminate or extend an escape sequence.
func Sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) || r == ';' {
			if r == '\n' || r == '\t' || r == ' ' {
				b.WriteByte(' ')
			}
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}

// OSC9 builds a desktop-notification sequence.
func OSC9(text string) string { return esc + "]9;" + Sanitize(text) + bel }

// OSC777 builds a titled desktop-notification sequence.
func OSC777(title, body string) string {
	return esc + "]777;notify;" + Sanitize(title) + ";" + Sanitize(body) + bel
}

// ITerm2Fireworks builds the iTerm2 attention request.
func ITerm2Fireworks() string { return esc + "]1337;RequestAttention=fireworks" + bel }

// TmuxWrap wraps seq in a tmux DCS passthrough.
func TmuxWrap(seq string) string {
	return esc + "Ptmux;" + strings.ReplaceAll(seq, esc, esc+esc) + esc + "\\"
}

// Notifications returns the sequences to emit for an event, in order. The
// caller sends each through tea.Raw. Nothing is returned when quiet or off.
func Notifications(c Caps, title, body string) []string {
	if c.Quiet || c.Notify == NotifyOff {
		return nil
	}
	var seqs []string // OSC sequences, wrapped below
	var out []string
	text := Sanitize(title)
	if b := Sanitize(body); b != "" {
		text += ": " + b
	}
	switch c.Notify {
	case NotifyBell:
		return []string{bel}
	case NotifyOSC9:
		seqs = []string{OSC9(text)}
	case NotifyOSC777:
		seqs = []string{OSC777(title, body)}
	default: // auto
		out = append(out, bel)
		switch c.Terminal {
		case ITerm2:
			seqs = []string{OSC9(text), ITerm2Fireworks()}
		case WezTerm:
			seqs = []string{OSC9(text)}
		case Ghostty, VTE:
			seqs = []string{OSC777(title, body)}
		}
	}
	for _, s := range seqs {
		if c.InTmux {
			s = TmuxWrap(s)
		}
		out = append(out, s)
	}
	return out
}

// TmuxHint returns a one-time hint about tmux passthrough, or "" when it does
// not apply or was already given.
func (c Caps) TmuxHint(shown *bool) string {
	if !c.InTmux || c.Quiet || c.Notify == NotifyOff || c.Notify == NotifyBell || shown == nil || *shown {
		return ""
	}
	*shown = true
	return "tmux: desktop notifications need `set -g allow-passthrough on`"
}
