package fixturegen

import (
	"bufio"
	"io"
	"time"
)

// GenerateLog writes prefixed log lines until at least sizeBytes bytes have
// been produced (the final line may overshoot). The mix is dominated by User,
// Pool, Authorised and Dropped lines. It returns the byte count written.
func GenerateLog(w io.Writer, sizeBytes int64, seed uint64) (int64, error) {
	return GenerateLogSplit(w, sizeBytes, seed, false)
}

// GenerateLogSplit is GenerateLog with the option to write long lines in
// several stamped pieces, as the pool's logger does. Splitting also turns on
// verbose User lines so that there is something to split.
func GenerateLogSplit(w io.Writer, sizeBytes int64, seed uint64, split bool) (int64, error) {
	g := New(seed, FixtureStart)
	g.split, g.verbose = split, split
	g.SetUsers(300, 3)
	bw := bufio.NewWriterSize(w, 1<<20)
	var n int64
	write := func(s string) error {
		for _, p := range g.splitLines([]string{s}) {
			k, err := bw.WriteString(p)
			n += int64(k)
			if err != nil {
				return err
			}
		}
		return nil
	}
	var cycle int
	for n < sizeBytes {
		g.Advance(time.Duration(1+g.rng.IntN(400)) * time.Millisecond)
		var err error
		switch r := g.rng.IntN(1000); {
		case r < 1:
			for _, l := range g.BlockCluster(g.Height, FullWorker(g.rng.IntN(300), 0), "SV1") {
				if err = write(l); err != nil {
					break
				}
			}
		case r < 4:
			for _, l := range g.NewTip() {
				if err = write(l); err != nil {
					break
				}
			}
		case r < 8:
			err = write(g.Workbase())
		case r < 40:
			for _, l := range g.Tick() {
				if err = write(l); err != nil {
					break
				}
			}
			cycle++
		case r < 500:
			err = write(g.Authorised())
		case r < 750:
			err = write(g.Dropped(g.rng.IntN(4) != 0, g.rng.IntN(8) == 0))
		default:
			err = write(g.User(g.rng.IntN(300)))
		}
		if err != nil {
			return n, err
		}
	}
	return n, bw.Flush()
}
