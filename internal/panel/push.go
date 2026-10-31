package panel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"zic-xray-agent/internal/strictjson"
)

func IsLoopbackPanel(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && ip.IsLoopback()
}

// PushTrafficLab is deliberately unavailable for real panels. It implements the
// verified wire protocol against a loopback HTTPS lab/mock only. The runtime
// daemon never calls it. There are no retries, redirects or idempotency headers.
func (c *Client) PushTrafficLab(ctx context.Context, auth Credentials, payload map[int64][2]uint64) error {
	if err := auth.Validate(); err != nil {
		return err
	}
	if !IsLoopbackPanel(auth.PanelURL) {
		return errors.New("traffic POST is restricted to an explicit loopback HTTPS lab")
	}
	if len(payload) == 0 || len(payload) > MaxUsers {
		return errors.New("invalid traffic batch size")
	}
	for uid, v := range payload {
		if uid <= 0 || v == [2]uint64{} || v[0] > math.MaxInt64 || v[1] > math.MaxInt64 {
			return errors.New("invalid traffic payload")
		}
	}
	body, err := json.Marshal(payload)
	if err != nil || len(body) > 1<<20 {
		return errors.New("traffic payload exceeds limit")
	}
	u, _ := url.Parse(auth.PanelURL + "/api/v1/server/UniProxy/push")
	q := u.Query()
	q.Set("token", auth.Token)
	q.Set("node_id", strconv.FormatInt(auth.NodeID, 10))
	q.Set("node_type", auth.NodeType)
	u.RawQuery = q.Encode()
	// Explicit ReadCloser leaves GetBody nil: no body rewind/replay capability.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), io.NopCloser(bytes.NewReader(body)))
	if err != nil {
		return errors.New("cannot create traffic request")
	}
	req.ContentLength = int64(len(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "ZIC-Xray-Agent/0.3-lab")
	client := *c.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }
	response, err := client.Do(req)
	if err != nil {
		return errors.New("traffic outcome unknown: transport/timeout/redirect failure")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("traffic outcome unknown: panel did not return HTTP 200")
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(raw) > 4096 || strictjson.Validate(raw) != nil {
		return errors.New("traffic outcome unknown: invalid response")
	}
	var result struct {
		Data bool `json:"data"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&result) != nil || !result.Data {
		return errors.New("traffic outcome unknown: no recognized acceptance response")
	}
	return nil // ACCEPTED by HTTP contract only, not proof of job/billing commit.
}
