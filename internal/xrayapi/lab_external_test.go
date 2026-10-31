package xrayapi_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"zic-xray-agent/internal/accounting"
	"zic-xray-agent/internal/daemon"
	"zic-xray-agent/internal/lab"
	"zic-xray-agent/internal/panel"
	"zic-xray-agent/internal/xrayapi"
)

type quietEvents struct{}

func (quietEvents) Event(string) error { return nil }
func newLab(t *testing.T) lab.Config {
	t.Helper()
	root := t.TempDir()
	for attempt := 0; attempt < 30; attempt++ {
		base := 30000 + int(time.Now().UnixNano()%20000)
		c, err := lab.Initialize(root, base, filepath.Join(root, "state"), filepath.Join(root, "core.pid"))
		if err == nil {
			return c
		}
	}
	t.Fatal("could not allocate six loopback lab ports")
	return lab.Config{}
}
func TestLabInitializationIsSyntheticAndNeverOverwrites(t *testing.T) {
	c := newLab(t)
	if !c.Synthetic || c.Users[0].UUID == c.Users[1].UUID || c.Credentials().NodeID != lab.NodeID {
		t.Fatal("not distinct generated lab identities")
	}
	path := filepath.Join(c.Root, "config", "lab.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lab.Initialize(c.Root, c.BasePort, c.AgentState, c.CorePIDFile); err == nil {
		t.Fatal("existing lab overwritten")
	}
	after, _ := os.ReadFile(path)
	if string(before) != string(after) {
		t.Fatal("private manifest changed")
	}
}

func TestLabAccountingRejectsIncompleteOrChangedPhaseEvidence(t *testing.T) {
	c := newLab(t)
	known := map[int64]lab.Bytes{}
	rows := []lab.UUIDResult{}
	for _, u := range c.Users {
		known[u.ID] = lab.Bytes{Upload: 4096, Download: 8192}
		rows = append(rows, lab.UUIDResult{UUID: u.UUID, UserID: u.ID, Expected: known[u.ID], Stats: known[u.ID], Exact: true})
	}
	before := lab.RecoverySnapshot{LabID: c.LabID, CoreEpoch: "core-1", AgentIdentity: "agent-1", BootID: "boot-1", Known: map[int64]lab.Bytes{}}
	after := before
	after.Known = known
	summary := lab.Summary{LabID: c.LabID, Synthetic: true, Passed: true, ResourcesMeasured: true, UUIDs: rows}
	write := func(name string, value any) string {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(c.Root, name)
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	beforePath := write("before.json", before)
	check := func(a lab.RecoverySnapshot, s lab.Summary) error {
		return lab.CheckPhase(c, beforePath, write("after.json", a), write("summary.json", s), filepath.Join(c.Root, "accounting.json"))
	}
	if err := check(after, summary); err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"core-restarted", "agent-restarted", "sending", "not-measured", "missing-uuid", "stats-mismatch", "missing-ledger"} {
		t.Run(scenario, func(t *testing.T) {
			a, s := after, summary
			s.UUIDs = append([]lab.UUIDResult(nil), summary.UUIDs...)
			switch scenario {
			case "core-restarted":
				a.CoreEpoch = "core-2"
			case "agent-restarted":
				a.AgentIdentity = "agent-2"
			case "sending":
				s.SendingEnabled = true
			case "not-measured":
				s.ResourcesMeasured = false
			case "missing-uuid":
				s.UUIDs = s.UUIDs[:1]
			case "stats-mismatch":
				s.UUIDs[0].Stats.Upload++
			case "missing-ledger":
				a.Known = map[int64]lab.Bytes{}
			}
			if err := check(a, s); err == nil {
				t.Fatal("incorrect phase evidence accepted")
			}
		})
	}
}
func TestPackagedLabFixtureAndAccountingWithOfficialCore(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("requires independently verified official core")
	}
	c := newLab(t)
	ctx, stop := context.WithCancel(context.Background())
	defer stop()
	serveResult := make(chan error, 1)
	go func() {
		serveResult <- lab.Serve(ctx, c, filepath.Join(c.Root, "config", "cert.pem"), filepath.Join(c.Root, "config", "key.pem"))
	}()
	log, err := os.Create(filepath.Join(c.Root, "core.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	command := exec.Command(binary, "run", "-config", filepath.Join(c.Root, "config", "xray.json"))
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		command.Process.Kill()
		command.Wait()
		stop()
		select {
		case <-serveResult:
		case <-time.After(15 * time.Second):
			t.Error("lab servers did not stop")
		}
	}()
	if err := os.WriteFile(c.CorePIDFile, []byte(strconv.Itoa(command.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	ready, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := lab.Ready(ready, c, "", false, false); err != nil {
		t.Fatal(err)
	}
	httpClient, err := lab.HTTPClient(c, "")
	if err != nil {
		t.Fatal(err)
	}
	defer httpClient.CloseIdleConnections()
	// Explicitly prove the standing mock can never acknowledge a traffic POST.
	response, err := httpClient.Post(c.Credentials().PanelURL+"/api/v1/server/UniProxy/push", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("standing lab mock accepted traffic")
	}
	core, err := xrayapi.Connect(c.Address(1))
	if err != nil {
		t.Fatal(err)
	}
	defer core.Close()
	panelClient := panel.NewClient()
	panelClient.HTTP = httpClient
	agentCtx, stopAgent := context.WithCancel(ctx)
	defer stopAgent()
	agentResult := make(chan error, 1)
	runner := daemon.Runner{Config: daemon.Config{Credentials: c.Credentials(), Mode: "observe", PanelCommit: panel.ProductionCommit, XrayEndpoint: c.Address(1), XrayPIDFile: c.CorePIDFile}, Panel: panelClient, Stats: core, Epoch: func() (string, error) { return fmt.Sprintf("fixture-%d", command.Process.Pid), nil }, Log: quietEvents{}, StateDir: c.AgentState}
	go func() { agentResult <- runner.Run(agentCtx) }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := accounting.Inspect(filepath.Join(c.AgentState, "traffic.json")); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("agent did not initialize ledger")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Native Windows tests verify functional correctness only; resource CSV is
	// mandatory in the Linux CLI and is never simulated as measured here.
	summary, err := lab.Measure(ctx, c, lab.MeasureOptions{Connections: 10, Duration: time.Second, Interval: 50 * time.Millisecond, Chunk: 4096, Output: filepath.Join(c.Root, "phase"), FunctionalOnly: true})
	if err != nil || !summary.Passed || summary.ResourcesMeasured || len(summary.UUIDs) != 2 {
		t.Fatal("packaged lab data path", summary, err)
	}
	stopAgent()
	select {
	case err := <-agentResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("agent did not stop")
	}
	known, ledger, err := lab.LedgerTotals(filepath.Join(c.AgentState, "traffic.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range summary.UUIDs {
		if known[u.UserID] != u.Expected {
			t.Fatal("standalone lab ledger differs from per-UUID traffic")
		}
	}
	if len(ledger.Queue) != 1 || ledger.Queue[0].State != accounting.Pending {
		t.Fatal("observer attempted sending or did not queue bytes")
	}
	// All listeners are loopback, including both synthetic SOCKS clients.
	for i := 0; i < 6; i++ {
		host, _, err := net.SplitHostPort(c.Address(i))
		if err != nil || host != "127.0.0.1" {
			t.Fatal("nonlocal fixture listener")
		}
	}
}
