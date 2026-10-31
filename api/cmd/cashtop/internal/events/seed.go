package events

import (
	"io"

	"cashstratumapi/internal/poolfmt"
)

// Heights is a SeenSource over a fixed list.
type Heights []int64

// SeenHeights implements SeenSource.
func (h Heights) SeenHeights() []int64 { return h }

// SolvedHeightsSource reads a solved-heights.idx stream. A torn or empty read
// yields no heights.
func SolvedHeightsSource(r io.Reader) SeenSource {
	h, err := poolfmt.ParseSolvedHeights(r)
	if err != nil {
		return Heights(nil)
	}
	return Heights(h)
}

// SolvesIndexSource reads a solves INDEX.tsv stream.
func SolvesIndexSource(r io.Reader) SeenSource {
	entries, err := poolfmt.ParseSolvesIndex(r)
	if err != nil {
		return Heights(nil)
	}
	out := make(Heights, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Height)
	}
	return out
}
