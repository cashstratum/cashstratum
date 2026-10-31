package fixturegen

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FixtureStart is the fake clock origin used by WriteTree and Demo.
var FixtureStart = time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)

// Tree describes what WriteTree laid out.
type Tree struct {
	Root     string
	LogPath  string
	Users    []string // user file names (prefixed addresses)
	Found    []BlockEvent
	Rejected []int
}

func put(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// WriteTree lays out a complete fake pool root under dir:
//
//	logs/cashstratum.log
//	logs/users/<addr>
//	logs/pool/pool.status
//	logs/<%08x>/<%016x>.sharelog
//	solved-heights.idx
//	solves/<height>-<hash>.log and solves/INDEX.tsv (only when solves is true)
func WriteTree(dir string, seed uint64, solves bool) (*Tree, error) {
	return WriteTreeOpts(dir, seed, solves, TreeOpts{})
}

// TreeOpts selects optional log shapes for WriteTreeOpts.
type TreeOpts struct {
	Verbose bool // User lines carry reject statistics and exceed 510 bytes
	Split   bool // lines longer than 510 body bytes are written in several pieces
}

// WriteTreeOpts is WriteTree with optional long and split log lines.
func WriteTreeOpts(dir string, seed uint64, solves bool, o TreeOpts) (*Tree, error) {
	g := New(seed, FixtureStart)
	g.split, g.verbose = o.Split, o.Verbose
	sim := g.Simulate(40)
	t := &Tree{Root: dir, LogPath: filepath.Join(dir, "logs", "cashstratum.log"), Found: sim.Found, Rejected: sim.Rejected}
	if err := put(t.LogPath, []byte(strings.Join(sim.Lines, ""))); err != nil {
		return nil, err
	}
	for u := 0; u < g.nUsers; u++ {
		name := Addr(u)
		t.Users = append(t.Users, name)
		if err := put(filepath.Join(dir, "logs", "users", name), g.UserFile(u)); err != nil {
			return nil, err
		}
	}
	if err := put(filepath.Join(dir, "logs", "pool", "pool.status"), g.PoolStatus()); err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(sim.Shares))
	for p := range sim.Shares {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if err := put(filepath.Join(dir, "logs", p), []byte(strings.Join(sim.Shares[p], "\n")+"\n")); err != nil {
			return nil, err
		}
	}
	heights := make([]int, 0, len(sim.Found))
	for _, b := range sim.Found {
		heights = append(heights, b.Height)
	}
	if err := put(filepath.Join(dir, "solved-heights.idx"), SolvedHeightsIdx(heights)); err != nil {
		return nil, err
	}
	if solves {
		var rows []SolveIndexRow
		for _, b := range sim.Found {
			if err := put(filepath.Join(dir, "solves", SolveFileName(b.Height, b.Hash)), SolveFile(b.Height, b.Hash, b.Finder, b.Stamp, b.Window)); err != nil {
				return nil, err
			}
			rows = append(rows, SolveIndexRow{b.Height, b.Hash, b.Stamp + ".000", "ACCEPTED", b.Finder, 12})
		}
		if err := put(filepath.Join(dir, "solves", "INDEX.tsv"), SolveIndex(rows)); err != nil {
			return nil, err
		}
	}
	if len(t.Users) == 0 {
		return nil, fmt.Errorf("fixturegen: no users generated")
	}
	return t, nil
}
