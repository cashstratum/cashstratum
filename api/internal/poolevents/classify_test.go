package poolevents

import (
	"testing"
	"time"
)

// testClassifier feeds lines to a Classifier and collects every event it
// returns, in order.
type testClassifier struct {
	c      *Classifier
	events *[]any
}

func (tc testClassifier) feed(line string) {
	if evt, ok := tc.c.Classify(line); ok {
		*tc.events = append(*tc.events, evt)
	}
}

func newTestClassifier(clock *fakeClock) (testClassifier, *[]any) {
	var events []any
	return testClassifier{c: NewClassifier(clock.now), events: &events}, &events
}

func TestClassify_BlockAccepted(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 23, 26, 26, 900000000, time.UTC)}
	cls, events := newTestClassifier(clock)

	cls.feed("[2026-08-30 23:26:26.902] BLOCK ACCEPTED!")

	if len(*events) != 1 {
		t.Fatalf("got %d events, want 1", len(*events))
	}
	evt, ok := (*events)[0].(PoolBlockSubmittedEvent)
	if !ok {
		t.Fatalf("event type = %T, want PoolBlockSubmittedEvent", (*events)[0])
	}
	if evt.Event != "pool.block.submitted" {
		t.Errorf("Event = %q, want pool.block.submitted", evt.Event)
	}
	if evt.SolveID == "" || evt.SolveID != evt.EventID {
		t.Errorf("SolveID = %q, EventID = %q -- want equal and non-empty", evt.SolveID, evt.EventID)
	}
}

func TestClassify_SolvedAndConfirmed(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 23, 26, 26, 944000000, time.UTC)}
	cls, events := newTestClassifier(clock)

	cls.feed("[2026-08-30 23:26:26.944] Solved and confirmed block 968432 by 1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS.testrig")

	if len(*events) != 1 {
		t.Fatalf("got %d events, want 1", len(*events))
	}
	evt, ok := (*events)[0].(PoolBlockEvent)
	if !ok {
		t.Fatalf("event type = %T, want PoolBlockEvent", (*events)[0])
	}
	if evt.Event != "pool.block" {
		t.Errorf("Event = %q, want pool.block", evt.Event)
	}
	if evt.Height != 968432 {
		t.Errorf("Height = %d, want 968432", evt.Height)
	}
	if evt.WorkerName != "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS.testrig" {
		t.Errorf("WorkerName = %q, want byte-identical to the log", evt.WorkerName)
	}
	if evt.Username != "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS" {
		t.Errorf("Username = %q, want 1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS", evt.Username)
	}
	if evt.Worker == nil || *evt.Worker != "testrig" {
		t.Errorf("Worker = %v, want \"testrig\"", evt.Worker)
	}
}

func TestClassify_SubmittedButRejected_NoReason(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 23, 26, 26, 958000000, time.UTC)}
	cls, events := newTestClassifier(clock)

	cls.feed("[2026-08-30 23:26:26.958] Submitted, but had block 968432 rejected")

	if len(*events) != 1 {
		t.Fatalf("got %d events, want 1", len(*events))
	}
	evt, ok := (*events)[0].(PoolBlockRejectedEvent)
	if !ok {
		t.Fatalf("event type = %T, want PoolBlockRejectedEvent", (*events)[0])
	}
	if evt.Height != 968432 {
		t.Errorf("Height = %d, want 968432", evt.Height)
	}
	if evt.Reason != nil {
		t.Errorf("Reason = %v, want nil (no SUBMIT BLOCK RETURNED line preceded it)", *evt.Reason)
	}
}

func TestClassify_SubmitBlockReturned_ThenRejected_CapturesReason(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 23, 26, 26, 950000000, time.UTC)}
	cls, events := newTestClassifier(clock)

	cls.feed("[2026-08-30 23:26:26.950] SUBMIT BLOCK RETURNED: inconclusive-not-best-prevblk")
	cls.feed("[2026-08-30 23:26:26.958] Submitted, but had block 968432 rejected")

	if len(*events) != 1 {
		t.Fatalf("got %d events, want 1 (the SUBMIT BLOCK RETURNED line emits nothing on its own)", len(*events))
	}
	evt := (*events)[0].(PoolBlockRejectedEvent)
	if evt.Reason == nil || *evt.Reason != "inconclusive-not-best-prevblk" {
		t.Fatalf("Reason = %v, want \"inconclusive-not-best-prevblk\"", evt.Reason)
	}
}

