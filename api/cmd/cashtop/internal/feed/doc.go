// Package feed turns raw pool log lines into the filtered, annotated rows of
// the live feed. The rules are an ordered, first-match-wins port of
// monitor.sh's filtered view: idle users and the pool's own API probes are
// hidden, difficulty-source tags are attached to the next connect, quick
// reconnect loops fold into a single FLAP row, and the three Pool lines of a
// minute collapse into one POOL row.
//
// The engine is stateful and single-goroutine: feed it lines in file order.
// Elapsed times use full timestamps, so gaps longer than a day do not wrap.
package feed
