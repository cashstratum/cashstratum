package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"cashstratumapi/internal/ckconf"
)

// ErrNoRoot is returned when no pool install directory can be found.
var ErrNoRoot = errors.New("pool directory not found; pass --root <dir> (the one holding logs/cashstratum.log)")

// hasLog reports whether dir holds the pool log: the default
// logs/cashstratum.log, or the log the given conf would make the daemon write.
func hasLog(dir, conf string) bool {
	if dir == "" {
		return false
	}
	if isFile(filepath.Join(dir, "logs", "cashstratum.log")) {
		return true
	}
	p, ok := confLogPath(dir, conf)
	return ok && isFile(p)
}

func isFile(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

// confLogPath returns the log the daemon writes when started with conf and
// no -n: <logdir>/<name>.log. The daemon reads <name>.conf when no -c is
// given, so name is the conf's file stem; logdir is the conf's "logdir",
// "logs" when unset or unreadable, and a relative logdir is taken relative to
// root. ok is false when conf is empty or not named *.conf, because then the
// instance name cannot be inferred.
func confLogPath(root, conf string) (string, bool) {
	base := filepath.Base(conf)
	name, isConf := strings.CutSuffix(base, ".conf")
	if conf == "" || !isConf || name == "" {
		return "", false
	}
	dir := "logs"
	if data, err := os.ReadFile(conf); err == nil {
		var cf struct {
			LogDir string `json:"logdir"`
		}
		if json.Unmarshal(data, &cf) == nil && cf.LogDir != "" {
			dir = cf.LogDir
		}
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	return filepath.Join(dir, name+".log"), true
}

// ResolveLogPath picks the pool log to follow. An explicit path (--log) always
// wins. Otherwise the log derived from the conf (see confLogPath) is used when
// it exists, so an instance run under another name is still found; failing
// that, the default <root>/logs/cashstratum.log.
func ResolveLogPath(root, conf, explicit string) string {
	if explicit != "" {
		if abs, err := filepath.Abs(explicit); err == nil {
			return abs
		}
		return explicit
	}
	if p, ok := confLogPath(root, conf); ok && isFile(p) {
		return p
	}
	return filepath.Join(root, "logs", "cashstratum.log")
}

// ResolveRoot finds the pool install directory and its configuration file.
// Precedence: --root, the directory of --conf, CASHSTRATUM_LOG_PATH (the API's
// environment file), the directory the binary lives in (installed as
// <root>/cashtop or <root>/api/cashtop, possibly via a ~/cashtop link), then
// ~/cashstratum and ~/ckpool. An explicit --root is trusted even before the
// log exists. A directory also counts when it holds the log --conf names
// (see confLogPath). The conf is --conf, else the first existing candidate beside
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
		if hasLog(c, conf) {
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
