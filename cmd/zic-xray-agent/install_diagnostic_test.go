package main

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"zic-xray-agent/internal/daemon"
	"zic-xray-agent/internal/logging"
	"zic-xray-agent/internal/panel"
)

type fixtureTransport func(*http.Request) (*http.Response, error)

func (f fixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type brokenFixtureBody struct{}

func (brokenFixtureBody) Read([]byte) (int, error) { return 0, errors.New("private-api-canary") }
func (brokenFixtureBody) Close() error             { return nil }

func realityInstallFixture(t *testing.T) string {
	t.Helper()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal("fixture key generation failed")
	}
	settings := map[string]any{"server_name": "fixture.invalid", "dest": "127.0.0.1:443", "private_key": base64.RawURLEncoding.EncodeToString(key.Bytes()), "short_id": "01234567"}
	raw, err := json.Marshal(map[string]any{"server_port": 1443, "network": "tcp", "tls": 2, "flow": "xtls-rprx-vision", "tls_settings": settings})
	if err != nil {
		t.Fatal("fixture encoding failed")
	}
	return string(raw)
}

func TestInstallerFailureStagesWithSyntheticPanel(t *testing.T) {
	const plain = `{"server_port":1443,"network":"tcp","tls":0}`
	const emptyUsers = `{"users":[]}`
	const fixtureUser = `{"id":42,"uuid":"15a4235c-39ac-42ea-a1b1-100000000042"}`
	type scenario struct {
		name, choice, config, users, code string
		remote, userFault                 bool
		status                            int
		fault                             string
		input                             string
		staging                           bool
	}
	cases := []scenario{
		{name: "staging", staging: true, code: "E_INSTALL_STAGING_STORAGE"},
		{name: "stdin_missing", input: "incomplete", code: "E_INSTALL_CREDENTIALS_INPUT"},
		{name: "stdin_extra", input: "panel.invalid\x00private-api-canary\x0042\x00extra", code: "E_INSTALL_CREDENTIALS_INPUT"},
		{name: "url", input: "http://fixture.invalid\x00private-api-canary\x0042\x00", code: "E_INSTALL_CREDENTIALS_INPUT"},
		{name: "node", input: "fixture.invalid\x00private-api-canary\x00bad\x00", code: "E_INSTALL_CREDENTIALS_INPUT"},
		{name: "empty_key", input: "fixture.invalid\x00\x0042\x00", code: "E_INSTALL_CREDENTIALS_AUTH_INPUT"},
		{name: "shadowsocks", choice: "1", config: `{}`, code: "E_INSTALL_ADAPTER_UNSUPPORTED"},
		{name: "trojan", choice: "2", config: `{}`, code: "E_INSTALL_ADAPTER_UNSUPPORTED"},
		{name: "hysteria1", choice: "3", config: `{"version":1}`, code: "E_INSTALL_ADAPTER_UNSUPPORTED"},
		{name: "hysteria2", choice: "3", config: `{"version":2}`, code: "E_INSTALL_ADAPTER_UNSUPPORTED"},
		{name: "hysteria_missing_version", choice: "3", config: `{}`, code: "E_INSTALL_PROBE_PANEL_RESPONSE"},
		{name: "invalid_json", config: `{"private-api-canary":`, code: "E_INSTALL_PROBE_PANEL_RESPONSE"},
		{name: "duplicate_json", config: `{"tls":0,"tls":2}`, code: "E_INSTALL_PROBE_PANEL_RESPONSE"},
		{name: "not_object", config: `[]`, code: "E_INSTALL_PROBE_PANEL_RESPONSE"},
		{name: "port", config: `{"server_port":0,"network":"tcp","tls":0}`, code: "E_INSTALL_PROBE_PANEL_RESPONSE"},
		{name: "missing_tls", config: `{"server_port":443,"network":"tcp"}`, code: "E_INSTALL_PROBE_PANEL_RESPONSE"},
		{name: "interval", config: `{"server_port":443,"network":"tcp","tls":0,"base_config":{"pull_interval":0}}`, code: "E_INSTALL_PROBE_PANEL_RESPONSE"},
		{name: "ws", config: `{"server_port":443,"network":"ws","tls":0}`, code: "E_INSTALL_VLESS_VLESS_CONFIG"},
		{name: "grpc", config: `{"server_port":443,"network":"grpc","tls":0}`, code: "E_INSTALL_VLESS_VLESS_CONFIG"},
		{name: "routes", config: `{"server_port":443,"network":"tcp","tls":0,"routes":[1]}`, code: "E_INSTALL_VLESS_VLESS_CONFIG"},
		{name: "encryption", config: `{"server_port":443,"network":"tcp","tls":0,"encryption":"unknown"}`, code: "E_INSTALL_VLESS_VLESS_CONFIG"},
		{name: "vision_without_tls", config: `{"server_port":443,"network":"tcp","tls":0,"flow":"xtls-rprx-vision"}`, code: "E_INSTALL_VLESS_VLESS_CONFIG"},
		{name: "certificate_tls", config: `{"server_port":443,"network":"tcp","tls":1,"tls_settings":{"server_name":"fixture.invalid"}}`, code: "E_INSTALL_VLESS_TLS_CERTIFICATE"},
		{name: "missing_tls_settings", config: `{"server_port":443,"network":"tcp","tls":2}`, code: "E_INSTALL_PROBE_PANEL_RESPONSE"},
		{name: "bad_reality", config: `{"server_port":443,"network":"tcp","tls":2,"tls_settings":{"server_name":"fixture.invalid"}}`, code: "E_INSTALL_VLESS_REALITY_CONFIG"},
		{name: "remote_plain", remote: true, code: "E_INSTALL_TLS_TLS_REQUIRED"},
		{name: "invalid_users", users: `{"users":null}`, code: "E_INSTALL_USERS_PANEL_RESPONSE"},
		{name: "missing_users", users: `{}`, code: "E_INSTALL_USERS_PANEL_RESPONSE"},
		{name: "user_object", users: `{"users":{}}`, code: "E_INSTALL_USERS_PANEL_RESPONSE"},
		{name: "duplicate_users_key", users: `{"users":[],"users":[]}`, code: "E_INSTALL_USERS_PANEL_RESPONSE"},
		{name: "invalid_user", users: `{"users":[{"id":42,"uuid":"private-api-canary"}]}`, code: "E_INSTALL_USERS_PANEL_RESPONSE"},
		{name: "unsupported_user_field", users: `{"users":[{"id":42,"uuid":"15a4235c-39ac-42ea-a1b1-100000000042","hwid":"hwid-canary"}]}`, code: "E_INSTALL_USERS_PANEL_RESPONSE"},
		{name: "duplicate_user", users: `{"users":[` + fixtureUser + `,` + fixtureUser + `]}`, code: "E_INSTALL_USERS_PANEL_RESPONSE"},
		{name: "speed_policy", users: `{"users":[{"id":42,"uuid":"15a4235c-39ac-42ea-a1b1-100000000042","speed_limit":1}]}`, code: "E_INSTALL_POLICY_POLICY"},
		{name: "device_policy", users: `{"users":[{"id":42,"uuid":"15a4235c-39ac-42ea-a1b1-100000000042","device_limit":1}]}`, code: "E_INSTALL_POLICY_POLICY"},
		{name: "core_missing", code: "E_INSTALL_CORE_CORE_BINARY"},
	}
	for _, userFault := range []bool{false, true} {
		stage := "PROBE"
		if userFault {
			stage = "USERS"
		}
		for _, tc := range []struct {
			status       int
			body, reason string
		}{
			{401, `{"message":"private-api-canary"}`, "AUTH"},
			{403, `{"message":"private-api-canary"}`, "AUTH"},
			{500, `{"message":"token is error"}`, "AUTH"},
			{500, `{"message":"token is null"}`, "AUTH"},
			{500, `{"message":"server is not exist"}`, "NODE"},
			{500, `{"message":"private-api-canary"}`, "PANEL_STATUS"},
			{500, `{"message":"private-api-canary","message":"token is error"}`, "PANEL_STATUS"},
			{500, `{"message":"token is error"}` + strings.Repeat(" ", 4097), "PANEL_STATUS"},
			{404, `{}`, "PANEL_STATUS"}, {422, `{}`, "PANEL_STATUS"}, {429, `{}`, "PANEL_STATUS"}, {503, `{}`, "PANEL_STATUS"}, {304, `{}`, "PANEL_RESPONSE"},
		} {
			c := scenario{name: fmt.Sprintf("http_%s_%d_%d", stage, tc.status, len(cases)), status: tc.status, userFault: userFault, code: "E_INSTALL_" + stage + "_" + tc.reason}
			if userFault {
				c.users = tc.body
			} else {
				c.config = tc.body
			}
			cases = append(cases, c)
		}
		for _, fault := range []struct{ name, code string }{{"transport", "PANEL_TRANSPORT"}, {"read", "PANEL_READ"}, {"size", "PANEL_SIZE"}} {
			cases = append(cases, scenario{name: stage + "_" + fault.name, userFault: userFault, fault: fault.name, code: "E_INSTALL_" + stage + "_" + fault.code})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.staging && os.WriteFile(filepath.Join(dir, "occupied"), nil, 0600) != nil {
				t.Fatal("fixture setup failed")
			}
			calls, userCalls := 0, 0
			client := &panel.Client{HTTP: &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" {
					t.Fatal("unexpected mutation")
				}
				isUser := strings.HasSuffix(r.URL.Path, "/user")
				if isUser {
					userCalls++
				}
				body := tc.config
				if body == "" {
					body = plain
				}
				if isUser {
					body = tc.users
					if body == "" {
						body = emptyUsers
					}
				}
				status := http.StatusOK
				var reader io.ReadCloser = io.NopCloser(strings.NewReader(body))
				if isUser == tc.userFault {
					if tc.status != 0 {
						status = tc.status
					}
					switch tc.fault {
					case "transport":
						return nil, errors.New("https://fixture.invalid/?token=private-api-canary")
					case "read":
						reader = brokenFixtureBody{}
					case "size":
						reader = io.NopCloser(strings.NewReader(strings.Repeat("x", (2<<20)+1)))
					}
				}
				return &http.Response{StatusCode: status, Body: reader, Header: make(http.Header), Request: r}, nil
			})}}
			defer client.HTTP.CloseIdleConnections()
			url := "https://127.0.0.1"
			if tc.remote {
				url = "https://fixture.invalid"
			}
			input := tc.input
			if input == "" {
				input = url + "\x00private-api-canary\x0042\x00"
			}
			choice := tc.choice
			if choice == "" {
				choice = "4"
			}
			var output strings.Builder
			err := configureInstallation(strings.NewReader(input), &output, dir, choice, filepath.Join(dir, "missing"), client)
			if err == nil || logging.Diagnostic(err) != tc.code || err.Error() != tc.code || output.Len() != 0 {
				t.Fatal("incorrect or unsafe failure diagnostic; payload suppressed")
			}
			if strings.HasPrefix(tc.code, "E_INSTALL_PROBE_") && (calls != 1 || userCalls != 0) {
				t.Fatal("probe retried or fetched users after failure")
			}
			if strings.HasPrefix(tc.code, "E_INSTALL_USERS_") && (calls != 2 || userCalls != 1) {
				t.Fatal("users request retried after failure")
			}
			if (strings.HasPrefix(tc.code, "E_INSTALL_ADAPTER_") || strings.HasPrefix(tc.code, "E_INSTALL_VLESS_") || strings.HasPrefix(tc.code, "E_INSTALL_TLS_")) && (calls != 1 || userCalls != 0) {
				t.Fatal("unsupported preflight fetched users or retried")
			}
			entries, _ := os.ReadDir(dir)
			want := 0
			if tc.staging {
				want = 1
			}
			if len(entries) != want {
				t.Fatal("failed preflight wrote credentials or retained core artifacts")
			}
		})
	}
}

