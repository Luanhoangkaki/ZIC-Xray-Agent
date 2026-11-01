package panel

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const fixtureUUID = "15a4235c-39ac-42ea-a1b1-100000000042"

// Fixtures below are derived from the checked-in PHP shape at f7784009,
// not captured responses from a deployed panel.
func TestProductionConfigHasNoProtocol(t *testing.T) {
	input := `{"server_port":443,"network":"tcp","networkSettings":null,"tls":0,"flow":null,"tls_settings":null,"encryption":null,"encryption_settings":null,"base_config":{"pull_interval":60,"push_interval":60}}`
	if _, _, err := ValidateNodeConfig([]byte(input)); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseVLESS([]byte(input)); err != nil {
		t.Fatal(err)
	}
}

func TestProductionErrorMessagesAreRecognizedButNotEchoed(t *testing.T) {
	for index, test := range []struct{ message, expected string }{{"token is error", "Server Token"}, {"token is null", "Server Token"}, {"server is not exist", "Node ID"}, {"private-token leaked in server exception", "panel lỗi nội bộ"}} {
		t.Run(fmt.Sprintf("error_case_%d", index), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(500)
				fmt.Fprintf(w, `{"message":%q}`, test.message)
			}))
			defer server.Close()
			client := trustedClient(server)
			defer client.HTTP.CloseIdleConnections()
			_, err := client.FetchConfig(context.Background(), Credentials{PanelURL: server.URL, Token: "private-token", NodeID: 42}, "")
			if err == nil || !strings.Contains(err.Error(), test.expected) || strings.Contains(err.Error(), "private-token") {
				t.Fatal("wrong/unsafe production error; raw error suppressed")
			}
		})
	}
}

func TestUsersAndConditionalFetch(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/server/UniProxy/user" || r.Header.Get("X-Response-Format") != "" {
			t.Error("wrong JSON user API request")
		}
		if r.Header.Get("If-None-Match") == `"users-1"` {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", `"users-1"`)
		fmt.Fprintf(w, `{"users":[{"id":42,"uuid":%q,"speed_limit":0,"device_limit":0}]}`, fixtureUUID)
	}))
	defer server.Close()
	c := trustedClient(server)
	defer c.HTTP.CloseIdleConnections()
	auth := Credentials{PanelURL: server.URL, Token: "secret", NodeID: 456}
	r, err := c.FetchUsers(context.Background(), auth, "")
	if err != nil || len(r.Users) != 1 || r.Users[0].UUID != fixtureUUID {
		t.Fatal("user parse failed", err)
	}
	if id, ok := StatsUserID(r.Users[0].Email(456), 456); !ok || id != 42 {
		t.Fatal("wrong counter mapping")
	}
	if _, ok := StatsUserID(r.Users[0].Email(456), 457); ok {
		t.Fatal("cross-node identity accepted")
	}
	if _, err := c.FetchUsers(context.Background(), auth, r.ETag); err != nil {
		t.Fatal(err)
	}
}

func TestUsersRejectAmbiguousOrUnsupportedData(t *testing.T) {
	for _, input := range []string{`{}`, `{"users":null}`, `{"users":{}}`, `{"users":[{"id":42,"uuid":"bad"}]}`, `{"users":[{"id":42,"uuid":"` + fixtureUUID + `","unexpected_policy":1}]}`, `{"users":[{"id":42,"uuid":"` + fixtureUUID + `"},{"id":42,"uuid":"` + fixtureUUID + `"}]}`} {
		if _, err := ParseUsers([]byte(input)); err == nil {
			t.Error("ambiguous users accepted")
		}
	}
	var u []User
	json.Unmarshal([]byte(`[{"id":42,"uuid":"`+fixtureUUID+`","speed_limit":1}]`), &u)
	if RequireSupportedPolicies(u) == nil {
		t.Fatal("speed limit silently ignored")
	}
}

func TestRealityValidationAndNoSecurityFallback(t *testing.T) {
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	node := map[string]any{"server_port": 443, "network": "tcp", "tls": 2, "flow": "xtls-rprx-vision", "tls_settings": map[string]any{"server_name": "example.org", "private_key": base64.RawURLEncoding.EncodeToString(key.Bytes()), "public_key": base64.RawURLEncoding.EncodeToString(key.PublicKey().Bytes()), "short_id": "aabbccdd", "server_port": "443", "xver": "0"}}
	raw, _ := json.Marshal(node)
	spec, err := ParseVLESS(raw)
	if err != nil || spec.Reality.Destination != "example.org:443" || len(spec.Reality.ShortID) != 8 {
		t.Fatal("invalid REALITY translation", err)
	}
	for _, mutate := range []func(){func() { node["network"] = "ws" }, func() { node["routes"] = []any{map[string]any{"action": "block"}} }, func() { node["encryption"] = "mlkem768x25519plus" }, func() { node["tls"] = 1 }, func() { node["tls_settings"].(map[string]any)["reject_unknown_sni"] = "1" }} {
		var copyNode map[string]any
		json.Unmarshal(raw, &copyNode)
		node = copyNode
		mutate()
		bad, _ := json.Marshal(node)
		if _, err := ParseVLESS(bad); err == nil {
			t.Fatal("unsupported policy silently downgraded")
		}
	}
}
