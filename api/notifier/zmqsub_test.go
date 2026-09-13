package main

import (
	"bytes"
	"testing"
)

func TestSelectHashFrame_PicksThe32ByteFrame(t *testing.T) {
	topic := []byte("hashblock")
	hash := bytes.Repeat([]byte{0xab}, 32)
	seq := []byte{0x01, 0x00, 0x00, 0x00}

	got, ok := selectHashFrame([][]byte{topic, hash, seq})
	if !ok {
		t.Fatalf("selectHashFrame() ok = false, want true")
	}
	if !bytes.Equal(got, hash) {
		t.Fatalf("selectHashFrame() = %x, want %x", got, hash)
	}
}

func TestSelectHashFrame_IgnoresShortAndOversizedFrames_NoPanic(t *testing.T) {
	cases := [][][]byte{
		{},
		{[]byte("hashblock")},            // 9 bytes only
		{{0x01, 0x00, 0x00, 0x00}},       // 4 bytes only
		{bytes.Repeat([]byte{0x01}, 31)}, // one short of 32
		{bytes.Repeat([]byte{0x01}, 33)}, // one over 32
		{[]byte("hashblock"), {0x01, 0x00, 0x00, 0x00}},
	}

	for i, frames := range cases {
		got, ok := selectHashFrame(frames)
		if ok {
			t.Errorf("case %d: selectHashFrame() ok = true, want false (frames=%v)", i, frames)
		}
		if got != nil {
			t.Errorf("case %d: selectHashFrame() = %x, want nil", i, got)
		}
	}
}

func TestSelectHashFrame_PicksFirst32ByteFrameEvenIfNotFrameZero(t *testing.T) {
	// Regression guard: must not assume Frames[0] is the hash.
	seq := []byte{0x2a, 0x00, 0x00, 0x00}
	hash := bytes.Repeat([]byte{0xcd}, 32)
	topic := []byte("hashblock")

	got, ok := selectHashFrame([][]byte{seq, topic, hash})
	if !ok || !bytes.Equal(got, hash) {
		t.Fatalf("selectHashFrame() = (%x, %v), want (%x, true)", got, ok, hash)
	}
}
