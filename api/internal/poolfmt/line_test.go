package poolfmt

import (
	"strings"
	"testing"
	"time"
)

const (
	h1 = "9f0c2a1b7d3e4f5a6b8c9d0e1f2a3b4c5d6e7f8091a2b3c4d5e6f708192a3b4c"
	h2 = "0000000000000000001a2b3c4d5e6f708192a3b4c5d6e7f8091a2b3c4d5e6f70"
	ts = "[2026-01-02 03:04:05.678] "
)

func TestParsePrefix(t *testing.T) {
	utc := time.UTC
	tm, rest, ok := ParsePrefixIn(ts+"hello", utc)
	if !ok || rest != "hello" || !tm.Equal(time.Date(2026, 1, 2, 3, 4, 5, 678e6, utc)) {
		t.Fatalf("got %v %q %v", tm, rest, ok)
	}
	for _, bad := range []string{"", "hello", "[2026-13-02 03:04:05.678] x", "[2026-02-31 03:04:05.678] x",
		"[2026-01-02 03:04:05.678]x", "[2026-01-02 03:04:05.67 ] x", "[2026-01-02 24:04:05.678] x", ts[:20]} {
		if _, _, ok := ParsePrefixIn(bad, utc); ok {
			t.Errorf("accepted %q", bad)
		}
	}
	if _, _, ok := ParsePrefix(ts + "x"); !ok {
		t.Error("local prefix")
	}
}

