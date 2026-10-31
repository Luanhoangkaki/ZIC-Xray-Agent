package panel

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInstallerExactPanelMappingAndHysteriaVersion(t *testing.T) {
	for _, tc := range []struct {
		choice, typ, raw string
		version          int
		allowed          bool
	}{
		{"1", "shadowsocks", `{"server_port":1443,"cipher":"aes-128-gcm"}`, 0, false},
		{"2", "trojan", `{"server_port":1443,"host":"test.invalid","network":"tcp"}`, 0, false},
		{"3", "hysteria", `{"server_port":1443,"version":1}`, 1, false},
		{"3", "hysteria", `{"server_port":1443,"version":2}`, 2, false},
		{"3", "hysteria", `{"server_port":1443,"version":"1"}`, 1, false},
		{"3", "hysteria", `{"server_port":1443,"version":"2"}`, 2, false},
		{"4", "vless", `{"server_port":1443,"network":"tcp","tls":0}`, 0, true},
	} {
		t.Run(tc.typ+fmt.Sprint(tc.version), func(t *testing.T) {
			requests := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != "GET" || r.URL.Path != "/api/v1/server/UniProxy/config" || r.URL.Query().Get("node_type") != tc.typ || r.URL.Query().Get("node_id") != "42" || r.URL.Query().Get("token") != "dummy-secret" {
					t.Error("wrong UniProxy contract")
				}
				fmt.Fprint(w, tc.raw)
			}))
			defer server.Close()
			client := trustedClient(server)
			defer client.HTTP.CloseIdleConnections()
			probe, err := client.ProbeInstallation(context.Background(), Credentials{PanelURL: server.URL, Token: "dummy-secret", NodeID: 42}, tc.choice)
			if err != nil || probe.NodeType != tc.typ || probe.HysteriaVersion != tc.version || requests != 1 {
				t.Fatal("probe", err)
			}
			if (probe.RequireAgentAdapter() == nil) != tc.allowed {
				t.Fatal("unsupported adapter enabled")
			}
			if tc.typ != "vless" {
				auth := Credentials{PanelURL: server.URL, Token: "dummy-secret", NodeID: 42, NodeType: tc.typ}
				if auth.Validate() == nil {
					t.Fatal("installer probe broadened daemon protocol support")
				}
			}
		})
	}
}

func TestInstallerRejectsAmbiguousHysteriaAndInvalidMenuWithoutGuessing(t *testing.T) {
	for _, raw := range []string{`{}`, `{"version":null}`, `{"version":"02"}`, `{"version":"hysteria2"}`, `{"version":0}`, `{"version":3}`, `{"version":1,"version":2}`, `[]`} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, raw) }))
		client := trustedClient(server)
		if _, err := client.ProbeInstallation(context.Background(), Credentials{PanelURL: server.URL, Token: "dummy-secret", NodeID: 42}, "3"); err == nil || strings.Contains(err.Error(), "dummy-secret") {
			t.Error("ambiguous version accepted or unsafe diagnostic")
		}
		client.HTTP.CloseIdleConnections()
		server.Close()
	}
	for _, choice := range []string{"", "0", "5", "04", " 4", "hysteria2", "4\n"} {
		if _, err := InstallationType(choice); err == nil {
			t.Fatal("invalid choice accepted")
		}
	}
}
