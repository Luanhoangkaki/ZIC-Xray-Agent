package accounting

import (
	"os"
	"path/filepath"
	"testing"
)

func sample(value uint64) map[string]Counter {
	return map[string]Counter{"counter": {UserID: 42, Direction: "upload", Value: value}}
}

func TestAgentRestartAndRepeatedSnapshotDoNotDoubleCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.json")
	l, err := Open(path, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Observe(path, "epoch-a", sample(1000)); err != nil {
		t.Fatal(err)
	}
	if err := l.Observe(path, "epoch-a", sample(1020)); err != nil {
		t.Fatal(err)
	}
	l, err = Open(path, "node-a")
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Observe(path, "epoch-a", sample(1025)); err != nil {
		t.Fatal(err)
	}
	if err := l.Observe(path, "epoch-a", sample(1025)); err != nil {
		t.Fatal(err)
	}
	payload, err := l.TrafficPayload()
	if err != nil || payload[42] != [2]uint64{25, 0} {
		t.Fatal("traffic lost or duplicated across agent restart", payload, err)
	}
	if _, err := Open(path, "other-node"); err == nil {
		t.Fatal("cross-node ledger accepted")
	}
}

func TestAnomaliesRetainLastDurableCheckpoint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "traffic.json")
	l, _ := Open(path, "node")
	if err := l.Observe(path, "epoch", sample(100)); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	for _, test := range []struct {
		epoch  string
		sample map[string]Counter
	}{{"epoch", sample(99)}, {"new-epoch", sample(110)}, {"epoch", map[string]Counter{}}} {
		if l.Observe(path, test.epoch, test.sample) == nil {
			t.Fatal("accounting anomaly ignored")
		}
		after, _ := os.ReadFile(path)
		if string(before) != string(after) {
			t.Fatal("anomaly overwrote evidence")
		}
	}
	if err := l.Observe(filepath.Join(path, "not-a-directory"), "epoch", sample(150)); err == nil {
		t.Fatal("disk error ignored")
	}
	if l.Counters["counter"].Value != 100 {
		t.Fatal("RAM advanced before durable checkpoint")
	}
}
