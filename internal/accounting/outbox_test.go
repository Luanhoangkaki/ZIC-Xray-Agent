package accounting

import (
	"path/filepath"
	"testing"
)

func queued(t *testing.T) (*Ledger, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "traffic.json")
	l, err := Open(path, "node")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Observe(path, "core-a", sample(100)); err != nil {
		t.Fatal(err)
	}
	if err := l.Observe(path, "core-a", sample(160)); err != nil {
		t.Fatal(err)
	}
	if err := l.Enqueue(path); err != nil {
		t.Fatal(err)
	}
	return l, path
}
func TestAtomicQueueTransferCoalescingAndRestart(t *testing.T) {
	l, path := queued(t)
	id := l.Queue[0].ID
	if l.Queue[0].Payload[42] != [2]uint64{60, 0} || l.Counters["counter"].Pending != 0 {
		t.Fatal("pending not transferred atomically")
	}
	l, err := Open(path, "node")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Observe(path, "core-a", sample(180)); err != nil {
		t.Fatal(err)
	}
	if err := l.Enqueue(path); err != nil {
		t.Fatal(err)
	}
	if len(l.Queue) != 1 || l.Queue[0].ID != id || l.Queue[0].Payload[42] != [2]uint64{80, 0} {
		t.Fatal("unsent coalescing lost/duplicated traffic")
	}
	if err := l.Observe(path, "core-a", sample(180)); err != nil {
		t.Fatal(err)
	}
	if err := l.Enqueue(path); err != nil {
		t.Fatal(err)
	}
	if l.Queue[0].Payload[42][0] != 80 {
		t.Fatal("repeated counter snapshot double counted")
	}
}
func TestCrashInSendingBecomesUnknownAndIsNeverRetried(t *testing.T) {
	l, path := queued(t)
	batch, err := l.BeginNext(path)
	if err != nil {
		t.Fatal(err)
	}
	l, err = Open(path, "node")
	if err != nil {
		t.Fatal(err)
	}
	if l.Queue[0].State != Unknown || l.Queue[0].Attempts != 1 {
		t.Fatal("crash did not quarantine attempted payload")
	}
	if _, err := l.BeginNext(path); err == nil {
		t.Fatal("UNKNOWN was retried")
	}
	if err := l.Observe(path, "core-a", sample(200)); err != nil {
		t.Fatal(err)
	}
	if err := l.Enqueue(path); err != nil {
		t.Fatal(err)
	}
	if len(l.Queue) != 2 || l.Queue[0].Digest != batch.Digest || l.Queue[0].Payload[42][0] != 60 || l.Queue[1].Payload[42][0] != 40 {
		t.Fatal("attempted payload mutated or traffic lost while quarantined")
	}
	if err := l.ResolveUnknown(path, batch.ID, "stale-digest", false, "ticket-42"); err == nil {
		t.Fatal("stale reconciliation approved")
	}
	if err := l.ResolveUnknown(path, batch.ID, batch.Digest, false, "ticket-42"); err != nil {
		t.Fatal(err)
	}
	if l.Queue[0].State != ConfirmedNotApplied || l.Queue[0].Attempts != 1 {
		t.Fatal("manual decision requeued old batch")
	}
	newBatch, err := l.BeginNext(path)
	if err != nil || newBatch.ID == batch.ID {
		t.Fatal("manual resolution replayed original batch", err)
	}
}
func TestQueueDiskFailureDoesNotClearPending(t *testing.T) {
	l, path := queued(t)
	if err := l.Observe(path, "core-a", sample(200)); err != nil {
		t.Fatal(err)
	}
	if err := l.Enqueue(filepath.Join(path, "invalid")); err == nil {
		t.Fatal("disk failure ignored")
	}
	if l.Counters["counter"].Pending != 40 || l.Queue[0].Payload[42][0] != 60 {
		t.Fatal("RAM advanced on failed checkpoint")
	}
}
func TestRebootRetainsKnownTrafficAndRecordsUnobservedTail(t *testing.T) {
	l, path := queued(t)
	if err := l.Observe(path, "core-a", sample(180)); err != nil {
		t.Fatal(err)
	}
	if err := l.TransitionEpoch(path, "core-b", sample(7)); err != nil {
		t.Fatal(err)
	}
	l, err := Open(path, "node")
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Incidents) != 1 || l.Incidents[0].Resolved || l.Queue[0].Payload[42][0] != 80 || l.Counters["counter"].Pending != 7 {
		t.Fatal("reboot lost known bytes or hid old-core gap")
	}
	if err := l.Observe(path, "core-b", sample(7)); err != nil {
		t.Fatal(err)
	}
	if err := l.Enqueue(path); err != nil {
		t.Fatal(err)
	}
	if len(l.Queue) != 2 || l.Queue[1].Payload[42][0] != 7 {
		t.Fatal("new epoch counted twice")
	}
	if _, err := l.BeginNext(path); err == nil {
		t.Fatal("open data gap did not block sending")
	}
}