func TestClassify_ReasonExpiresOutsideItsWindow(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 23, 26, 26, 0, time.UTC)}
	cls, events := newTestClassifier(clock)

	cls.feed("[2026-08-30 23:26:26.000] SUBMIT BLOCK RETURNED: some-stale-reason")
	clock.advance(31 * time.Second) // past ReasonWindow (30s)
	cls.feed("[2026-08-30 23:26:57.000] Submitted, but had block 1 rejected")

	evt := (*events)[0].(PoolBlockRejectedEvent)
	if evt.Reason != nil {
		t.Fatalf("Reason = %q, want nil -- the captured reason is 31s stale, past the 30s window", *evt.Reason)
	}
}

func TestClassify_UnrelatedLineProducesNothing(t *testing.T) {
	cls, events := newTestClassifier(&fakeClock{t: time.Now()})

	cls.feed("[2026-08-30 23:26:26.000] Stratifier: 25 workers, pool hashrate 1.2TH")

	if len(*events) != 0 {
		t.Fatalf("got %d events, want 0 for an unrelated line", len(*events))
	}
}

func TestClassify_SolveIDCarriedOntoConfirmWithinWindow(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 23, 26, 26, 900000000, time.UTC)}
	cls, events := newTestClassifier(clock)

	cls.feed("[2026-08-30 23:26:26.902] BLOCK ACCEPTED!")
	submitted := (*events)[0].(PoolBlockSubmittedEvent)

	clock.advance(1 * time.Second) // well within the 120s window
	cls.feed("[2026-08-30 23:26:27.944] Solved and confirmed block 968432 by addr.rig")
	confirmed := (*events)[1].(PoolBlockEvent)

	if confirmed.SolveID != submitted.SolveID {
		t.Fatalf("confirmed.SolveID = %q, want %q (inherited within the window)", confirmed.SolveID, submitted.SolveID)
	}
}

func TestClassify_SolveIDFreshOutsideWindow(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 8, 30, 23, 26, 26, 900000000, time.UTC)}
	cls, events := newTestClassifier(clock)

	cls.feed("[2026-08-30 23:26:26.902] BLOCK ACCEPTED!")
	submitted := (*events)[0].(PoolBlockSubmittedEvent)

	clock.advance(121 * time.Second) // past the 120s window
	cls.feed("[2026-08-30 23:28:27.944] Solved and confirmed block 968432 by addr.rig")
	confirmed := (*events)[1].(PoolBlockEvent)

	if confirmed.SolveID == submitted.SolveID {
		t.Fatalf("confirmed.SolveID = %q, want a fresh id distinct from the expired %q", confirmed.SolveID, submitted.SolveID)
	}
	if confirmed.SolveID == "" {
		t.Fatalf("confirmed.SolveID is empty, want a freshly minted id even with no open submitted event")
	}
}

func TestClassify_ConfirmWithNoPrecedingSubmittedStillEmitsWithFreshID(t *testing.T) {
	cls, events := newTestClassifier(&fakeClock{t: time.Now()})

	cls.feed("[2026-08-30 23:26:26.944] Solved and confirmed block 968432 by addr.rig")

	if len(*events) != 1 {
		t.Fatalf("got %d events, want 1", len(*events))
	}
	evt := (*events)[0].(PoolBlockEvent)
	if evt.SolveID == "" {
		t.Fatalf("SolveID is empty, want a freshly minted id")
	}
}

func TestSplitWorkerName(t *testing.T) {
	cases := []struct {
		workername   string
		wantUsername string
		wantWorker   *string
	}{
		{"1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS", "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS", nil},
		{"1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS.testrig", "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS", strPtr("testrig")},
		{"1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS_testrig", "1AGQcP3KNqTAQkZQA2LBCKqvYn1C4V7cS", strPtr("testrig")},
		{"addr.rig.1", "addr", strPtr("rig.1")},
	}

	for _, c := range cases {
		gotUsername, gotWorker := SplitWorkerName(c.workername)
		if gotUsername != c.wantUsername {
			t.Errorf("SplitWorkerName(%q) username = %q, want %q", c.workername, gotUsername, c.wantUsername)
		}
		if (gotWorker == nil) != (c.wantWorker == nil) {
			t.Errorf("SplitWorkerName(%q) worker = %v, want %v", c.workername, gotWorker, c.wantWorker)
			continue
		}
		if gotWorker != nil && *gotWorker != *c.wantWorker {
			t.Errorf("SplitWorkerName(%q) worker = %q, want %q", c.workername, *gotWorker, *c.wantWorker)
		}
	}
}

func strPtr(s string) *string { return &s }
