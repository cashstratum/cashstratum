package main

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// markerRule is one generic pattern that must not appear in shipped files.
type markerRule struct {
	name string
	re   *regexp.Regexp
}

// privateMarkerRules holds generic patterns only. Operator-specific values
// belong in the workspace deny-list, never in this file.
var privateMarkerRules = []markerRule{
	{"rfc1918-10", regexp.MustCompile(`\b10\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)},
	{"rfc1918-192.168", regexp.MustCompile(`\b192\.168\.\d{1,3}\.\d{1,3}\b`)},
	{"rfc1918-172.16", regexp.MustCompile(`\b172\.(1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}\b`)},
	{"ticket-id", regexp.MustCompile(`\b[PB]-\d{4}\b`)},
	{"planning-words", regexp.MustCompile(`(?i)\b(orchestrat\w*|subagent|recon dossier|execution[- ]plan)\b`)},
	{"phase-number", regexp.MustCompile(`(?i)\bphase \d+\b`)},
	{"round-number", regexp.MustCompile(`(?i)\bround \d+\b`)},
	{"ai-attribution", regexp.MustCompile(`(?i)co-authored-by: claude|generated with claude`)},
}

// markerScanRoots are scanned relative to the test working directory
// (api/cmd/cashtop).
var markerScanRoots = []string{
	".",
	"../../internal/poolfmt",
	"../../internal/fixturegen",
	"../../internal/apitypes",
	"../../internal/logtail",
	"../../internal/zmqsub",
	"../../internal/poolevents",
	"../../internal/noderpc",
}

func TestNoPrivateMarkers(t *testing.T) {
	self, err := filepath.Abs("markers_test.go")
	if err != nil {
		t.Fatalf("resolve self path: %v", err)
	}

	scanned := 0
	for _, root := range markerScanRoots {
		walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.Type().IsRegular() {
				return nil
			}
			abs, err := filepath.Abs(path)
			if err != nil {
				return err
			}
			if abs == self {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if bytes.IndexByte(data, 0) >= 0 {
				return nil // binary file
			}
			scanned++
			scanMarkers(t, path, string(data))
			return nil
		})
		if walkErr != nil {
			t.Fatalf("walk %s: %v", root, walkErr)
		}
	}

	if scanned == 0 {
		t.Fatal("no files scanned; scan roots are wrong")
	}
	t.Logf("scanned %d text files", scanned)
}

func scanMarkers(t *testing.T, path, content string) {
	t.Helper()
	for i, line := range strings.Split(content, "\n") {
		for _, rule := range privateMarkerRules {
			if m := rule.re.FindString(line); m != "" {
				t.Errorf("%s:%d: %s marker %q", path, i+1, rule.name, m)
			}
		}
	}
}