func TestParseLineFamilies(t *testing.T) {
	w := "qqfixture0000000000000000000000000000dummy"
	addr := "bitcoincash:" + w
	userJSON := `{"hashrate1m":"1.5T","hashrate5m":"1T","hashrate1hr":"900G","hashrate1d":"0","hashrate7d":"10K","lastshare":1767323045,"workers":2,"shares":100,"roundshares":50,"bestshare":123456.5,"luck":12.5,"networkdiff":1000.5,"bestever":999,"authorised":1767000000}`
	shortJSON := `{"hashrate1m":"1.5T","hashrate5m":"1T","hashrate1hr":"900G","hashrate1d":"0","hashrate7d":"10K","shares":100,"authorised":1767000000}`

	tests := []struct {
		name string
		body string
		kind Kind
		chk  func(*testing.T, Event)
	}{
		{"authorised", "Authorised client 17 192.0.2.7 worker " + addr + ".rig01 as user " + addr + " with diff 5000", KindAuthorised,
			func(t *testing.T, e Event) {
				if e.ClientID != "17" || e.Address != "192.0.2.7" || e.Worker != addr+".rig01" || e.User != addr || e.Diff != 5000 {
					t.Errorf("%+v", e)
				}
			}},
		{"dropped old", "Dropped client 17 192.0.2.7 user " + addr + " worker user1.rig01 ", KindDropped,
			func(t *testing.T, e Event) {
				if e.User != addr || e.Worker != "user1.rig01" || e.Reason.OK || e.Lazily {
					t.Errorf("%+v", e)
				}
			}},
		{"dropped lazily", "Dropped client 17 192.0.2.7 user u worker w lazily", KindDropped,
			func(t *testing.T, e Event) {
				if !e.Lazily || e.Reason.OK {
					t.Errorf("%+v", e)
				}
			}},
		{"dropped reason", "Dropped client 17 192.0.2.7 user u worker w  reason=peer_reset errno=104 age=33", KindDropped,
			func(t *testing.T, e Event) {
				if e.Reason.V != "peer_reset" || e.Errno.V != 104 || !e.Errno.OK || e.Age.V != 33 || !e.Age.OK {
					t.Errorf("%+v", e)
				}
			}},
		{"dropped lazily reason no errno", "Dropped client 17 192.0.2.7 user u worker w lazily reason=idle_drop age=5", KindDropped,
			func(t *testing.T, e Event) {
				if !e.Lazily || e.Reason.V != "idle_drop" || e.Errno.OK || e.Age.V != 5 {
					t.Errorf("%+v", e)
				}
			}},
		{"dropped bad age", "Dropped client 17 192.0.2.7 user u worker w  reason=peer_eof age=xx", KindDropped,
			func(t *testing.T, e Event) {
				if e.Age.OK || e.Reason.V != "peer_eof" {
					t.Errorf("%+v", e)
				}
			}},
		{"dropped no user", "Dropped client 17 192.0.2.7 no user worker w ", KindDropped,
			func(t *testing.T, e Event) {
				if !e.NoUser || e.Worker != "w" {
					t.Errorf("%+v", e)
				}
			}},
		{"dropped subclients", "Dropped 9 192.0.2.7 with 3 subclients  reason=dropall age=1", KindDropped,
			func(t *testing.T, e Event) {
				if e.Subclient.V != 3 || e.Reason.V != "dropall" {
					t.Errorf("%+v", e)
				}
			}},
		{"user periodic", "User " + addr + ":" + userJSON, KindUser,
			func(t *testing.T, e Event) {
				s := e.Stats
				if e.Name != addr || s.Hashrate1m != 1.5e12 || s.Hashrate1hr != 900e9 || s.Hashrate7d != 1e4 || !s.LastShare.OK ||
					s.RoundShares.V != 50 || s.BestShare.V != 123456.5 || s.Reject.Present {
					t.Errorf("%+v", e)
				}
			}},
		{"user solve shape", "User " + addr + ":" + shortJSON, KindUser,
			func(t *testing.T, e Event) {
				if e.Stats.LastShare.OK || e.Stats.RoundShares.OK || e.Stats.Shares != 100 {
					t.Errorf("%+v", e)
				}
			}},
		{"user bare colon name", "User a:b:c:{\"shares\":1}", KindUser,
			func(t *testing.T, e Event) {
				if e.Name != "a:b:c" {
					t.Errorf("%q", e.Name)
				}
			}},
		{"user truncated json", "User " + addr + `:{"hashrate1m":"1T","sha`, KindUnknown, nil},
		{"user no colon", "User bare", KindUnknown, nil},
		{"worker", "Worker " + addr + `.rig01:{"hashrate1m":"500G","hashrate5m":"0","hashrate1hr":"0","hashrate1d":"0","hashrate7d":"0","rejected":5,"rejectedcount":2,"rejects":{"stale":1,"lowdiff":1},"rejectrate1m":"1.5K","connects":3,"lastdisconnectreason":"peer_eof"}`, KindWorker,
			func(t *testing.T, e Event) {
				w := e.WStat
				if w.Name != addr+".rig01" || w.Hashrate1m != 500e9 || !w.Reject.Present || w.Reject.Rejected != 5 ||
					w.Reject.Buckets.Stale != 1 || w.Reject.Buckets.LowDiff != 1 || w.Reject.Rate1m.V != 1500 ||
					w.Reject.Rate5m.OK || !w.Connects.OK || w.Disconnects.OK || w.LastDisconnectReason.V != "peer_eof" {
					t.Errorf("%+v", w)
				}
			}},
		{"pool status", `Pool:{"runtime":311776,"lastupdate":1767323045,"Users":3,"Workers":9,"Idle":1,"Disconnected":5}`, KindPool,
			func(t *testing.T, e Event) {
				if !e.Pool.HasStatus || e.Pool.Runtime != 311776 || e.Pool.Workers != 9 || e.Pool.HasShares {
					t.Errorf("%+v", e.Pool)
				}
			}},
		{"pool hashrate", `Pool:{"hashrate1m":"4.97P","hashrate5m":"4P","hashrate15m":"3P","hashrate1hr":"2P","hashrate6hr":"1P","hashrate1d":"379K","hashrate7d":"0"}`, KindPool,
			func(t *testing.T, e Event) {
				if !e.Pool.HasHashrate || e.Pool.Hashrate1m != 4.97e15 || e.Pool.Hashrate1d != 379e3 {
					t.Errorf("%+v", e.Pool)
				}
			}},
		{"pool shares", `Pool:{"diff":131.93,"luck":131.93,"accepted":694727436102,"rejected":12,"bestshare":4927660663172,"SPS1m":1.5,"SPS5m":2,"SPS15m":3,"SPS1h":4}`, KindPool,
			func(t *testing.T, e Event) {
				if !e.Pool.HasShares || e.Pool.Diff != 131.93 || e.Pool.Accepted != 694727436102 || e.Pool.SPS1h != 4 {
					t.Errorf("%+v", e.Pool)
				}
			}},
		{"pool sv2 ignored", `SV2:{"x":1}`, KindUnknown, nil},
		{"pool unknown first key", `Pool:{"other":1}`, KindUnknown, nil},
		{"stored", "Stored local workbase with 39 transactions", KindStoredWorkbase, func(t *testing.T, e Event) {
			if e.Count != 39 {
				t.Error(e.Count)
			}
		}},
		{"stored remote", "Stored remote workbase with 7 transactions", KindStoredWorkbase, func(t *testing.T, e Event) {
			if e.Count != 7 || e.Extra != "remote" {
				t.Errorf("%+v", e)
			}
		}},
		{"zmq", "ZMQ block hash " + h1 + " from endpoint 0", KindZMQBlock, func(t *testing.T, e Event) {
			if e.Hash != h1 {
				t.Error(e.Hash)
			}
		}},
		{"zmq short hash", "ZMQ block hash abc from endpoint 0", KindUnknown, nil},
		{"netdiff", "Network diff set to 4927660663172.2", KindNetworkDiff, func(t *testing.T, e Event) {
			if e.Diff != 4927660663172.2 {
				t.Error(e.Diff)
			}
		}},
		{"netdiff proxy", "Network diff set to 12.5 (proxy 1:2)", KindNetworkDiff, nil},
		{"hash changed", "Block hash changed to " + h2, KindBlockHashChanged, func(t *testing.T, e Event) {
			if e.Hash != h2 {
				t.Error(e.Hash)
			}
		}},
		{"possible", "Possible block solve diff 4927660663172.164062 !", KindPossibleSolve, func(t *testing.T, e Event) {
			if e.Stale || e.Diff < 4.9e12 {
				t.Errorf("%+v", e)
			}
		}},
		{"possible stale", "Possible stale share block solve diff 12.000000 !", KindPossibleSolve, func(t *testing.T, e Event) {
			if !e.Stale {
				t.Error("stale")
			}
		}},
		{"submitting", "Submitting block data!", KindSubmitting, nil},
		{"submitting share", "Submitting possible block solve share diff 5.500000 !", KindSubmitting, func(t *testing.T, e Event) {
			if e.Diff != 5.5 || e.Extra != "share" {
				t.Errorf("%+v", e)
			}
		}},
		{"accepted", "BLOCK ACCEPTED!", KindBlockAccepted, nil},
		{"returned", "SUBMIT BLOCK RETURNED: high-hash", KindSubmitReturned, func(t *testing.T, e Event) {
			if e.Message != "high-hash" {
				t.Error(e.Message)
			}
		}},
		{"no response", "SUBMIT BLOCK GOT NO RESPONSE!", KindSubmitNoResponse, nil},
		{"solved sv1", "Solved and confirmed block 975091 by " + addr + ".rig01 on SV1", KindSolved, func(t *testing.T, e Event) {
			if e.Height != 975091 || e.Worker != addr+".rig01" || e.Address != addr || e.Protocol != "SV1" {
				t.Errorf("%+v", e)
			}
		}},
		{"solved old", "Solved and confirmed block 975091 by user1", KindSolved, func(t *testing.T, e Event) {
			if e.Protocol != "" || e.Address != "user1" {
				t.Errorf("%+v", e)
			}
		}},
		{"solved no height", "Solved and confirmed block!", KindSolvedNoHeight, nil},
		{"solve shares", "Block solved after 695060791782 shares at 132.0% diff", KindSolveShares, func(t *testing.T, e Event) {
			if e.Count != 695060791782 || e.Percent != 132 {
				t.Errorf("%+v", e)
			}
		}},
		{"rejected", "Submitted, but had block 975092 rejected", KindBlockRejected, func(t *testing.T, e Event) {
			if e.Height != 975092 {
				t.Error(e.Height)
			}
		}},
		{"sv2 accepted", "Block 975093 solved by user1.rig01 accepted on SV2/JD", KindSV2Accepted, func(t *testing.T, e Event) {
			if e.Height != 975093 || e.Address != "user1" {
				t.Errorf("%+v", e)
			}
		}},
		{"sv2 hash", "SV2/JD block hash " + h2 + " network_diff 1000.5", KindSV2Hash, nil},
		{"sv2 rejected", "SV2/JD block submit rejected/error height 975094 hash " + h2 + " by user1.rig01", KindSV2Rejected, func(t *testing.T, e Event) {
			if e.Height != 975094 || e.Hash != h2 || e.Worker != "user1.rig01" {
				t.Errorf("%+v", e)
			}
		}},
		{"nicehash", "NiceHash detected from useragent 'excavator/1.0 (x y)' for client 12, applied difficulty 500000", KindRentalDetected, func(t *testing.T, e Event) {
			if e.Extra != "NiceHash" || e.Message != "excavator/1.0 (x y)" || e.ClientID != "12" || e.Diff != 500000 {
				t.Errorf("%+v", e)
			}
		}},
		{"mrr", "MiningRigRentals detected from useragent '' for client 12, applied difficulty 1", KindRentalDetected, nil},
		{"pattern", "Client 12 workername 'user1.rig01' matched pattern 'rig*', applied difficulty 300", KindPatternDiff, func(t *testing.T, e Event) {
			if e.Worker != "user1.rig01" || e.Message != "rig*" || e.Diff != 300 {
				t.Errorf("%+v", e)
			}
		}},
		{"password", "Client 12 set difficulty to 2048 via password parameter", KindPasswordDiff, func(t *testing.T, e Event) {
			if e.Diff != 2048 {
				t.Error(e.Diff)
			}
		}},
		{"reaping", "Reaping client id 7 192.0.2.9:4444 idle pre-session for 30 seconds", KindReaping, nil},
		{"unknown", "Something entirely new happened", KindUnknown, nil},
		{"empty body", "", KindUnknown, nil},
		{"bad number", "Network diff set to 1.2.3", KindUnknown, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := ParseLineIn(ts+tc.body, time.UTC)
			if tc.kind != KindUnknown && (e.Kind != tc.kind || !e.HasTime) {
				t.Fatalf("kind %v want %v", e.Kind, tc.kind)
			}
			if tc.kind == KindUnknown && e.Kind != KindUnknown {
				t.Fatalf("kind %v want unknown", e.Kind)
			}
			if tc.chk != nil {
				tc.chk(t, e)
			}
		})
	}
}

