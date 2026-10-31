package poolfmt

import (
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Errors returned by the file parsers. Callers rewriting-in-place readers
// should treat all of them as "retry, keep the last good copy".
var (
	ErrEmpty      = errors.New("poolfmt: empty input")
	ErrIncomplete = errors.New("poolfmt: incomplete input")
	ErrTooLong    = errors.New("poolfmt: line too long")
)

// maxFile bounds how much of a reader the file parsers will consume.
const maxFile = 32 << 20

// Opt is an optional value: OK is false when the key was absent, which is
// distinct from a present zero.
type Opt[T any] struct {
	V  T
	OK bool
}

func some[T any](v T) Opt[T] { return Opt[T]{V: v, OK: true} }

func readAll(r io.Reader) ([]byte, error) {
	if r == nil {
		return nil, ErrEmpty
	}
	b, err := io.ReadAll(io.LimitReader(r, maxFile))
	if err != nil {
		return nil, err
	}
	return b, nil
}

// ParseHashrate converts an SI hashrate string such as "301", "679M" or
// "4.97P" into hashes per second. Suffixes K, M, G, T, P and E are decimal.
func ParseHashrate(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("poolfmt: empty hashrate")
	}
	mult := 1.0
	switch s[len(s)-1] {
	case 'K', 'k':
		mult = 1e3
	case 'M':
		mult = 1e6
	case 'G':
		mult = 1e9
	case 'T':
		mult = 1e12
	case 'P':
		mult = 1e15
	case 'E':
		mult = 1e18
	}
	num := s
	if mult != 1 {
		num = s[:len(s)-1]
	}
	if num == "" {
		return 0, fmt.Errorf("poolfmt: bad hashrate %q", s)
	}
	dots := 0
	for i := 0; i < len(num); i++ {
		c := num[i]
		switch {
		case c >= '0' && c <= '9':
		case c == '.' && dots == 0:
			dots++
		default:
			return 0, fmt.Errorf("poolfmt: bad hashrate %q", s)
		}
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, fmt.Errorf("poolfmt: bad hashrate %q", s)
	}
	return f * mult, nil
}

var addrPrefix = regexp.MustCompile(`^[a-z]+:`)

// NormalizeAddress strips a leading network prefix such as "bitcoincash:" so
// that user file keys and worker names can be joined.
func NormalizeAddress(s string) string {
	return addrPrefix.ReplaceAllString(s, "")
}

// SplitWorker splits "address.rig" at the first dot. Addresses never contain
// dots, so everything after it is the rig name. A name without a dot has an
// empty rig.
func SplitWorker(full string) (address, rig string) {
	if i := strings.IndexByte(full, '.'); i >= 0 {
		return full[:i], full[i+1:]
	}
	return full, ""
}

func digits(s string, i, n int) (int, bool) {
	if i+n > len(s) {
		return 0, false
	}
	v := 0
	for _, c := range []byte(s[i : i+n]) {
		if c < '0' || c > '9' {
			return 0, false
		}
		v = v*10 + int(c-'0')
	}
	return v, true
}

// ParseStamp parses "YYYY-MM-DD HH:MM:SS.mmm" in loc (nil means time.Local).
func ParseStamp(s string, loc *time.Location) (time.Time, bool) {
	if len(s) != 23 || s[4] != '-' || s[7] != '-' || s[10] != ' ' ||
		s[13] != ':' || s[16] != ':' || s[19] != '.' {
		return time.Time{}, false
	}
	y, ok1 := digits(s, 0, 4)
	mo, ok2 := digits(s, 5, 2)
	d, ok3 := digits(s, 8, 2)
	h, ok4 := digits(s, 11, 2)
	mi, ok5 := digits(s, 14, 2)
	sec, ok6 := digits(s, 17, 2)
	ms, ok7 := digits(s, 20, 3)
	if !(ok1 && ok2 && ok3 && ok4 && ok5 && ok6 && ok7) ||
		mo < 1 || mo > 12 || d < 1 || d > 31 || h > 23 || mi > 59 || sec > 60 {
		return time.Time{}, false
	}
	if loc == nil {
		loc = time.Local
	}
	t := time.Date(y, time.Month(mo), d, h, mi, sec, ms*int(time.Millisecond), loc)
	if t.Day() != d {
		return time.Time{}, false
	}
	return t, true
}

// ParsePrefix splits "[YYYY-MM-DD HH:MM:SS.mmm] rest" using the host's local
// time zone. Lines without the prefix report ok=false.
func ParsePrefix(line string) (t time.Time, rest string, ok bool) {
	return ParsePrefixIn(line, nil)
}

// ParsePrefixIn is ParsePrefix with an explicit time zone.
func ParsePrefixIn(line string, loc *time.Location) (t time.Time, rest string, ok bool) {
	if len(line) < 26 || line[0] != '[' || line[24] != ']' || line[25] != ' ' {
		return time.Time{}, "", false
	}
	t, ok = ParseStamp(line[1:24], loc)
	if !ok {
		return time.Time{}, "", false
	}
	return t, line[26:], true
}
