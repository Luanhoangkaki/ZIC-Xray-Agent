// Package panel implements the verified V2B-ZIC UniProxy V1 read API.
package panel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"zic-xray-agent/internal/buildinfo"
	"zic-xray-agent/internal/logging"
	"zic-xray-agent/internal/strictjson"
)

const MaxConfigBytes = 1 << 20

type Credentials struct {
	PanelURL string `json:"panel_url"`
	Token    string `json:"server_token"`
	NodeID   int64  `json:"node_id"`
	NodeType string `json:"node_type"`
}

func NormalizeURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return "", logging.Failure(logging.Input, nil)
	}
	if strings.ContainsAny(u.Host, "\\ \t\r\n") {
		return "", logging.Failure(logging.Input, nil)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	return strings.TrimRight(u.String(), "/"), nil
}

func (c *Credentials) Validate() error {
	base, err := NormalizeURL(c.PanelURL)
	if err != nil {
		return err
	}
	c.PanelURL = base
	if c.NodeID <= 0 {
		return logging.Failure(logging.Input, nil)
	}
	if c.NodeType == "" {
		c.NodeType = "vless"
	}
	if c.NodeType != "vless" {
		return errors.New("agent hiện chỉ hỗ trợ Node Type VLESS")
	}
	if len(c.Token) == 0 || len(c.Token) > 4096 || strings.ContainsAny(c.Token, "\x00\r\n") {
		return logging.Failure(logging.AuthInput, nil)
	}
	return nil
}

type Client struct{ HTTP *http.Client }