func TestParseLineEdges(t *testing.T) {
	if e := ParseLine("Authorised client 1 a worker b as user c with diff 1"); e.Kind != KindUnknown || e.HasTime {
		t.Error("unprefixed must be unknown")
	}
	if e := ParseLine(ts + "BLOCK ACCEPTED!\r\n"); e.Kind != KindBlockAccepted {
		t.Error("CRLF")
	}
	if e := ParseLine(ts + "Solved and confirmed block 5 by \xff\xfe.rig"); e.Kind != KindSolved {
		t.Error("non-UTF-8 worker")
	}
	big := ts + "User x:{\"shares\":1,\"pad\":\"" + strings.Repeat("a", 1<<20) + "\"}"
	if e := ParseLine(big); e.Kind != KindUser {
		t.Error("1 MB line")
	}
	if e := ParseLine(ts + strings.Repeat("User ", 1<<18)); e.Kind != KindUnknown {
		t.Error("pathological")
	}
	if Kind(99).String() != "unknown" || KindSolved.String() != "solved" {
		t.Error("String")
	}
}

func lineSeeds() []string {
	return []string{
		ts + "Authorised client 17 192.0.2.7 worker user1.rig01 as user user1 with diff 5000",
		ts + "Dropped client 17 192.0.2.7 user u worker w  reason=peer_reset errno=104 age=33",
		ts + `User bitcoincash:qqdummy:{"hashrate1m":"1.5T","lastshare":1,"shares":2}`,
		ts + `Worker w:{"hashrate1m":"1T","connects":1}`,
		ts + `Pool:{"diff":1.5,"luck":1.5,"accepted":1,"rejected":0,"bestshare":3}`,
		ts + "Solved and confirmed block 975091 by user1.rig01 on SV1",
		ts + "Block solved after 10 shares at 132.0% diff",
		ts + "Possible block solve diff 1.000000 !",
		ts + "NiceHash detected from useragent 'a' for client 1, applied difficulty 2",
		ts + "SV2/JD block submit rejected/error height 1 hash " + h2 + " by w",
		"plain", "", "[", ts,
	}
}

func FuzzParseLine(f *testing.F) {
	for _, s := range lineSeeds() {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		e := ParseLineIn(s, time.UTC)
		_ = e.Kind.String()
		_ = ParseBody(s)
	})
}

func FuzzParsePrefix(f *testing.F) {
	for _, s := range lineSeeds() {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		if _, rest, ok := ParsePrefixIn(s, time.UTC); ok && len(rest) > len(s) {
			t.Fatal("rest longer than input")
		}
	})
}
