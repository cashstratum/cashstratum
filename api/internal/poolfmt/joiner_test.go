package poolfmt

import (
	"strings"
	"testing"
)

func stampAt(ms int) string {
	return "[2026-10-30 07:25:00." + string(rune('0'+ms/100%10)) + string(rune('0'+ms/10%10)) + string(rune('0'+ms%10)) + "] "
}

func feedAll(j *Joiner, lines []string) []Joined {
	var out []Joined
	off := int64(0)
	for _, l := range lines {
		out = append(out, j.Feed(l, off)...)
		off += int64(len(l)) + 1
	}
	return out
}

func TestJoinerTable(t *testing.T) {
	full := strings.Repeat("a", ChunkBody)
	full2 := strings.Repeat("b", ChunkBody)
	tests := []struct {
		name  string
		in    []string
		out   []string
		offs  []int64
		flush []string
	}{
		{"plain", []string{stampAt(1) + "hello", stampAt(2) + "world"}, []string{stampAt(1) + "hello", stampAt(2) + "world"}, []int64{0, int64(len(stampAt(1)+"hello")) + 1}, nil},
		{"exact 510 then unrelated", []string{stampAt(1) + full, "[2026-10-30 07:25:01.500] next"}, []string{stampAt(1) + full, "[2026-10-30 07:25:01.500] next"}, []int64{0, int64(len(stampAt(1)+full)) + 1}, nil},
		{"exact 510 held until flush", []string{stampAt(1) + full}, nil, nil, []string{stampAt(1) + full}},
		{"two chunks", []string{stampAt(1) + full, stampAt(1) + "tail"}, []string{stampAt(1) + full + "tail"}, []int64{0}, nil},
		{"three chunks", []string{stampAt(1) + full, stampAt(2) + full2, stampAt(2) + "end", stampAt(3) + "x"}, []string{stampAt(1) + full + full2 + "end", stampAt(3) + "x"}, []int64{0, int64(len(stampAt(1)+full)+len(stampAt(2)+full2)+len(stampAt(2)+"end")) + 3}, nil},
		{"gap exactly 50 joins", []string{stampAt(0) + full, stampAt(50) + "z"}, []string{stampAt(0) + full + "z"}, []int64{0}, nil},
		{"gap 51 does not join", []string{stampAt(0) + full, stampAt(51) + "z"}, []string{stampAt(0) + full, stampAt(51) + "z"}, nil, nil},
		{"earlier stamp does not join", []string{stampAt(10) + full, stampAt(5) + "z"}, []string{stampAt(10) + full, stampAt(5) + "z"}, nil, nil},
		{"unstamped follower", []string{stampAt(1) + full, "no stamp"}, []string{stampAt(1) + full, "no stamp"}, nil, nil},
		{"chain gap measured from last chunk", []string{stampAt(0) + full, stampAt(40) + full2, stampAt(80) + "e"}, []string{stampAt(0) + full + full2 + "e"}, []int64{0}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var j Joiner
			got := feedAll(&j, tc.in)
			if len(got) != len(tc.out) {
				t.Fatalf("got %d lines, want %d: %v", len(got), len(tc.out), got)
			}
			for i, g := range got {
				if g.Line != tc.out[i] {
					t.Errorf("line %d = %q, want %q", i, g.Line, tc.out[i])
				}
				if i < len(tc.offs) && g.Offset != tc.offs[i] {
					t.Errorf("offset %d = %d, want %d", i, g.Offset, tc.offs[i])
				}
			}
			fl := j.Flush()
			if len(fl) != len(tc.flush) {
				t.Fatalf("flush = %v, want %v", fl, tc.flush)
			}
			for i := range fl {
				if fl[i].Line != tc.flush[i] {
					t.Errorf("flush %d mismatch", i)
				}
			}
			if _, ok := j.Pending(); ok {
				t.Error("still pending after Flush")
			}
		})
	}
}

// Cut points mirror a long User line: the split falls inside a key.
func TestJoinerProductionShapes(t *testing.T) {
	body := `User bitcoincash:qqexampleexampleexampleexampleexampleexamp:{"hashrate1m":"1.50T","hashrate5m":"1.40T",` +
		`"lastshare":1793345000,"workers":3,"shares":123456,"lastreject":1793345089,"lastrejectreason":"Above target",` +
		`"rejectrate1m":"56.8T","rejectrate5m":"50.1T","pad":"` + strings.Repeat("x", 600) + `"}`
	// Daemon split: 510-byte pieces.
	var lines []string
	for rest := body; len(rest) > 0; {
		n := min(len(rest), ChunkBody)
		lines = append(lines, stampAt(123)+rest[:n])
		rest = rest[n:]
	}
	if len(lines) < 2 {
		t.Fatal("fixture too short to split")
	}
	var j Joiner
	got := feedAll(&j, append(lines, stampAt(900)+"Next line"))
	if len(got) != 2 || got[0].Line != stampAt(123)+body || got[0].Offset != 0 {
		t.Fatalf("got %d lines; first ok=%v", len(got), len(got) > 0 && got[0].Line == stampAt(123)+body)
	}
	if ev := ParseLine(got[0].Line); ev.Kind != KindUser {
		t.Errorf("joined line parsed as %v", ev.Kind)
	}
}
