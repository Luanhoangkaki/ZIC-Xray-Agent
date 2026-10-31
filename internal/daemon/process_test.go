package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"zic-xray-agent/internal/accounting"
	"zic-xray-agent/internal/durable"
	"zic-xray-agent/internal/panel"
)

type crashPanel struct{}

func (crashPanel) FetchConfig(context.Context, panel.Credentials, string) (panel.Result, error) {
	os.Exit(23)
	return panel.Result{}, nil
}
func (crashPanel) FetchUsers(context.Context, panel.Credentials, string) (panel.UsersResult, error) {
	panic("unreachable")
}

type fixedStats struct{}

func (fixedStats) Counters(context.Context, int64) (map[string]accounting.Counter, error) {
	return map[string]accounting.Counter{"counter": {UserID: 42, Direction: "upload", Value: 160}}, nil
}

func TestDaemonCrashHelper(t *testing.T) {
	if os.Getenv("ZIC_DAEMON_CRASH_HELPER") != "1" {
		return
	}
	dir := os.Getenv("ZIC_TEST_STATE")
	r := Runner{Config: config(), Panel: crashPanel{}, Stats: fixedStats{}, Epoch: func() (string, error) { return "epoch", nil }, Log: fakeLog{}, StateDir: dir}
	if err := r.Run(context.Background()); err != nil {
		os.Exit(24)
	}
	os.Exit(25)
}
func TestActualProcessCrashAndRestartPreserveCounterDeltaOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "traffic.json")
	cfg := config()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	l, err := accounting.Open(path, cfg.Identity())
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Observe(path, "epoch", map[string]accounting.Counter{"counter": {UserID: 42, Direction: "upload", Value: 100}}); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		command := exec.Command(os.Args[0], "-test.run=^TestDaemonCrashHelper$")
		command.Env = append(os.Environ(), "ZIC_DAEMON_CRASH_HELPER=1", "ZIC_TEST_STATE="+dir)
		err := command.Run()
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 23 {
			t.Fatal("daemon did not crash at selected durable boundary", err)
		}
		unlock, err := durable.Lock(filepath.Join(dir, "agent.lock"))
		if err != nil {
			t.Fatal("crashed process leaked kernel lock", err)
		}
		l, err = accounting.Open(path, cfg.Identity())
		if err != nil {
			unlock()
			t.Fatal(err)
		}
		if l.Counters["counter"].Pending != 60 {
			unlock()
			t.Fatal("restart double-counted/lost already-checkpointed delta")
		}
		unlock()
	}
	if err := l.Enqueue(path); err != nil {
		t.Fatal(err)
	}
	if len(l.Queue) != 1 || l.Queue[0].Payload[42][0] != 60 {
		t.Fatal("crash data was not atomically queued")
	}
}
