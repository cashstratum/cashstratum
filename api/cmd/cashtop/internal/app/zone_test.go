package app

import (
	"os"
	"testing"
	"time"
)

// The fixture generator writes pool log stamps from UTC times, while the
// parser reads them as local time, as the pool writes them. Pin the zone so
// the fixture's log and sharelogs agree on every machine.
func TestMain(m *testing.M) {
	time.Local = time.UTC
	os.Exit(m.Run())
}