func TestRemoteRealityAndSaveFailureAfterVerifiedCore(t *testing.T) {
	core := os.Getenv("XRAY_BINARY")
	if core == "" {
		t.Skip("requires verified official Xray executable")
	}
	reality := realityInstallFixture(t)
	for _, failSave := range []bool{false, true} {
		dir := t.TempDir()
		client := &panel.Client{HTTP: &http.Client{Transport: fixtureTransport(func(r *http.Request) (*http.Response, error) {
			body := reality
			if strings.HasSuffix(r.URL.Path, "/user") {
				body = `{"users":[]}`
				if failSave && os.WriteFile(filepath.Join(dir, "save-conflict"), nil, 0600) != nil {
					t.Fatal("fixture setup failed")
				}
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header), Request: r}, nil
		})}}
		var output strings.Builder
		err := configureInstallation(strings.NewReader("https://fixture.invalid\x00private-api-canary\x0042\x00"), &output, dir, "4", core, client)
		client.HTTP.CloseIdleConnections()
		if failSave {
			if logging.Diagnostic(err) != "E_INSTALL_SAVE_STORAGE" || output.Len() != 0 {
				t.Fatal("save error misclassified")
			}
			if _, err := os.Lstat(filepath.Join(dir, "config.json")); !os.IsNotExist(err) {
				t.Fatal("save conflict committed credentials")
			}
			if entries, err := os.ReadDir(dir); err != nil || len(entries) != 1 || entries[0].Name() != "save-conflict" {
				t.Fatal("save failure retained unexpected files")
			}
		} else {
			if err != nil {
				t.Fatal("supported remote REALITY preflight rejected")
			}
			c, err := daemon.LoadConfig(filepath.Join(dir, "config.json"))
			if err != nil || c.Mode != "observe" || c.TrafficSendEnabled || c.XrayEndpoint != "" {
				t.Fatal("unsafe installation mode")
			}
		}
	}
}