func NewClient() *Client {
	return &Client{HTTP: &http.Client{
		Timeout:       10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") },
		Transport: &http.Transport{
			DialContext:  (&net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
			MaxIdleConns: 2, MaxIdleConnsPerHost: 2, MaxConnsPerHost: 2,
			IdleConnTimeout: 90 * time.Second, TLSHandshakeTimeout: 5 * time.Second,
			ResponseHeaderTimeout: 8 * time.Second, MaxResponseHeaderBytes: 32 << 10,
			DisableCompression: true,
		},
	}}
}

type Result struct {
	Raw          json.RawMessage
	ETag         string
	NotModified  bool
	PullInterval int
	PushInterval int
}

// FetchConfig never includes raw network errors or panel response bodies in errors:
// net/http errors can contain the authenticated URL; panel errors can echo secrets.
// UniProxy V1's Laravel middleware reads token from request input, not Bearer auth.
func (c *Client) FetchConfig(ctx context.Context, auth Credentials, etag string) (Result, error) {
	result, err := c.fetch(ctx, auth, "config", etag, MaxConfigBytes)
	if err != nil || result.NotModified {
		return result, err
	}
	pull, push, err := ValidateNodeConfig(result.Raw)
	if err != nil {
		return Result{}, err
	}
	result.PullInterval, result.PushInterval = pull, push
	return result, nil
}

func (c *Client) fetch(ctx context.Context, auth Credentials, resource, etag string, limit int) (Result, error) {
	if err := auth.Validate(); err != nil {
		return Result{}, err
	}
	return c.request(ctx, auth, resource, etag, limit)
}

// request is private: callers must validate credentials and the exact API type.
func (c *Client) request(ctx context.Context, auth Credentials, resource, etag string, limit int) (Result, error) {
	u, _ := url.Parse(auth.PanelURL + "/api/v1/server/UniProxy/" + resource)
	q := u.Query()
	q.Set("token", auth.Token)
	q.Set("node_id", strconv.FormatInt(auth.NodeID, 10))
	q.Set("node_type", auth.NodeType)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Result{}, logging.Failure(logging.Input, nil)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", buildinfo.UserAgent)
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return Result{}, logging.Failure(logging.PanelTransport, nil)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified {
		if etag == "" {
			return Result{}, logging.Failure(logging.PanelResponse, nil)
		}
		return Result{NotModified: true, ETag: etag}, nil
	}
	if resp.StatusCode != http.StatusOK {
		var reason string
		code := logging.PanelStatus
		switch resp.StatusCode {
		case 401, 403:
			code = logging.Auth
			reason = "Server Token bị từ chối hoặc không có quyền truy cập node"
		case 404:
			reason = "không tìm thấy API UniProxy V1 hoặc Node ID"
		case 400, 422:
			reason = "panel từ chối Token, Node ID hoặc Node Type; hãy kiểm tra cả ba trường"
		case 429:
			reason = "panel đang giới hạn tần suất yêu cầu"
		case 500:
			// The production PHP uses HTTP 500 for auth/node failures. Only
			// recognize exact source-backed messages; never echo an arbitrary body.
			body, readErr := io.ReadAll(io.LimitReader(resp.Body, 4097))
			var failure struct {
				Message string `json:"message"`
			}
			if readErr == nil && len(body) <= 4096 && strictjson.Validate(body) == nil {
				_ = json.Unmarshal(body, &failure)
			}
			switch failure.Message {
			case "token is null", "token is error":
				code = logging.Auth
				reason = "Server Token bị từ chối bởi V2B-ZIC"
			case "server is not exist":
				code = logging.Node
				reason = "Node ID không tồn tại cho Node Type đã chọn"
			default:
				reason = "panel lỗi nội bộ; không thể xác nhận token hoặc node"
			}
		default:
			reason = "panel không trả cấu hình hợp lệ"
		}
		return Result{}, logging.Failure(code, fmt.Errorf("%s (HTTP %d)", reason, resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return Result{}, logging.Failure(logging.PanelRead, nil)
	}
	if len(body) > limit {
		return Result{}, logging.Failure(logging.PanelSize, nil)
	}
	return Result{Raw: body, ETag: resp.Header.Get("ETag")}, nil
}

// ValidateNodeConfig validates the installation schema, not Xray applicability.
// The future Xray renderer must separately reject unsupported TLS/policy details.
func ValidateNodeConfig(body []byte) (int, int, error) {
	if err := strictjson.Validate(body); err != nil {
		return 0, 0, err
	}
	var node struct {
		Protocol    string          `json:"protocol"`
		Port        int             `json:"server_port"`
		Network     string          `json:"network"`
		TLS         *int            `json:"tls"`
		TLSSettings json.RawMessage `json:"tls_settings"`
		Base        struct {
			Pull *int `json:"pull_interval"`
			Push *int `json:"push_interval"`
		} `json:"base_config"`
	}
	if json.Unmarshal(body, &node) != nil {
		return 0, 0, errors.New("API không trả JSON cấu hình hợp lệ")
	}
	// V2B-ZIC f7784009 does not return protocol. Authentication used
	// node_type=vless and getServer selects ServerVless. Reject a conflicting
	// field if a future panel adds it, but do not invent a mandatory field.
	if node.Protocol != "" && node.Protocol != "vless" {
		return 0, 0, errors.New("Node ID không trả cấu hình VLESS; không được phép khởi động")
	}
	if node.Port < 1 || node.Port > 65535 {
		return 0, 0, errors.New("server_port từ panel không hợp lệ")
	}
	switch node.Network {
	case "tcp", "raw", "ws", "grpc", "httpupgrade", "xhttp", "splithttp", "http", "h2":
	default:
		return 0, 0, errors.New("network từ panel thiếu hoặc chưa được nhận diện; không được phép khởi động")
	}
	if node.TLS == nil || *node.TLS < 0 || *node.TLS > 2 {
		return 0, 0, errors.New("tls từ panel thiếu hoặc không hợp lệ")
	}
	if *node.TLS != 0 {
		var settings map[string]json.RawMessage
		if json.Unmarshal(node.TLSSettings, &settings) != nil || len(settings) == 0 {
			return 0, 0, errors.New("thiếu tls_settings cho TLS/Reality")
		}
	}
	pull, push := 60, 60
	if node.Base.Pull != nil {
		pull = *node.Base.Pull
	}
	if node.Base.Push != nil {
		push = *node.Base.Push
	}
	if pull < 1 || push < 1 || pull > 86400 || push > 86400 {
		return 0, 0, errors.New("chu kỳ đồng bộ của panel phải trong 1..86400 giây")
	}
	return pull, push, nil
}
