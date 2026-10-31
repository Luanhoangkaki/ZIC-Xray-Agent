package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"zic-xray-agent/internal/panel"
)

func TestCredentialInput(t *testing.T) {
	auth, err := readCredentials(strings.NewReader("panel.example\x00private token\x0042\x00"))
	if err != nil || auth.PanelURL != "https://panel.example" || auth.NodeID != 42 || auth.NodeType != "vless" {
		t.Fatal("input normalization failed", err)
	}
	for _, input := range []string{"", "panel\x00token\x00", "panel\x00token\x000\x00", "panel\x00token\x0042\x00extra", "panel\x00\x0042\x00", strings.Repeat("x", 9000)} {
		if _, err := readCredentials(strings.NewReader(input)); err == nil {
			t.Error("invalid input accepted")
		}
	}
}

func TestSaveNewNeverReplacesCredentials(t *testing.T) {
	dir := t.TempDir()
	auth := panel.Credentials{PanelURL: "https://panel.example", Token: "private-token", NodeID: 42, NodeType: "vless"}
	result := panel.Result{Raw: json.RawMessage(`{"protocol":"vless"}`), PullInterval: 60, PushInterval: 60}
	if err := saveNew(dir, auth, result); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil || !json.Valid(before) {
		t.Fatal("missing config", err)
	}
	auth.Token = "changed-token"
	if saveNew(dir, auth, result) == nil {
		t.Fatal("existing credentials replaced")
	}
	after, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil || string(before) != string(after) {
		t.Fatal("credentials changed")
	}
	if runtime.GOOS != "windows" {
		for _, name := range []string{"config.json", "panel-config.json"} {
			info, err := os.Stat(filepath.Join(dir, name))
			if err != nil || info.Mode().Perm() != 0600 {
				t.Fatal("unsafe file permissions")
			}
		}
		info, err := os.Stat(dir)
		if err != nil || info.Mode().Perm() != 0700 {
			t.Fatal("unsafe directory permissions")
		}
	}
}

func TestInvalidInputDoesNotWriteConfig(t *testing.T) {
	dir := t.TempDir()
	err := run([]string{"configure", "--output-dir", dir}, strings.NewReader("http://panel.example\x00secret\x0042\x00"), io.Discard)
	if err == nil {
		t.Fatal("invalid URL accepted")
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil || len(entries) != 0 {
		t.Fatal("invalid configuration was saved")
	}
}

func TestTokenFlagsAreRejectedBeforeReadingAnyRealConfig(t *testing.T) {
	for _, args := range [][]string{{"run", "--token", "secret"}, {"configure", "--token", "secret"}, {"configure-install", "--selection", "4", "--token", "secret"}, {"install-status", "--token", "secret"}} {
		err := run(args, strings.NewReader(""), io.Discard)
		if err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("unsupported mode or unsafe diagnostic")
		}
	}
}

func TestPanelPreflightCommitsOnlySuccessfulResponse(t *testing.T) {
	const secret = "secret-must-not-be-printed"
	for _, status := range []int{200, 403, 422, 500} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				if strings.HasSuffix(r.URL.Path, "/user") {
					io.WriteString(w, `{"users":[]}`)
					return
				}
				io.WriteString(w, `{"protocol":"vless","server_port":443,"network":"tcp","tls":0}`)
			}))
			defer server.Close()
			client := panel.NewClient()
			client.HTTP.Transport.(*http.Transport).TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
			dir := t.TempDir()
			var output strings.Builder
			err := configure(strings.NewReader(server.URL+"\x00"+secret+"\x0042\x00"), &output, dir, client)
			entries, readErr := os.ReadDir(dir)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if status == 200 {
				if err != nil || len(entries) != 2 {
					t.Fatal("successful preflight not committed", err)
				}
				config, readErr := os.ReadFile(filepath.Join(dir, "config.json"))
				if readErr != nil || !strings.Contains(string(config), secret) {
					t.Fatal("credential not persisted privately")
				}
			} else if err == nil || len(entries) != 0 {
				t.Fatal("failed API preflight wrote configuration")
			}
			if strings.Contains(output.String(), secret) || (err != nil && strings.Contains(err.Error(), secret)) {
				t.Fatal("secret in diagnostics")
			}
		})
	}
}
