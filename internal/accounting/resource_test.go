package accounting

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestIdleEnqueueLeavesDiskAndUnknownQueueUntouched(t *testing.T) {
	path := filepath.Join(t.TempDir(), "checkpoint")
	if os.WriteFile(path, []byte("unchanged checkpoint"), 0600) != nil {
		t.Fatal("setup failed")
	}
	l := &Ledger{Counters: map[string]Counter{"counter": {UserID: 1, Direction: "upload", Value: 99}}, Queue: []Batch{{ID: "opaque-batch", State: Unknown}}}
	before, _ := json.Marshal(l)
	if err := l.Enqueue(path); err != nil {
		t.Fatal("idle enqueue failed")
	}
	after, _ := json.Marshal(l)
	raw, err := os.ReadFile(path)
	if string(before) != string(after) || err != nil || string(raw) != "unchanged checkpoint" {
		t.Fatal("idle enqueue mutated persistent accounting")
	}
}

// Control-plane microbenchmark only. This is not VPS RSS/CPU/SSD or VPN load.
func BenchmarkEnqueueIdle(b *testing.B) {
	for _, count := range []int{5000, MaxCounters} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			l := &Ledger{Counters: make(map[string]Counter, count)}
			for i := 0; i < count; i++ {
				l.Counters[fmt.Sprint(i)] = Counter{UserID: int64(i + 1), Direction: "upload", Value: 100}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := l.Enqueue("unused-idle-path"); err != nil {
					b.Fatal("idle enqueue failed")
				}
			}
		})
	}
}
