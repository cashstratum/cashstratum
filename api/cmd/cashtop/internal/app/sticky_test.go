package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"cashstratumapi/internal/fixturegen"
)

// A new height whose sharelogs hold no line from a worker must not blank that
// worker's useragent or difficulty.
func TestAgentAndDiffSurviveHeightChange(t *testing.T) {
	_, dir := tree(t, false)
	a := open(t, dir)
	a.refreshFiles()
	before := a.Snapshot()
	var name string
	var agent string
	var diff float64
	for _, w := range before.Workers {
		if w.UserAgent != "" && w.Diff > 0 {
			name, agent, diff = w.Name, w.UserAgent, w.Diff
			break
		}
	}
	if name == "" {
		t.Fatal("fixture has no worker with a useragent and difficulty")
	}
	nd := filepath.Join(dir, "logs", "7fffffff")
	if err := os.MkdirAll(nd, 0o755); err != nil {
		t.Fatal(err)
	}
	other := fixturegen.BareAddr(5) + ".newheightonly"
	line := fmt.Sprintf(`{"workinfoid":1,"diff":1,"sdiff":1,"result":true,"createdate":"%d,0","workername":"%s","username":"%s","address":"%s"}`+"\n",
		fixturegen.FixtureStart.Unix()+60, other, fixturegen.BareAddr(5), fixturegen.IP(9))
	if err := os.WriteFile(filepath.Join(nd, "0000000000000001.sharelog"), []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refreshFiles()
	a.refreshFiles()
	if a.stats.shareHeight != 0x7fffffff {
		t.Fatalf("height = %x, want the new directory", a.stats.shareHeight)
	}
	for _, w := range a.Snapshot().Workers {
		if w.Name != name {
			continue
		}
		if w.UserAgent != agent || w.Diff != diff {
			t.Fatalf("%s lost its last-known data: agent %q diff %v, want %q %v", name, w.UserAgent, w.Diff, agent, diff)
		}
		return
	}
	t.Fatalf("worker %s vanished", name)
}
