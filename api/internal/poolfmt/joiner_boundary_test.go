package poolfmt

import (
	"strings"
	"testing"
)

const boundaryAddr = "bitcoincash:qpg2vwdw65xjz67x5dzuqq4ugtt968rth52upzna8f"

// workerBody builds a Worker message of exactly n bytes with a JSON tail of
// the given suffix, padding the object with a filler field.
func workerBody(t *testing.T, n int, tail string) string {
	t.Helper()
	head := "Worker " + boundaryAddr + ".worker3:{\"hashrate1m\":\"1.2T\",\"shares\":206025000000,\"pad\":\""
	end := "\"," + tail
	pad := n - len(head) - len(end)
	if pad < 0 {
		t.Fatalf("fixture too long by %d", -pad)
	}
	b := head + strings.Repeat("x", pad) + end
	if len(b) != n {
		t.Fatalf("len %d want %d", len(b), n)
	}
	return b
}

func TestJoinerCompleteChunkIsNotSwallowed(t *testing.T) {
	const ts = "[2026-11-07 05:57:12.599] "
	body := workerBody(t, ChunkBody, "\"lastdisconnect\":1793956713,\"lastdisconnectreason\":\"peer_eof\"}")
	lines := []string{
		ts + "Solved and confirmed block 976322 by " + boundaryAddr + ".worker3 on SV1",
		ts + "User " + boundaryAddr + ":{\"hashrate1m\":\"1T\"}",
		ts + body,
		ts + "Block solved after 206025000000 shares at 37.4% diff",
	}
	var j Joiner
	got := feedAll(&j, lines)
	got = append(got, j.Flush()...)
	if len(got) != len(lines) {
		t.Fatalf("got %d lines want %d", len(got), len(lines))
	}
	for i := range lines {
		if got[i].Line != lines[i] {
			t.Fatalf("line %d changed: %q", i, got[i].Line)
		}
	}
	ev := ParseLine(got[3].Line)
	if ev.Kind != KindSolveShares || ev.Percent != 37.4 {
		t.Fatalf("kind=%v percent=%v", ev.Kind, ev.Percent)
	}
}

func TestJoinerRealChunkStillJoins(t *testing.T) {
	const ts = "[2026-11-07 02:00:00.646] "
	body := workerBody(t, ChunkBody, "\"lastdisconnect\":1793992336,\"lastdisconnectreason\":\"p")
	lines := []string{ts + body, ts + "eer_eof\"}", ts + "Block solved after 65606500000 shares at 11.9% diff"}
	var j Joiner
	got := feedAll(&j, lines)
	if len(got) != 2 {
		t.Fatalf("got %d lines want 2", len(got))
	}
	if got[0].Line != ts+body+"eer_eof\"}" {
		t.Fatalf("not joined: %q", got[0].Line)
	}
	if ParseLine(got[1].Line).Kind != KindSolveShares {
		t.Fatalf("second line kind %v", ParseLine(got[1].Line).Kind)
	}
}
