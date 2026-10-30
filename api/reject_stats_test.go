package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const rejectStatsAddr = "bitcoincash:qzxq7lc575az8tw357ck50wdl7cmn4lc2v9kp3rz4u"

// rejectStatsUserBody is a status file carrying every reject and disconnect
// key at user and worker level, plus a probe worker that must be filtered.
const rejectStatsUserBody = `{
	"hashrate1m": "1.41P",
	"shares": 123456789012,
	"rejected": 287309824,
	"rejectedcount": 274,
	"rejects": {"stale": 3, "duplicate": 0, "lowdiff": 268, "ntime": 0, "badjob": 2, "malformed": 1, "worker": 0},
	"lastreject": 1793327788,
	"lastrejectreason": "Above target",
	"rejectrate1m": "1.21P", "rejectrate5m": "402T", "rejectrate1hr": "33.5T",
	"worker": [
		{
			"workername": "` + rejectStatsAddr + `.4",
			"hashrate1m": "350T",
			"shares": 30000000000,
			"rejected": 287309824,
			"rejectedcount": 274,
			"rejects": {"stale": 3, "duplicate": 0, "lowdiff": 268, "ntime": 0, "badjob": 2, "malformed": 1, "worker": 0},
			"lastreject": 1793327788,
			"lastrejectreason": "Above target",
			"rejectrate1m": "1.21P", "rejectrate5m": "402T", "rejectrate1hr": "33.5T",
			"connects": 61,
			"disconnects": 60,
			"lastdisconnect": 1793327788,
			"lastdisconnectreason": "peer_eof"
		},
		{"workername": "` + rejectStatsAddr + `.` + ProbeWorkerSuffix + `", "shares": 1, "rejectedcount": 9}
	],
	"workers": 2
}`

func fetchUserFileContent(t *testing.T, body string) map[string]any {
	t.Helper()
	dir := useTempUserLogDir(t)
	if err := os.WriteFile(filepath.Join(dir, rejectStatsAddr), []byte(body), 0o644); err != nil {
		t.Fatalf("writing fixture: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/user-file?user="+rejectStatsAddr, nil)
	rec := httptest.NewRecorder()
	handleUserFile(rec, req)

	var resp struct {
		Content map[string]any `json:"content"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decoding response: %v\n%s", err, rec.Body.String())
	}
	if resp.Content == nil {
		t.Fatalf("response has no content: %s", rec.Body.String())
	}
	return resp.Content
}

// Every reject and disconnect key written by the pool reaches the caller
// unchanged at user and worker level, the derived "worker" short name is kept
// on each entry, and probe workers are still filtered out.
func TestUserFilePassesRejectStatsThrough(t *testing.T) {
	content := fetchUserFileContent(t, rejectStatsUserBody)

	var want map[string]any
	if err := json.Unmarshal([]byte(rejectStatsUserBody), &want); err != nil {
		t.Fatalf("fixture is not valid JSON: %v", err)
	}

	for _, k := range []string{"rejected", "rejectedcount", "rejects", "lastreject", "lastrejectreason", "rejectrate1m", "rejectrate5m", "rejectrate1hr"} {
		got, ok := content[k]
		if !ok {
			t.Errorf("user level: key %q missing", k)
			continue
		}
		if !reflect.DeepEqual(got, want[k]) {
			t.Errorf("user level: %q = %v, want %v", k, got, want[k])
		}
	}

	workers, ok := content["worker"].([]any)
	if !ok || len(workers) != 1 {
		t.Fatalf("worker entries = %v, want exactly 1 (probe filtered)", content["worker"])
	}
	entry := workers[0].(map[string]any)
	wantEntry := want["worker"].([]any)[0].(map[string]any)

	for _, k := range []string{
		"rejected", "rejectedcount", "rejects", "lastreject", "lastrejectreason",
		"rejectrate1m", "rejectrate5m", "rejectrate1hr",
		"connects", "disconnects", "lastdisconnect", "lastdisconnectreason",
	} {
		got, ok := entry[k]
		if !ok {
			t.Errorf("worker level: key %q missing", k)
			continue
		}
		if !reflect.DeepEqual(got, wantEntry[k]) {
			t.Errorf("worker level: %q = %v, want %v", k, got, wantEntry[k])
		}
	}

	if entry["worker"] != "4" {
		t.Errorf("derived worker key = %v, want %q", entry["worker"], "4")
	}
	if content["workers"] != float64(1) {
		t.Errorf("workers = %v, want 2 - 1 probe = 1", content["workers"])
	}
}

// A file from an older pool has none of the new keys. They must stay absent
// rather than be synthesised as zero.
func TestUserFileDoesNotInventRejectKeys(t *testing.T) {
	content := fetchUserFileContent(t, `{"shares": 5, "worker": [{"workername": "`+rejectStatsAddr+`.a", "shares": 5}]}`)

	for _, k := range []string{"rejected", "rejectedcount", "rejects", "lastreject", "lastrejectreason"} {
		if _, ok := content[k]; ok {
			t.Errorf("user level: key %q present, want absent", k)
		}
	}
	entry := content["worker"].([]any)[0].(map[string]any)
	for _, k := range []string{"rejectedcount", "connects", "disconnects", "lastdisconnectreason"} {
		if _, ok := entry[k]; ok {
			t.Errorf("worker level: key %q present, want absent", k)
		}
	}
}

// No "/" route is registered, so the root path must 404 rather than serve
// anything.
func TestRootPathIs404(t *testing.T) {
	rec := httptest.NewRecorder()
	newMux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("GET / = %d, want 404", rec.Code)
	}
}
