package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"zic-xray-agent/internal/accounting"
)

func TestTrafficCLIStatusAndManualResolutionCannotSend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traffic.json")
	l, err := accounting.Open(path, "test")
	if err != nil {
		t.Fatal(err)
	}
	sample := map[string]accounting.Counter{"counter": {UserID: 42, Direction: "upload"}}
	if err := l.Observe(path, "epoch", sample); err != nil {
		t.Fatal(err)
	}
	sample["counter"] = accounting.Counter{UserID: 42, Direction: "upload", Value: 100}
	if err := l.Observe(path, "epoch", sample); err != nil {
		t.Fatal(err)
	}
	if err := l.Enqueue(path); err != nil {
		t.Fatal(err)
	}
	batch, err := l.BeginNext(path)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := trafficCommand([]string{"status", "--state-dir", dir}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), `"sending_enabled":false`) || strings.Contains(output.String(), `"user_id"`) {
		t.Fatal("status leaks user data or implies sending enabled")
	}
	onDisk, err := accounting.Inspect(path)
	if err != nil || onDisk.Queue[0].State != accounting.Sending {
		t.Fatal("read-only status mutated an active attempt", err)
	}
	if err := trafficCommand([]string{"resolve", "--state-dir", dir, "--id", batch.ID, "--digest", batch.Digest, "--decision", "not-applied", "--evidence", "TICKET-42"}, &output); err != nil {
		t.Fatal(err)
	}
	if err := trafficCommand([]string{"send", "--state-dir", dir}, &output); err == nil {
		t.Fatal("CLI enabled network sending")
	}
	l, err = accounting.Open(path, "test")
	if err != nil || l.Queue[0].State != accounting.ConfirmedNotApplied {
		t.Fatal("manual decision automatically retried/requeued", err)
	}
}
