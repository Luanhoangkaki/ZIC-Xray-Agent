package main

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"zic-xray-agent/internal/daemon"
	"zic-xray-agent/internal/durable"
	"zic-xray-agent/internal/installcheck"
	"zic-xray-agent/internal/panel"
)

func TestInteractivePreflightRejectsWithoutSavingOrPosting(t *testing.T) {
	const secret = "dummy-api-key-never-print"
	for _, tc := range []struct {
		choice, config, users string
		code                  int
	}{
		{"1", `{"cipher":"aes-128-gcm"}`, `{"users":[]}`, 200},
		{"2", `{"network":"tcp"}`, `{"users":[]}`, 200},
		{"3", `{"version":1}`, `{"users":[]}`, 200},
		{"3", `{"version":2}`, `{"users":[]}`, 200},
		{"4", `{"server_port":443,"network":"ws","tls":0}`, `{"users":[]}`, 200},
		{"4", `{"server_port":443,"network":"tcp","tls":0}`, `{"users":[{"id":42,"uuid":"15a4235c-39ac-42ea-a1b1-100000000042","speed_limit":1}]}`, 200},
		{"4", `{"message":"token is error"}`, `{"users":[]}`, 500},
		{"4", `{"server_port":443,"network":"tcp","tls":0}`, `{"users":[]}`, 200}, // invalid core
	} {
		t.Run(tc.choice+fmt.Sprint(tc.code)+tc.config, func(t *testing.T) {
			posts := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					posts++
				}
				w.WriteHeader(tc.code)
				if strings.HasSuffix(r.URL.Path, "/user") {
					io.WriteString(w, tc.users)
				} else {
					io.WriteString(w, tc.config)
				}
			}))
			defer server.Close()
			client := panel.NewClient()
			client.HTTP = server.Client()
			defer client.HTTP.CloseIdleConnections()
			dir := t.TempDir()
			var output strings.Builder
			err := configureInstallation(strings.NewReader(server.URL+"\x00"+secret+"\x0042\x00"), &output, dir, tc.choice, filepath.Join(dir, "not-a-core"), client)
			if err == nil || posts != 0 || strings.Contains(err.Error(), secret) || strings.Contains(output.String(), secret) {
				t.Fatal("unsafe preflight", err)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Fatal("rejected preflight committed files")
			}
		})
	}
}

func TestInstallerOfflineCoreValidationAndObserveCommit(t *testing.T) {
	binary := os.Getenv("XRAY_BINARY")
	if binary == "" {
		t.Skip("requires verified official Xray executable")
	}
	// Holding the selected port proves -test does not bind a second server.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Query().Get("node_type") != "vless" {
			t.Error("unexpected mutation or node type")
		}
		if strings.HasSuffix(r.URL.Path, "/user") {
			io.WriteString(w, `{"users":[{"id":42,"uuid":"15a4235c-39ac-42ea-a1b1-100000000042","speed_limit":0,"device_limit":0}]}`)
		} else {
			fmt.Fprintf(w, `{"server_port":%d,"network":"tcp","tls":0,"base_config":{"pull_interval":17,"push_interval":19}}`, port)
		}
	}))
	defer server.Close()
	client := panel.NewClient()
	client.HTTP = server.Client()
	defer client.HTTP.CloseIdleConnections()
	t.Setenv("XRAY_LOCATION_CONFDIR", t.TempDir())
	if err := os.WriteFile(filepath.Join(os.Getenv("XRAY_LOCATION_CONFDIR"), "poison.json"), []byte("NOT JSON"), 0600); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	var output strings.Builder
	if err := configureInstallation(strings.NewReader(server.URL+"\x00dummy-key\x0042\x00"), &output, dir, "4", binary, client); err != nil {
		t.Fatal(err)
	}
	c, err := daemon.LoadConfig(filepath.Join(dir, "config.json"))
	if err != nil || c.Mode != "observe" || c.NodeType != "vless" || c.TrafficSendEnabled || c.Pull != 17 || c.Push != 19 || c.XrayEndpoint != "" {
		t.Fatal("unsafe saved configuration", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 2 {
		t.Fatal("core validation temp files leaked")
	}
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	reality := panel.VLESSSpec{Port: uint32(port), Flow: "xtls-rprx-vision", TLS: 2, Reality: &panel.RealitySpec{Destination: "127.0.0.1:443", ServerName: "lab.invalid", PrivateKey: key.Bytes(), ShortID: []byte{1, 2, 3, 4, 0, 0, 0, 0}}}
	if err := installcheck.Validate(context.Background(), binary, t.TempDir(), reality, 42, []panel.User{{ID: 42, UUID: "15a4235c-39ac-42ea-a1b1-100000000042"}}); err != nil {
		t.Fatal("REALITY offline core validation", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if installcheck.Validate(ctx, binary, t.TempDir(), panel.VLESSSpec{Port: 1443}, 42, nil) == nil {
		t.Fatal("cancelled core check accepted")
	}
}

func TestInstallStatusRequiresFreshMatchingReadOnlySnapshot(t *testing.T) {
	dir := t.TempDir()
	auth := panel.Credentials{PanelURL: "https://127.0.0.1:1443", Token: "dummy-key", NodeID: 42, NodeType: "vless"}
	raw := json.RawMessage(`{"server_port":1443,"network":"tcp","tls":0}`)
	if err := saveNew(dir, auth, panel.Result{Raw: raw, PullInterval: 60, PushInterval: 60}); err != nil {
		t.Fatal(err)
	}
	c, err := daemon.LoadConfig(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := daemon.Snapshot{Version: 1, Identity: c.Identity(), Config: raw, Users: []panel.User{}, LastSuccess: time.Now().UTC(), Pull: 60, Push: 60, PolicySupported: true}
	path := filepath.Join(dir, "snapshot.json")
	for _, scenario := range []string{"valid", "stale", "future", "identity", "unsupported"} {
		s := snapshot
		switch scenario {
		case "stale":
			s.LastSuccess = time.Now().Add(-time.Hour)
		case "future":
			s.LastSuccess = time.Now().Add(time.Hour)
		case "identity":
			s.Identity = "different"
		case "unsupported":
			s.PolicySupported = false
		}
		if err := durable.Save(path, s); err != nil {
			t.Fatal(err)
		}
		before, _ := os.ReadFile(path)
		err := installationStatus([]string{"--config", filepath.Join(dir, "config.json"), "--state-dir", dir})
		if (err == nil) != (scenario == "valid") {
			t.Fatal("wrong status", scenario, err)
		}
		after, _ := os.ReadFile(path)
		if string(before) != string(after) {
			t.Fatal("status changed checkpoint")
		}
	}
}
