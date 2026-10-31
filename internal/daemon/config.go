package daemon

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"strconv"

	"zic-xray-agent/internal/durable"
	"zic-xray-agent/internal/panel"
	"zic-xray-agent/internal/strictjson"
)

type Config struct {
	panel.Credentials
	Pull               int    `json:"pull_interval_seconds"`
	Push               int    `json:"push_interval_seconds"`
	LogLevel           string `json:"log_level"`
	Mode               string `json:"mode,omitempty"`
	PanelCommit        string `json:"panel_commit,omitempty"`
	XrayEndpoint       string `json:"xray_api_endpoint,omitempty"`
	XrayPIDFile        string `json:"xray_pid_file,omitempty"`
	ListenIP           string `json:"listen_ip,omitempty"`
	MaxStaleSeconds    int    `json:"max_stale_seconds,omitempty"`
	TrafficSendEnabled bool   `json:"traffic_send_enabled,omitempty"`
}

func LoadConfig(path string) (Config, error) {
	var c Config
	raw, err := durable.ReadPrivate(path, 16<<10)
	if err != nil {
		return c, errors.New("cannot read private agent config")
	}
	if err := strictjson.Validate(raw); err != nil {
		return c, err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil || json.Valid(raw) == false {
		return c, errors.New("invalid agent config")
	}
	if err := c.Validate(); err != nil {
		return c, err
	}
	return c, nil
}

func (c *Config) Validate() error {
	if err := c.Credentials.Validate(); err != nil {
		return err
	}
	if c.Pull == 0 {
		c.Pull = 60
	}
	if c.Push == 0 {
		c.Push = 60
	}
	if c.Pull < 1 || c.Pull > 86400 || c.Push < 1 || c.Push > 86400 {
		return errors.New("invalid agent synchronization intervals")
	}
	if c.LogLevel == "" {
		c.LogLevel = "warn"
	}
	if c.LogLevel != "warn" && c.LogLevel != "error" {
		return errors.New("only warn/error logging is supported")
	}
	if c.Mode == "" {
		c.Mode = "observe"
	}
	if c.Mode != "observe" && c.Mode != "manage-test" {
		return errors.New("only observe or explicit loopback manage-test modes are available; production management is blocked")
	}
	if c.TrafficSendEnabled {
		return errors.New("runtime traffic sending is disabled; lab sender exists only for opt-in tests")
	}
	if c.ListenIP == "" {
		c.ListenIP = "127.0.0.1"
	}
	if c.MaxStaleSeconds == 0 {
		c.MaxStaleSeconds = 120
	}
	if c.MaxStaleSeconds < 10 || c.MaxStaleSeconds > 3600 {
		return errors.New("max_stale_seconds must be in 10..3600")
	}
	if c.PanelCommit != panel.ProductionCommit {
		return errors.New("panel contract has not been verified for this commit")
	}
	if (c.XrayEndpoint == "") != (c.XrayPIDFile == "") {
		return errors.New("Xray stats requires both API endpoint and a trusted PID file")
	}
	if c.Mode == "manage-test" {
		ip := net.ParseIP(c.ListenIP)
		if !panel.IsLoopbackPanel(c.PanelURL) || ip == nil || !ip.IsLoopback() || c.XrayEndpoint == "" {
			return errors.New("manage-test requires a loopback HTTPS test panel, loopback listen IP and explicit Xray API/PID file")
		}
	}
	return nil
}

func (c Config) Identity() string {
	hash := sha256.Sum256([]byte(c.PanelURL + "|vless|" + strconv.FormatInt(c.NodeID, 10)))
	return hex.EncodeToString(hash[:])
}
