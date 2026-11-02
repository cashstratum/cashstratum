package app

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"cashstratumapi/internal/fixturegen"
)

// snapshot runs on the node loop while refreshFiles runs on the main loop;
// the maps refreshFiles publishes must never be written after publication.
func TestRaceSnapshotDuringRefresh(t *testing.T) {
	_, dir := tree(t, false)
	a := open(t, dir)
	a.refreshFiles()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					a.snapshot(false)
				}
			}
		}()
	}

	for i := 0; i < 60; i++ {
		nd := filepath.Join(dir, "logs", fmt.Sprintf("7ffffff%x", i%16))
		if err := os.MkdirAll(nd, 0o755); err != nil {
			t.Fatal(err)
		}
		line := fmt.Sprintf(`{"workinfoid":1,"diff":%d,"sdiff":1,"result":true,"createdate":"%d,0","workername":"%s","username":"%s","address":"%s","useragent":"rig/%d"}`+"\n",
			i+1, fixturegen.FixtureStart.Unix()+int64(60+i), fixturegen.BareAddr(5)+fmt.Sprintf(".w%d", i), fixturegen.BareAddr(5), fixturegen.IP(9), i)
		if err := os.WriteFile(filepath.Join(nd, "0000000000000001.sharelog"), []byte(line), 0o644); err != nil {
			t.Fatal(err)
		}
		a.refreshFiles()
	}
	close(stop)
	wg.Wait()
}
