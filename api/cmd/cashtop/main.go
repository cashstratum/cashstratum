// Command cashtop is a terminal dashboard for a CashStratum pool.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"cashstratumapi/cmd/cashtop/internal/app"
	"cashstratumapi/cmd/cashtop/internal/demo"
	"cashstratumapi/cmd/cashtop/internal/index"
	"cashstratumapi/cmd/cashtop/internal/termcap"
	"cashstratumapi/cmd/cashtop/internal/ui"
)

const version = "1.4.0"

type options struct {
	showVersion     bool
	demo            bool
	demoProfile     string
	quiet           bool
	ascii           bool
	noColor         bool
	notify          string
	reduceMotion    bool
	replayLastBlock bool
	conf            string
	root            string
	snapshotOnce    bool
	snapshotWait    time.Duration
	renderFrame     string
	size            string
	theme           string
}

// demoFlag accepts --demo and --demo=<profile>.
type demoFlag struct{ o *options }

func (f demoFlag) String() string {
	if f.o == nil || !f.o.demo {
		return "false"
	}
	if f.o.demoProfile == "" {
		return "true"
	}
	return f.o.demoProfile
}

func (f demoFlag) IsBoolFlag() bool { return true }

func (f demoFlag) Set(v string) error {
	switch v {
	case "true", "1", "t", "T", "TRUE", "True":
		f.o.demo, f.o.demoProfile = true, ""
	case "false", "0", "f", "F", "FALSE", "False":
		f.o.demo, f.o.demoProfile = false, ""
	case "showcase":
		f.o.demo, f.o.demoProfile = true, v
	default:
		return fmt.Errorf("unknown demo profile %q (want showcase)", v)
	}
	return nil
}

// parseSize reads "WxH".
func parseSize(s string) (int, int, error) {
	ws, hs, ok := strings.Cut(strings.ToLower(s), "x")
	w, err1 := strconv.Atoi(ws)
	h, err2 := strconv.Atoi(hs)
	if !ok || err1 != nil || err2 != nil || w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("invalid --size %q (want WxH, e.g. 120x36)", s)
	}
	return w, h, nil
}

func parseFlags(args []string) (options, error) {
	var o options
	fs := flag.NewFlagSet("cashtop", flag.ContinueOnError)
	fs.BoolVar(&o.showVersion, "version", false, "print version and exit")
	fs.Var(demoFlag{&o}, "demo", "run against generated demo data; --demo=showcase for a busy sample pool")
	fs.BoolVar(&o.quiet, "quiet", false, "suppress celebrations and notifications")
	fs.BoolVar(&o.ascii, "ascii", false, "use ASCII-only glyphs")
	fs.BoolVar(&o.noColor, "no-color", false, "disable colour output")
	fs.StringVar(&o.notify, "notify", "auto", "notification mode: auto|bell|osc9|osc777|off")
	fs.BoolVar(&o.reduceMotion, "reduce-motion", false, "disable animations")
	fs.BoolVar(&o.replayLastBlock, "replay-last-block", false, "replay the most recent block celebration")
	fs.StringVar(&o.conf, "conf", "", "path to the pool configuration file")
	fs.StringVar(&o.root, "root", "", "pool root directory")
	fs.BoolVar(&o.snapshotOnce, "snapshot-once", false, "")
	fs.DurationVar(&o.snapshotWait, "snapshot-wait", 0, "")
	fs.StringVar(&o.renderFrame, "render-frame", "", "")
	fs.StringVar(&o.size, "size", "120x36", "")
	fs.StringVar(&o.theme, "theme", "cashgreen", "colour theme: cashgreen|mono")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage of cashtop:")
		fs.VisitAll(func(f *flag.Flag) {
			if f.Usage != "" {
				fmt.Fprintf(fs.Output(), "  --%s\n    \t%s\n", f.Name, f.Usage)
			}
		})
	}
	if err := fs.Parse(args); err != nil {
		return o, err
	}
	switch o.notify {
	case "auto", "bell", "osc9", "osc777", "off":
	default:
		return o, fmt.Errorf("invalid --notify value %q", o.notify)
	}
	if _, err := ui.ThemeNamed(o.theme); err != nil {
		return o, err
	}
	if o.renderFrame != "" && !o.demo {
		return o, fmt.Errorf("--render-frame needs --demo or --demo=showcase")
	}
	return o, nil
}

