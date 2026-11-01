package panel

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"zic-xray-agent/internal/buildinfo"
)

const validNode = `{"protocol":"vless","server_port":443,"network":"tcp","tls":0,"base_config":{"pull_interval":75,"push_interval":90}}`

func trustedClient(server *httptest.Server) *Client {
	c := NewClient()
	c.HTTP.Transport.(*http.Transport).TLSClientConfig = server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
	return c
}

func TestConfigAuthenticationAndETag(t *testing.T) {
	const secret = "private+/&=token"
	requests := 0
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("User-Agent") != buildinfo.UserAgent {
			t.Error("agent release identity missing from request")
		}
		if r.URL.Path != "/prefix/api/v1/server/UniProxy/config" || r.URL.Query().Get("token") != secret || r.URL.Query().Get("node_id") != "123" || r.URL.Query().Get("node_type") != "vless" {
			t.Error("incorrect UniProxy request")
		}
		if r.Header.Get("If-None-Match") == `"version-a"` {
			w.WriteHeader(304)
			return
		}
		w.Header().Set("ETag", `"version-a"`)
		fmt.Fprint(w, validNode)
	}))
	defer server.Close()
	c := trustedClient(server)
	defer c.HTTP.CloseIdleConnections()
	auth := Credentials{PanelURL: server.URL + "/prefix/", Token: secret, NodeID: 123}
	result, err := c.FetchConfig(context.Background(), auth, "")
	if err != nil {
		t.Fatal(err)
	}
	if result.PullInterval != 75 || result.PushInterval != 90 || result.ETag != `"version-a"` || result.NotModified {
		t.Fatal("panel settings were not preserved")
	}
	result, err = c.FetchConfig(context.Background(), auth, result.ETag)
	if err != nil || !result.NotModified || requests != 2 {
		t.Fatal("conditional request failed", err)
	}
}

func TestFailuresNeverReflectSecrets(t *testing.T) {
	const secret = "do-not-log-this-token"
	for _, status := range []int{200, 301, 400, 401, 403, 404, 422, 429, 500} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				fmt.Fprintf(w, `{"message":%q}`, secret)
			}))
			defer server.Close()
			c := trustedClient(server)
			defer c.HTTP.CloseIdleConnections()
			_, err := c.FetchConfig(context.Background(), Credentials{PanelURL: server.URL, Token: secret, NodeID: 99}, "")
			if err == nil || strings.Contains(err.Error(), secret) || strings.Contains(err.Error(), server.URL) {
				t.Fatal("missing or unsafe error; raw value suppressed")
			}
		})
	}
}

func TestRedirectDoesNotForwardToken(t *testing.T) {
	target := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("redirect target must never receive request") }))
	defer target.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 302) }))
	defer server.Close()
	c := trustedClient(server)
	defer c.HTTP.CloseIdleConnections()
	_, err := c.FetchConfig(context.Background(), Credentials{PanelURL: server.URL, Token: "secret", NodeID: 99}, "")
	if err == nil {
		t.Fatal("redirect accepted")
	}
}

func TestBoundsAndUnsolicited304(t *testing.T) {
	for _, status := range []int{200, 304} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(status)
				if status == 200 {
					fmt.Fprint(w, strings.Repeat(" ", MaxConfigBytes+1))
				}
			}))
			defer server.Close()
			c := trustedClient(server)
			defer c.HTTP.CloseIdleConnections()
			if _, err := c.FetchConfig(context.Background(), Credentials{PanelURL: server.URL, Token: "secret", NodeID: 99}, ""); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}

func TestValidateNodeConfig(t *testing.T) {
	for _, input := range []string{
		`null`, `{}`, `<html>login</html>`, validNode + `{}`,
		strings.Replace(validNode, `"vless"`, `"vmess"`, 1),
		strings.Replace(validNode, `443`, `0`, 1),
		strings.Replace(validNode, `"tcp"`, `"unknown"`, 1),
		strings.Replace(validNode, `"tls":0`, `"tls":2`, 1),
		strings.Replace(validNode, `"pull_interval":75`, `"pull_interval":0`, 1),
		strings.Replace(validNode, `"push_interval":90`, `"push_interval":1.5`, 1),
	} {
		if _, _, err := ValidateNodeConfig([]byte(input)); err == nil {
			t.Error("invalid config accepted")
		}
	}
	pull, push, err := ValidateNodeConfig([]byte(`{"protocol":"vless","server_port":8443,"network":"ws","tls":0}`))
	if err != nil || pull != 60 || push != 60 {
		t.Fatal("default intervals incorrect", err)
	}
}

func TestURLValidation(t *testing.T) {
	for _, input := range []string{"http://panel.example", "https://user:password@panel.example", "https://panel.example?token=secret", "https://panel.example/#secret", "https://", "://"} {
		if _, err := NormalizeURL(input); err == nil {
			t.Error("unsafe URL accepted")
		}
	}
	actual, err := NormalizeURL(" panel.example/base/ ")
	if err != nil || actual != "https://panel.example/base" {
		t.Fatal("normalization failed", err)
	}
}
