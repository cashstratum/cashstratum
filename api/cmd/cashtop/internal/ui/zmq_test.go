package ui

import (
	"testing"
	"time"
)

func TestZMQLabel(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	ago := now.Add(-12 * time.Minute)
	cases := []struct {
		name string
		n    NetworkInfo
		want string
	}{
		{"off", NetworkInfo{}, "off"},
		{"down", NetworkInfo{ZMQConfigured: true}, "down"},
		{"waiting", NetworkInfo{ZMQConfigured: true, ZMQConnected: true}, "waiting"},
		{"age", NetworkInfo{ZMQConfigured: true, ZMQConnected: true, ZMQLast: ago}, ageCoarse(now, ago)},
		{"age down", NetworkInfo{ZMQConfigured: true, ZMQLast: ago}, ageCoarse(now, ago) + " down"},
		{"log fallback without endpoint", NetworkInfo{ZMQLast: ago}, ageCoarse(now, ago)},
	}
	for _, c := range cases {
		if got := zmqLabel(now, c.n); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
