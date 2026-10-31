package celebrate

import (
	"strings"
	"testing"

	"cashstratumapi/cmd/cashtop/internal/events"
	"cashstratumapi/cmd/cashtop/internal/termcap"
)

var blk = events.BlockFound{Height: 900123, Finder: "addr.rig1", Reward: 3.125, HasReward: true}

func hasOSC(steps []Step) bool {
	for _, s := range steps {
		if strings.Contains(s.Data, "\x1b]") {
			return true
		}
	}
	return false
}

func count(steps []Step, d string) (n int) {
	for _, s := range steps {
		if s.Data == d {
			n++
		}
	}
	return
}

func TestB4QuietEmitsNothing(t *testing.T) {
	c := termcap.Caps{Notify: termcap.NotifyAuto, Terminal: termcap.ITerm2, Quiet: true}
	if p := Plan(c, blk); len(p) != 0 {
		t.Fatalf("%q", p)
	}
	if Cmd(c, blk) != nil {
		t.Fatal("cmd not nil")
	}
	off := termcap.Caps{Notify: termcap.NotifyOff, Terminal: termcap.ITerm2}
	if Cmd(off, blk) != nil {
		t.Fatal("notify off emitted")
	}
}

func TestThreeBellsAndOSC(t *testing.T) {
	c := termcap.Caps{Notify: termcap.NotifyAuto, Terminal: termcap.Ghostty}
	p := Plan(c, blk)
	if count(p, "\a") != 3 || !hasOSC(p) {
		t.Fatalf("%q", p)
	}
	var bells []Step
	for _, s := range p {
		if s.Data == "\a" {
			bells = append(bells, s)
		}
	}
	if bells[1].Delay != 300*1e6 || bells[2].Delay != 300*1e6 {
		t.Fatalf("%v", bells)
	}
	if !strings.Contains(strings.Join(dataOf(p), ""), "Block 900123 found!") || !strings.Contains(strings.Join(dataOf(p), ""), "addr.rig1") {
		t.Fatalf("%q", p)
	}
	if Cmd(c, blk) == nil {
		t.Fatal("nil cmd")
	}
}

func TestBellOnlyMode(t *testing.T) {
	p := Plan(termcap.Caps{Notify: termcap.NotifyBell}, blk)
	if count(p, "\a") != 3 || hasOSC(p) {
		t.Fatalf("%q", p)
	}
}

func TestB4TmuxWrapsOSC(t *testing.T) {
	c := termcap.Caps{Notify: termcap.NotifyAuto, Terminal: termcap.Ghostty, InTmux: true}
	p := Plan(c, blk)
	var wrapped bool
	for _, s := range p {
		if strings.HasPrefix(s.Data, "\x1bPtmux;") && strings.HasSuffix(s.Data, "\x1b\\") && strings.Contains(s.Data, "\x1b\x1b]777;notify;") {
			wrapped = true
		}
		if strings.HasPrefix(s.Data, "\x1b]") {
			t.Fatalf("bare OSC under tmux: %q", s.Data)
		}
	}
	if !wrapped || count(p, "\a") != 3 {
		t.Fatalf("%q", p)
	}
}

func dataOf(p []Step) []string {
	var o []string
	for _, s := range p {
		o = append(o, s.Data)
	}
	return o
}
