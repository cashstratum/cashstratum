package poolevents

import (
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"regexp"
	"sort"
	"testing"
	"time"
)

func TestFormatEmittedAt_MatchesContractExample(t *testing.T) {
	ts := time.Date(2026, 8, 30, 23, 26, 27, 191000000, time.UTC)
	got := FormatEmittedAt(ts)
	want := "2026-08-30T23:26:27.191Z"
	if got != want {
		t.Fatalf("FormatEmittedAt() = %q, want %q", got, want)
	}
}

// TestNetworkBlockEvent_MarshalsToContractFieldSet asserts the §4.1
// envelope's key set matches the contract example exactly -- catching a
// renamed/missing/extra json tag before it ever reaches Laravel.
func TestNetworkBlockEvent_MarshalsToContractFieldSet(t *testing.T) {
	height := int64(968432)
	blockTime := int64(1756598780)
	evt := NetworkBlockEvent{
		Envelope: Envelope{
			Event:           "network.block",
			EventID:         "018f3c9e-7a41-7b02-9c33-2d5e8f1a4b60",
			EmittedAt:       "2026-08-30T23:26:27.191Z",
			Pool:            "blocksniper",
			NotifierVersion: "1.0.0",
		},
		Hash:      "00000000000000000157d95f4f88ba7e98ac6234edecfe8ec91388b1d1fa1744",
		Height:    &height,
		BlockTime: &blockTime,
		Enriched:  true,
		Source:    "zmq",
	}

	raw, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}

	// Golden field set from the contract's §4.1 example body.
	golden := `{
		"event": "network.block",
		"event_id": "018f3c9e-7a41-7b02-9c33-2d5e8f1a4b60",
		"emitted_at": "2026-08-30T23:26:27.191Z",
		"pool": "blocksniper",
		"notifier_version": "1.0.0",
		"hash": "00000000000000000157d95f4f88ba7e98ac6234edecfe8ec91388b1d1fa1744",
		"height": 968432,
		"block_time": 1756598780,
		"enriched": true,
		"source": "zmq"
	}`
	var want map[string]json.RawMessage
	if err := json.Unmarshal([]byte(golden), &want); err != nil {
		t.Fatalf("json.Unmarshal(golden) error = %v", err)
	}

	gotKeys := sortedKeys(got)
	wantKeys := sortedKeys(want)
	if len(gotKeys) != len(wantKeys) {
		t.Fatalf("key set mismatch: got %v, want %v", gotKeys, wantKeys)
	}
	for i := range gotKeys {
		if gotKeys[i] != wantKeys[i] {
			t.Fatalf("key set mismatch: got %v, want %v", gotKeys, wantKeys)
		}
	}

	for k, wantVal := range want {
		gotVal, ok := got[k]
		if !ok {
			t.Errorf("missing key %q", k)
			continue
		}
		if string(gotVal) != string(wantVal) {
			t.Errorf("field %q = %s, want %s", k, gotVal, wantVal)
		}
	}
}

func TestNetworkBlockEvent_UnenrichedHasNullHeightAndBlockTime(t *testing.T) {
	evt := NewNetworkBlockEvent("deadbeef")
	raw, err := json.Marshal(evt)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if string(got["height"]) != "null" {
		t.Errorf("height = %s, want null", got["height"])
	}
	if string(got["block_time"]) != "null" {
		t.Errorf("block_time = %s, want null", got["block_time"])
	}
	if string(got["enriched"]) != "false" {
		t.Errorf("enriched = %s, want false", got["enriched"])
	}
	if string(got["source"]) != `"zmq"` {
		t.Errorf(`source = %s, want "zmq"`, got["source"])
	}
}

var uuidRE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewUUIDv7_VersionAndVariantBits(t *testing.T) {
	for i := 0; i < 100; i++ {
		id := NewUUIDv7()
		if !uuidRE.MatchString(id) {
			t.Fatalf("NewUUIDv7() = %q, does not match UUIDv7 shape (version nibble 7, variant 8/9/a/b)", id)
		}
	}
}

func TestNewUUIDv7_TimestampPrefixIsMonotonicish(t *testing.T) {
	before := uint64(time.Now().UnixMilli())
	id := NewUUIDv7()
	after := uint64(time.Now().UnixMilli())

	ms := unixMillisPrefix(id)
	if ms < before || ms > after {
		t.Fatalf("unixMillisPrefix(%q) = %d, want between %d and %d", id, ms, before, after)
	}

	// Successive ids should not have a decreasing timestamp prefix.
	var prev uint64
	for i := 0; i < 20; i++ {
		next := unixMillisPrefix(NewUUIDv7())
		if i > 0 && next < prev {
			t.Fatalf("UUIDv7 timestamp prefix went backwards: %d then %d", prev, next)
		}
		prev = next
	}
}

func sortedKeys(m map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func TestPoolName(t *testing.T) {
	os.Unsetenv("NOTIFIER_POOL_NAME")
	if got := PoolName(); got != "blocksniper" {
		t.Fatalf("default PoolName = %q, want %q", got, "blocksniper")
	}

	t.Setenv("NOTIFIER_POOL_NAME", "custompool")
	if got := PoolName(); got != "custompool" {
		t.Fatalf("custom PoolName = %q, want %q", got, "custompool")
	}
}

// unixMillisPrefix extracts the 48-bit timestamp a UUIDv7 was minted with,
// in Unix milliseconds.
func unixMillisPrefix(id string) uint64 {
	raw, err := hex.DecodeString(id[0:8] + id[9:13])
	if err != nil || len(raw) != 6 {
		return 0
	}
	return binary.BigEndian.Uint64(append([]byte{0, 0}, raw...))
}
