package app

import (
	"errors"
	"os"
	"path/filepath"

	"cashstratumapi/internal/ckconf"
)

// ErrNoRoot is returned when no pool install directory can be found.
var ErrNoRoot = errors.New("pool directory not found; pass --root <dir> (the one holding logs/cashstratum.log)")

func hasLog(dir string) bool {
	if dir == "" {
		return false
	}
	fi, err := os.Stat(filepath.Join(dir, "logs", "cashstratum.log"))
	return err == nil && !fi.IsDir()
}

// ResolveRoot finds the pool install directory and its configuration file.
// Precedence: --root, the directory of --conf, CASHSTRATUM_LOG_PATH (the API's
// environment file), the directory the binary lives in (installed as
// <root>/cashtop or <root>/api/cashtop, possibly via a ~/cashtop link), then
// ~/cashstratum and ~/ckpool. An explicit --root is trusted even before the
// log exists. The conf is --conf, else the first existing candidate beside
// the logs; it may be empty.
func ResolveRoot(root, conf string, getenv func(string) string) (string, string, error) {
	var cands []string
	switch {
	case root != "":
		cands = []string{root}
	default:
		if conf != "" {
			cands = append(cands, filepath.Dir(conf))
		}
		if lp := getenv("CASHSTRATUM_LOG_PATH"); lp != "" {
			cands = append(cands, filepath.Dir(filepath.Dir(lp)))
		}
		if exe, err := os.Executable(); err == nil {
			if real, err := filepath.EvalSymlinks(exe); err == nil {
				exe = real
			}
			d := filepath.Dir(exe)
			cands = append(cands, d, filepath.Dir(d))
		}
		if home := getenv("HOME"); home != "" {
			cands = append(cands, filepath.Join(home, "cashstratum"), filepath.Join(home, "ckpool"))
		}
	}
	found := ""
	for _, c := range cands {
		if hasLog(c) {
			found = c
			break
		}
	}
	if found == "" && root != "" {
		if fi, err := os.Stat(root); err == nil && fi.IsDir() {
			found = root
		}
	}
	if found == "" {
		return "", "", ErrNoRoot
	}
	if abs, err := filepath.Abs(found); err == nil {
		found = abs
	}
	if conf == "" {
		for _, c := range ckconf.Candidates(filepath.Join(found, "logs", "cashstratum.log")) {
			if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
				conf = c
				break
			}
		}
	}
	return found, conf, nil
}