func TestInstallerDiagnosticProcessHelper(t *testing.T) {
	if os.Getenv("ZIC_INSTALL_DIAGNOSTIC_HELPER") != "1" {
		return
	}
	logging.SuppressUnstructured()
	defer logging.RecoverMain()
	client := &panel.Client{HTTP: &http.Client{Transport: fixtureTransport(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("https://198.51.100.73/?token=private-api-canary&uuid=private-key-canary&hwid=hwid-canary")
	})}}
	err := configureInstallation(strings.NewReader("https://fixture.invalid\x00private-api-canary\x0042\x00"), os.Stdout, os.Getenv("ZIC_INSTALL_DIAGNOSTIC_DIR"), "4", "", client)
	fmt.Fprintln(os.Stderr, logging.Diagnostic(err))
	os.Exit(1)
}

func TestInstallerDiagnosticProcessOnlyPrintsFixedCode(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestInstallerDiagnosticProcessHelper$")
	command.Env = append(os.Environ(), "ZIC_INSTALL_DIAGNOSTIC_HELPER=1", "ZIC_INSTALL_DIAGNOSTIC_DIR="+t.TempDir())
	raw, err := command.CombinedOutput()
	if err == nil || strings.TrimSpace(string(raw)) != "E_INSTALL_PROBE_PANEL_TRANSPORT" {
		t.Fatal("unsafe process diagnostic; payload suppressed")
	}
	if logging.Diagnostic(configureInstallCommand([]string{"--token", "private-api-canary"}, strings.NewReader(""), io.Discard)) != "E_INSTALL_ARGS_INPUT" {
		t.Fatal("unsafe argument diagnostic")
	}
}