// printFrame writes one fully rendered frame of the demo data to stdout.
func printFrame(o options, caps termcap.Caps, stdout io.Writer) error {
	w, h, err := parseSize(o.size)
	if err != nil {
		return err
	}
	th, err := ui.ThemeNamed(o.theme)
	if err != nil {
		return err
	}
	now := time.Date(2026, 10, 30, 12, 0, 0, 0, time.UTC)
	var prov ui.Provider = demo.New(demo.Options{Seed: 1, Now: now})
	if o.demoProfile == "showcase" {
		prov = demo.Showcase(demo.Options{Seed: 7, Now: now})
	}
	caps.Glyphs, caps.NoColor, caps.Quiet, caps.Notify = termcap.UTF8, false, true, termcap.NotifyOff
	if o.ascii {
		caps.Glyphs = termcap.ASCII
	}
	frame, err := ui.RenderFrame(prov, ui.Options{Caps: caps, Theme: &th, Now: func() time.Time { return now }, Motion: !o.reduceMotion},
		o.renderFrame, w, h)
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdout, frame+"\n")
	return err
}

// openLive resolves the pool directory and opens the live data source.
// Diagnostics go to a file in the cache directory, never to the terminal.
func openLive(o options, caps termcap.Caps, getenv func(string) string) (*app.App, func(), error) {
	root, conf, err := app.ResolveRoot(o.root, o.conf, getenv)
	if err != nil {
		return nil, nil, err
	}
	lg := log.New(io.Discard, "", log.LstdFlags)
	closeLog := func() {}
	dir := index.CacheDirFor(root)
	if err := os.MkdirAll(dir, 0o700); err == nil {
		if f, err := os.OpenFile(filepath.Join(dir, "cashtop.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			lg.SetOutput(f)
			closeLog = func() { f.Close() }
		}
	}
	log.SetOutput(lg.Writer())
	a, err := app.Open(app.Config{Root: root, Conf: conf, Caps: caps, Log: lg})
	if err != nil {
		closeLog()
		return nil, nil, err
	}
	return a, func() {
		if err := a.Close(); err != nil {
			lg.Printf("closing the index: %v", err)
		}
		closeLog()
	}, nil
}

func run(args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	o, err := parseFlags(args)
	if err != nil {
		fmt.Fprintln(stderr, "cashtop:", err)
		return 2
	}
	if o.showVersion {
		fmt.Fprintln(stdout, "cashtop", version)
		return 0
	}
	caps := termcap.Detect(getenv, termcap.Flags{ASCII: o.ascii, NoColor: o.noColor, Quiet: o.quiet, Notify: o.notify})

	if o.renderFrame != "" {
		if err := printFrame(o, caps, stdout); err != nil {
			fmt.Fprintln(stderr, "cashtop:", err)
			return 2
		}
		return 0
	}

	var (
		prov    ui.Provider
		live    *app.App
		cleanup = func() {}
		poll    time.Duration
	)
	if o.demo {
		if o.demoProfile == "showcase" {
			prov = demo.Showcase(demo.Options{Seed: 7, Now: time.Now()})
		} else {
			prov = demo.New(demo.Options{Seed: 1, Now: time.Now()})
		}
		poll = 2 * time.Second
	} else {
		live, cleanup, err = openLive(o, caps, getenv)
		if err != nil {
			fmt.Fprintln(stderr, "cashtop:", err)
			return 2
		}
		defer cleanup()
		prov = live
	}

	if o.snapshotOnce {
		if live != nil {
			live.RefreshNode(context.Background())
			if o.snapshotWait > 0 {
				waitForRewards(live, o.snapshotWait)
			}
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(prov.Snapshot()); err != nil {
			fmt.Fprintln(stderr, "cashtop:", err)
			return 1
		}
		return 0
	}
	m := ui.New(prov, ui.Options{Caps: caps, Motion: !o.reduceMotion, PollInterval: poll})
	p := tea.NewProgram(m, termcap.ProgramOptions(caps, nil)...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if live != nil {
		done := make(chan struct{})
		go func() { live.Run(ctx, p.Send); close(done) }()
		// Runs before cleanup: stop the loops and wait for them, so the index
		// is closed only after the last enrichment has been written.
		defer func() { cancel(); <-done }()
	}
	if o.replayLastBlock {
		// The same path as ":test-celebrate": with no block yet it shows a
		// stand-in at the current height.
		replay := ui.ReplayCmd(caps, prov.Snapshot(), time.Now())
		go func() { p.Send(replay()) }()
	}
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(stderr, "cashtop:", err)
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// waitForRewards runs the live loops until every accepted block has a reward
// or d has passed, whichever comes first.
func waitForRewards(a *app.App, d time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	done := make(chan struct{})
	go func() {
		a.Run(ctx, func(tea.Msg) {})
		close(done)
	}()
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-t.C:
			if a.RewardsComplete() {
				cancel()
				break loop
			}
		}
	}
	<-done
}
