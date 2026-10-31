package daemon

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"zic-xray-agent/internal/accounting"
	"zic-xray-agent/internal/panel"
)

const nodeFixture = `{"server_port":443,"network":"tcp","tls":0,"base_config":{"pull_interval":60,"push_interval":60}}`

type fakePanel struct {
	configETag, userETag string
	failUsers            bool
	cancel               context.CancelFunc
}

type offlinePanel struct{ calls int }

func (p *offlinePanel) FetchConfig(context.Context, panel.Credentials, string) (panel.Result, error) {
	p.calls++
	return panel.Result{}, errors.New("offline")
}
func (p *offlinePanel) FetchUsers(context.Context, panel.Credentials, string) (panel.UsersResult, error) {
	panic("must not fetch users after config failure")
}
func TestPanelBackoffDoesNotSlowStatsAndQueue(t *testing.T) {
	p := &offlinePanel{}
	stats := &fakeStats{}
	cfg := config()
	cfg.Pull = 1
	cfg.Push = 1
	r := Runner{Config: cfg, Panel: p, Stats: stats, Epoch: func() (string, error) { return "epoch", nil }, Log: fakeLog{}, StateDir: t.TempDir()}
	ctx, cancel := context.WithTimeout(context.Background(), 6500*time.Millisecond)
	defer cancel()
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if stats.calls < p.calls+2 {
		t.Fatal("panel backoff stretched traffic sampling", stats.calls, p.calls)
	}
	l, err := accounting.Open(filepath.Join(r.StateDir, "traffic.json"), r.Config.Identity())
	if err != nil || len(l.Queue) != 1 || l.Queue[0].State != accounting.Pending {
		t.Fatal("offline traffic was not bounded/coalesced", err)
	}
}

func TestAuthorizationLeaseDoesNotTrustOldOrFutureCache(t *testing.T) {
	r := Runner{Config: config()}
	if err := r.Config.Validate(); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for _, last := range []time.Time{{}, now.Add(-121 * time.Second), now.Add(time.Hour)} {
		r.snapshot.LastSuccess = last
		if !r.authorizationStale(now) {
			t.Fatal("stale/future authorization trusted")
		}
	}
	r.snapshot.LastSuccess = now
	if r.authorizationStale(now) {
		t.Fatal("fresh authorization rejected")
	}
}

func (p *fakePanel) FetchConfig(_ context.Context, _ panel.Credentials, etag string) (panel.Result, error) {
	p.configETag = etag
	if p.cancel != nil {
		p.cancel()
		return panel.Result{}, errors.New("offline")
	}
	return panel.Result{Raw: []byte(nodeFixture), ETag: "config-1", PullInterval: 60, PushInterval: 60}, nil
}
func (p *fakePanel) FetchUsers(_ context.Context, _ panel.Credentials, etag string) (panel.UsersResult, error) {
	p.userETag = etag
	if p.failUsers {
		return panel.UsersResult{}, errors.New("offline")
	}
	return panel.UsersResult{Users: []panel.User{}, ETag: "users-1"}, nil
}

type fakeLog struct{}

func (fakeLog) Event(string) error { return nil }

type fakeStats struct{ calls int }

func (s *fakeStats) Counters(context.Context, int64) (map[string]accounting.Counter, error) {
	s.calls++
	return map[string]accounting.Counter{"counter": {UserID: 42, Direction: "upload", Value: uint64(s.calls)}}, nil
}
func config() Config {
	return Config{Credentials: panel.Credentials{PanelURL: "https://panel.example", Token: "secret", NodeID: 42}, PanelCommit: panel.ProductionCommit}
}

func TestSnapshotETagsAdvanceOnlyAfterBothRequestsCommit(t *testing.T) {
	p := &fakePanel{}
	r := Runner{Config: config(), Panel: p, Log: fakeLog{}, StateDir: t.TempDir()}
	unlock, err := r.Initialize()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	p.failUsers = true
	if err := r.Sync(context.Background()); err == nil {
		t.Fatal("network failure ignored")
	}
	if r.snapshot.ConfigETag != "config-1" || r.snapshot.UsersETag != "users-1" {
		t.Fatal("cache changed on failure")
	}
	unlock()
	r2 := Runner{Config: config(), Panel: p, Log: fakeLog{}, StateDir: r.StateDir}
	unlock, err = r2.Initialize()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	p.failUsers = false
	if err := r2.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if p.configETag != "config-1" || p.userETag != "users-1" {
		t.Fatal("ETags not restored after restart")
	}
}

func TestPanelOfflineDoesNotPreventTrafficCheckpoint(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stats := &fakeStats{}
	r := Runner{Config: config(), Panel: &fakePanel{cancel: cancel}, Stats: stats, Epoch: func() (string, error) { return "epoch", nil }, Log: fakeLog{}, StateDir: t.TempDir()}
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if stats.calls != 2 { // periodic sample plus a final graceful-shutdown sample
		t.Fatal("stats depended on panel availability")
	}
	l, err := accounting.Open(filepath.Join(r.StateDir, "traffic.json"), r.Config.Identity())
	if err != nil || l.Epoch != "epoch" {
		t.Fatal("traffic not durable while offline", err)
	}
}

func TestCoreRestartBetweenReadsRejectsSample(t *testing.T) {
	calls := 0
	r := Runner{Config: config(), Panel: &fakePanel{}, Stats: &fakeStats{}, Epoch: func() (string, error) {
		calls++
		if calls == 1 {
			return "old", nil
		}
		return "new", nil
	}, Log: fakeLog{}, StateDir: t.TempDir()}
	unlock, err := r.Initialize()
	if err != nil {
		t.Fatal(err)
	}
	defer unlock()
	if r.Collect(context.Background()) == nil {
		t.Fatal("mixed-epoch snapshot accepted")
	}
	if r.ledger.Epoch != "" {
		t.Fatal("ledger mutated by mixed-epoch read")
	}
}

func TestManageAndUnknownContractAreBlocked(t *testing.T) {
	c := config()
	c.Mode = "manage"
	if c.Validate() == nil {
		t.Fatal("unverified production management enabled")
	}
	c = config()
	c.PanelCommit = "unverified"
	if c.Validate() == nil {
		t.Fatal("unknown panel contract accepted")
	}
}
