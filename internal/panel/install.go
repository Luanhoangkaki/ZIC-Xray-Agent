package panel

import (
	"context"
	"encoding/json"
	"errors"

	"zic-xray-agent/internal/strictjson"
)

// InstallationType maps ONLY the four numbered installer choices. This does
// not broaden daemon protocol support or treat hysteria2 as hysteria v1.
func InstallationType(choice string) (string, error) {
	switch choice {
	case "1":
		return "shadowsocks", nil
	case "2":
		return "trojan", nil
	case "3":
		return "hysteria", nil
	case "4":
		return "vless", nil
	default:
		return "", errors.New("lựa chọn phải là một số từ 1 đến 4")
	}
}

type InstallationProbe struct {
	NodeType        string
	HysteriaVersion int
	Config          Result
}

// ProbeInstallation reads ONLY config to validate authentication/node existence
// for the selected API family. Unsupported adapters must not fetch customers,
// commit credentials or start a service. Runtime Credentials.Validate remains
// VLESS-only. No v2node endpoint or alias guessing is involved.
func (c *Client) ProbeInstallation(ctx context.Context, auth Credentials, choice string) (InstallationProbe, error) {
	typ, err := InstallationType(choice)
	if err != nil {
		return InstallationProbe{}, err
	}
	auth.NodeType = "vless"
	if err := auth.Validate(); err != nil {
		return InstallationProbe{}, err
	}
	auth.NodeType = typ
	config, err := c.request(ctx, auth, "config", "", MaxConfigBytes)
	if err != nil {
		return InstallationProbe{}, err
	}
	if err := strictjson.Validate(config.Raw); err != nil {
		return InstallationProbe{}, errors.New("JSON cấu hình node không hợp lệ")
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(config.Raw, &object) != nil || object == nil {
		return InstallationProbe{}, errors.New("cấu hình node phải là JSON object")
	}
	probe := InstallationProbe{NodeType: typ, Config: config}
	if typ == "hysteria" {
		var version int
		if json.Unmarshal(object["version"], &version) != nil {
			// The pinned Eloquent model does not cast version. Accept only the
			// exact numeric strings a PHP/database stack may serialize.
			var text string
			if json.Unmarshal(object["version"], &text) == nil {
				switch text {
				case "1":
					version = 1
				case "2":
					version = 2
				}
			}
		}
		if version != 1 && version != 2 {
			return InstallationProbe{}, errors.New("panel phải trả version Hysteria rõ ràng là 1 hoặc 2; không tự đoán")
		}
		probe.HysteriaVersion = version
	}
	if typ == "vless" {
		pull, push, err := ValidateNodeConfig(config.Raw)
		if err != nil {
			return InstallationProbe{}, err
		}
		probe.Config.PullInterval, probe.Config.PushInterval = pull, push
	}
	return probe, nil
}

func (p InstallationProbe) RequireAgentAdapter() error {
	switch p.NodeType {
	case "vless":
		return nil
	case "shadowsocks":
		return errors.New("Shadowsocks: agent chưa có adapter được kiểm chứng; không cài hoặc khởi động")
	case "trojan":
		return errors.New("Trojan: agent chưa có adapter được kiểm chứng; không cài hoặc khởi động")
	case "hysteria":
		if p.HysteriaVersion == 1 {
			return errors.New("Hysteria version 1: Xray được kiểm chứng chỉ nhận version 2; không chuyển đổi sang Hysteria2")
		}
		if p.HysteriaVersion == 2 {
			return errors.New("Hysteria2 (panel version 2): agent chưa có adapter được kiểm chứng; không cài hoặc khởi động")
		}
	}
	return errors.New("giao thức chưa được xác nhận hỗ trợ")
}
