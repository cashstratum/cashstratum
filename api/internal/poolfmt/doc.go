// Package poolfmt parses the on-disk and log formats written by the
// CashStratum pool daemon: cashstratum.log lines, users/<address> files,
// pool.status, sharelog records, solved-heights.idx and the optional solves/
// archive.
//
// The package is pure: it does only io.Reader work, uses the standard library,
// and every function is total (bad input yields an error or an Unknown event,
// never a panic).
package poolfmt
