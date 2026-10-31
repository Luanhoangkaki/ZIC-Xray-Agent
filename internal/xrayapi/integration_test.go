package xrayapi

import (
	"bytes"
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/net/proxy"

	"zic-xray-agent/internal/accounting"
	"zic-xray-agent/internal/control"
	"zic-xray-agent/internal/daemon"
	"zic-xray-agent/internal/panel"
)

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return p
}

type integrationEvents struct{}

func (integrationEvents) Event(string) error { return nil }

// Opt-in real-core integration. Runs ONE disposable official core on loopback;
// no production panel, systemd service or existing Xray config is touched.
func TestOfficialCoreIntegration(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("set XRAY_BINARY to an independently verified official Xray executable")
	}
	version, err := exec.Command(binary, "version").Output()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("official-core fixture version: %s", bytes.SplitN(version, []byte("\n"), 2)[0])
	apiPort, managedPort, socksPort, socksPortB := freePort(t), freePort(t), freePort(t), freePort(t)
	const uuid = "15a4235c-39ac-42ea-a1b1-100000000042"
	const uuidB = "15a4235c-39ac-42ea-a1b1-100000000043"
	config := fmt.Sprintf(`{
	"log":{"loglevel":"none"},
	"api":{"tag":"api","services":["HandlerService","StatsService"]},
	"stats":{},"policy":{"levels":{"0":{"statsUserUplink":true,"statsUserDownlink":true}}},
	"inbounds":[
	 {"tag":"api-in","listen":"127.0.0.1","port":%d,"protocol":"dokodemo-door","settings":{"address":"127.0.0.1"}},
	 {"tag":"test-socks","listen":"127.0.0.1","port":%d,"protocol":"socks","settings":{"auth":"noauth","udp":false}},
	 {"tag":"test-socks-b","listen":"127.0.0.1","port":%d,"protocol":"socks","settings":{"auth":"noauth","udp":false}}
	],
	"outbounds":[{"tag":"direct","protocol":"freedom","settings":{"finalRules":[{"action":"allow","ip":["127.0.0.1/32"]}]}},{"tag":"test-vless","protocol":"vless","settings":{"vnext":[{"address":"127.0.0.1","port":%d,"users":[{"id":%q,"encryption":"none"}]}]},"streamSettings":{"network":"tcp","security":"none"}},{"tag":"test-vless-b","protocol":"vless","settings":{"vnext":[{"address":"127.0.0.1","port":%d,"users":[{"id":%q,"encryption":"none"}]}]},"streamSettings":{"network":"tcp","security":"none"}}],
	"routing":{"rules":[{"type":"field","inboundTag":["api-in"],"outboundTag":"api"},{"type":"field","inboundTag":["test-socks"],"outboundTag":"test-vless"},{"type":"field","inboundTag":["test-socks-b"],"outboundTag":"test-vless-b"}]}
	}`, apiPort, socksPort, socksPortB, managedPort, uuid, managedPort, uuidB)
	dir := t.TempDir()
	path := filepath.Join(dir, "xray.json")
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	log, err := os.Create(filepath.Join(dir, "core.log"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "run", "-config", path)
	cmd.Dir = dir
	cmd.Stdout = log
	cmd.Stderr = log
	if err := cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait(); _ = log.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	client, err := Connect(net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort)))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := client.Counters(ctx, 99); err == nil {
			break
		}
		if time.Now().After(deadline) {
			data, _ := os.ReadFile(log.Name())
			t.Fatalf("core API did not start: %s", data)
		}
		time.Sleep(20 * time.Millisecond)
	}
	const tag = "zic-node-99"
	if err := client.AddInbound(ctx, tag, "127.0.0.1", panel.VLESSSpec{Port: uint32(managedPort)}); err != nil {
		t.Fatal("AddInbound", err)
	}
	u := panel.User{ID: 42, UUID: uuid}
	if err := client.AddUser(ctx, tag, User{Email: u.Email(99), UUID: uuid}); err != nil {
		t.Fatal("AddUser", err)
	}
	users, err := client.Users(ctx, tag)
	if err != nil || len(users) != 1 || users[0].UUID != uuid {
		t.Fatal("GetInboundUsers", users, err)
	}
	if err := client.ReconcileUsers(ctx, tag, 99, "", []panel.User{u}, nil); err != nil {
		t.Fatal("idempotent reconciliation", err)
	}
	uB := panel.User{ID: 43, UUID: uuidB}
	if err := client.ReconcileUsers(ctx, tag, 99, "", []panel.User{u, uB}, nil); err != nil {
		t.Fatal("second user reconciliation", err)
	}
	ledgerPath := filepath.Join(dir, "traffic.json")
	ledger, err := accounting.Open(ledgerPath, "fixture")
	if err != nil {
		t.Fatal(err)
	}
	initial, err := client.Counters(ctx, 99)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Observe(ledgerPath, "one-core-epoch", initial); err != nil {
		t.Fatal(err)
	}
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			conn, err := echo.Accept()
			if err != nil {
				return
			}
			go func() { defer conn.Close(); io.Copy(conn, conn) }()
		}
	}()
	dialer, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPort)), nil, &net.Dialer{Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	dialerB, err := proxy.SOCKS5("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(socksPortB)), nil, &net.Dialer{Timeout: 3 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	dialers := []proxy.Dialer{dialer, dialerB}
	payload := bytes.Repeat([]byte("zic-test"), 512)
	expected := uint64(0)
	for _, connections := range []int{10, 20, 30} {
		t.Run(fmt.Sprintf("%d_connections", connections), func(t *testing.T) {
			var workers sync.WaitGroup
			for i := 0; i < connections; i++ {
				workers.Add(1)
				go func(index int) {
					defer workers.Done()
					conn, err := dialers[index%2].Dial("tcp", echo.Addr().String())
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.Close()
					conn.SetDeadline(time.Now().Add(5 * time.Second))
					if _, err := conn.Write(payload); err != nil {
						t.Error(err)
						return
					}
					reply := make([]byte, len(payload))
					if _, err := io.ReadFull(conn, reply); err != nil || !bytes.Equal(reply, payload) {
						t.Error("VLESS echo failed", err)
					}
				}(i)
			}
			workers.Wait()
			if t.Failed() {
				data, _ := os.ReadFile(log.Name())
				if len(data) > 12000 {
					data = data[len(data)-12000:]
				}
				t.Logf("isolated fixture core log: %s", data)
			}
		})
		if t.Failed() {
			t.Fatal("stop after first failed traffic batch")
		}
		expected += uint64(connections * len(payload))
	}
	stats, err := client.Counters(ctx, 99)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Observe(ledgerPath, "one-core-epoch", stats); err != nil {
		t.Fatal(err)
	}
	ledger, err = accounting.Open(ledgerPath, "fixture")
	if err != nil {
		t.Fatal(err)
	} // agent restart
	again, err := client.Counters(ctx, 99)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.Observe(ledgerPath, "one-core-epoch", again); err != nil {
		t.Fatal(err)
	}
	traffic, err := ledger.TrafficPayload()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("total payload each direction=%d; each user's pending upload/download: user42=%v user43=%v", expected, traffic[42], traffic[43])
	if len(traffic) != 2 || traffic[42] != [2]uint64{expected / 2, expected / 2} || traffic[43] != [2]uint64{expected / 2, expected / 2} {
		t.Fatalf("unexpected per-user accounting: got %v expected per-user bytes in each direction %d", traffic, expected/2)
	}
	checkpointed := false
	if err := client.ReconcileUsers(ctx, tag, 99, "", nil, func() error { checkpointed = true; return nil }); err != nil || !checkpointed {
		t.Fatal("user revocation/checkpoint", err)
	}
	if users, err := client.Users(ctx, tag); err != nil || len(users) != 0 {
		t.Fatal("revoked user remains", err)
	}
	if err := client.RemoveInbound(ctx, tag); err != nil {
		t.Fatal("RemoveInbound", err)
	}
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	realityRaw, _ := json.Marshal(map[string]any{"server_port": managedPort, "network": "tcp", "tls": 2, "flow": "xtls-rprx-vision", "tls_settings": map[string]any{"server_name": "example.org", "dest": echo.Addr().String(), "private_key": base64.RawURLEncoding.EncodeToString(key.Bytes()), "public_key": base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), "short_id": "aabbccdd"}})
	spec, err := panel.ParseVLESS(realityRaw)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.AddInbound(ctx, tag, "127.0.0.1", spec); err != nil {
		t.Fatal("REALITY AddInbound", err)
	}
	if err := client.AddUser(ctx, tag, User{Email: u.Email(99), UUID: uuid, Flow: "xtls-rprx-vision"}); err != nil {
		t.Fatal("REALITY AddUser", err)
	}
	if err := client.RemoveInbound(ctx, tag); err != nil {
		t.Fatal(err)
	}
	// Exercise the lifecycle controller against the real core, not just mocks.
	controller := &control.Controller{Core: client, Path: filepath.Join(dir, "controller.json"), Identity: "lab-controller", NodeID: 100, ListenIP: "127.0.0.1", Epoch: func() (string, error) { return "fixture-core", nil }, BeforeRemove: func(ctx context.Context) error { _, err := client.Counters(ctx, 100); return err }}
	if err := controller.Load(); err != nil {
		t.Fatal(err)
	}
	managedRaw := []byte(fmt.Sprintf(`{"server_port":%d,"network":"tcp","tls":0}`, freePort(t)))
	if err := controller.Apply(ctx, managedRaw, []panel.User{u, uB}); err != nil {
		t.Fatal("controller initial apply", err)
	}
	rotated := panel.User{ID: 42, UUID: "15a4235c-39ac-42ea-a1b1-100000000044"}
	if err := controller.Apply(ctx, managedRaw, []panel.User{rotated}); err != nil {
		t.Fatal("UUID rotation/revocation", err)
	}
	actualUsers, err := client.Users(ctx, "zic-node-100")
	if err != nil || len(actualUsers) != 1 || actualUsers[0].UUID != rotated.UUID {
		t.Fatal("old UUID/user survived controller reconciliation", err)
	}
	managedRaw = []byte(fmt.Sprintf(`{"server_port":%d,"network":"tcp","tls":0}`, freePort(t)))
	if err := controller.Apply(ctx, managedRaw, []panel.User{rotated}); err != nil {
		t.Fatal("controller transport replacement", err)
	}
	if err := controller.Disable(ctx); err != nil {
		t.Fatal("controller disable", err)
	}
	// Full daemon -> HTTPS source-shaped mock -> real core, including ETags,
	// UUID rotation, removed users, transport change and graceful shutdown.
	var panelVersion atomic.Int32
	panelVersion.Store(1)
	portA, portB := freePort(t), freePort(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Error("daemon attempted traffic sending")
			w.WriteHeader(500)
			return
		}
		if r.URL.Query().Get("node_id") != "101" || r.URL.Query().Get("token") != "dummy-token" || r.URL.Query().Get("node_type") != "vless" {
			t.Error("incorrect daemon panel credentials")
		}
		version := panelVersion.Load()
		etag := fmt.Sprintf(`"%s-%d"`, r.URL.Path, version)
		if r.Header.Get("If-None-Match") == etag {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", etag)
		switch r.URL.Path {
		case "/api/v1/server/UniProxy/config":
			port := portA
			if version == 2 {
				port = portB
			}
			fmt.Fprintf(w, `{"server_port":%d,"network":"tcp","tls":0,"base_config":{"pull_interval":1,"push_interval":1}}`, port)
		case "/api/v1/server/UniProxy/user":
			if version == 1 {
				fmt.Fprintf(w, `{"users":[{"id":42,"uuid":%q},{"id":43,"uuid":%q}]}`, uuid, uuidB)
			} else {
				fmt.Fprintf(w, `{"users":[{"id":42,"uuid":%q}]}`, rotated.UUID)
			}
		default:
			t.Error("unexpected daemon endpoint")
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	panelClient := panel.NewClient()
	panelClient.HTTP.Transport.(*http.Transport).TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	defer panelClient.HTTP.CloseIdleConnections()
	pidPath := filepath.Join(dir, "core.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(cmd.Process.Pid)), 0600); err != nil {
		t.Fatal(err)
	}
	runner := &daemon.Runner{Config: daemon.Config{Credentials: panel.Credentials{PanelURL: server.URL, Token: "dummy-token", NodeID: 101}, Mode: "manage-test", PanelCommit: panel.ProductionCommit, XrayEndpoint: net.JoinHostPort("127.0.0.1", strconv.Itoa(apiPort)), XrayPIDFile: pidPath, Pull: 1, Push: 1}, Panel: panelClient, Stats: client, ControlCore: client, Epoch: func() (string, error) { return fmt.Sprintf("fixture-core-%d", cmd.Process.Pid), nil }, Log: integrationEvents{}, StateDir: filepath.Join(dir, "daemon-state")}
	runContext, stop := context.WithCancel(ctx)
	defer stop()
	finished := make(chan error, 1)
	go func() { finished <- runner.Run(runContext) }()
	waitUsers := func(count int, expectedUUID string) {
		t.Helper()
		until := time.Now().Add(8 * time.Second)
		for {
			users, err := client.Users(ctx, "zic-node-101")
			if err == nil && len(users) == count && (expectedUUID == "" || users[0].UUID == expectedUUID) {
				return
			}
			if time.Now().After(until) {
				stop()
				select {
				case err := <-finished:
					t.Fatalf("daemon reconcile timeout: %v", err)
				case <-time.After(9 * time.Second):
					t.Fatal("daemon did not stop")
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	waitUsers(2, "")
	panelVersion.Store(2)
	waitUsers(1, rotated.UUID)
	stop()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal("daemon shutdown", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon shutdown timed out")
	}
	if _, exists, err := client.InspectInbound(ctx, "zic-node-101"); err != nil || exists {
		t.Fatal("daemon left owned test inbound on shutdown", err)
	}
}

func TestAPIRejectsNonlocalOrUnsafeTargets(t *testing.T) {
	for _, endpoint := range []string{"panel.example:443", "0.0.0.0:8080", "192.0.2.1:8080", "127.0.0.1:0"} {
		if c, err := Connect(endpoint); err == nil {
			c.Close()
			t.Fatal("nonlocal API accepted")
		}
	}
}

func TestDesiredInboundFingerprintIsStableAcrossBuilds(t *testing.T) {
	c := &Client{}
	want, err := c.PlanFingerprint("zic-node-42", "127.0.0.1", panel.VLESSSpec{Port: 12345})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 200; i++ {
		got, err := c.PlanFingerprint("zic-node-42", "127.0.0.1", panel.VLESSSpec{Port: 12345})
		if err != nil || got != want {
			t.Fatal("identical desired config changed fingerprint and would recreate the inbound")
		}
	}
}
