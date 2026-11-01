package panel

import (
	"bytes"
	"crypto/ecdh"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"strconv"
	"strings"

	"zic-xray-agent/internal/logging"
)

// VLESSSpec is intentionally narrow: unsupported routes, transports, encryption
// and user limits must never be silently translated into weaker policy.
type VLESSSpec struct {
	Port    uint32
	Flow    string
	TLS     int
	Reality *RealitySpec
}
type RealitySpec struct {
	Destination string
	ServerName  string
	PrivateKey  []byte
	ShortID     []byte
	Xver        uint64
}

func emptySetting(raw json.RawMessage) bool {
	s := string(bytes.TrimSpace(raw))
	return s == "" || s == "null" || s == "{}" || s == "[]"
}

func ParseVLESS(raw []byte) (VLESSSpec, error) {
	if _, _, err := ValidateNodeConfig(raw); err != nil {
		return VLESSSpec{}, err
	}
	var fields map[string]json.RawMessage
	_ = json.Unmarshal(raw, &fields)
	for key := range fields {
		switch key {
		case "protocol", "server_port", "network", "networkSettings", "tls", "flow", "tls_settings", "encryption", "encryption_settings", "base_config", "routes":
		default:
			return VLESSSpec{}, errors.New("unknown VLESS setting requires contract review")
		}
	}
	var node struct {
		Port               uint32          `json:"server_port"`
		Flow               string          `json:"flow"`
		Network            string          `json:"network"`
		Settings           json.RawMessage `json:"networkSettings"`
		TLS                int             `json:"tls"`
		TLSSettings        json.RawMessage `json:"tls_settings"`
		Encryption         string          `json:"encryption"`
		EncryptionSettings json.RawMessage `json:"encryption_settings"`
		Routes             json.RawMessage `json:"routes"`
	}
	if json.Unmarshal(raw, &node) != nil {
		return VLESSSpec{}, errors.New("invalid VLESS settings")
	}
	if node.Network != "tcp" && node.Network != "raw" {
		return VLESSSpec{}, errors.New("only TCP VLESS is implemented; transport will not be downgraded")
	}
	if !emptySetting(node.Settings) || !emptySetting(node.Routes) {
		return VLESSSpec{}, errors.New("custom network settings/routes need an explicit implementation")
	}
	if (node.Encryption != "" && node.Encryption != "none") || !emptySetting(node.EncryptionSettings) {
		return VLESSSpec{}, errors.New("VLESS encryption settings are not supported; refusing fallback")
	}
	if node.Flow != "" && node.Flow != "xtls-rprx-vision" {
		return VLESSSpec{}, errors.New("unsupported VLESS flow")
	}
	spec := VLESSSpec{Port: node.Port, Flow: node.Flow, TLS: node.TLS}
	if node.TLS == 0 {
		if node.Flow != "" {
			return VLESSSpec{}, errors.New("Vision requires an implemented TLS transport")
		}
		return spec, nil
	}
	if node.TLS != 2 {
		return VLESSSpec{}, logging.Failure(logging.TLSCertificate, nil)
	}
	reality, err := parseReality(node.TLSSettings)
	if err != nil {
		return VLESSSpec{}, logging.Failure(logging.RealityConfig, nil)
	}
	spec.Reality = &reality
	return spec, nil
}

func parseReality(raw json.RawMessage) (RealitySpec, error) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return RealitySpec{}, errors.New("invalid REALITY settings")
	}
	for key := range fields {
		switch key {
		case "server_name", "dest", "server_port", "private_key", "public_key", "short_id", "xver", "fingerprint", "cert_mode", "provider", "dns_env", "cert_file", "key_file", "pinned_peer_cert_sha256", "allow_insecure", "reject_unknown_sni":
		default:
			return RealitySpec{}, errors.New("unknown REALITY setting requires implementation")
		}
	}
	for _, name := range []string{"allow_insecure", "reject_unknown_sni"} {
		if raw, exists := fields[name]; exists {
			switch string(bytes.TrimSpace(raw)) {
			case "null", "false", "0", `"0"`, `"false"`, `""`:
			default:
				return RealitySpec{}, errors.New("REALITY TLS policy flag is not implemented; refusing to ignore it")
			}
		}
	}
	var settings struct {
		ServerName  string          `json:"server_name"`
		Destination string          `json:"dest"`
		Port        json.RawMessage `json:"server_port"`
		PrivateKey  string          `json:"private_key"`
		PublicKey   string          `json:"public_key"`
		ShortID     string          `json:"short_id"`
		Xver        json.RawMessage `json:"xver"`
	}
	if json.Unmarshal(raw, &settings) != nil {
		return RealitySpec{}, errors.New("invalid REALITY settings")
	}
	name := strings.TrimSpace(settings.ServerName)
	if name == "" || strings.ContainsAny(name, " ,/\\\t\r\n:@") || strings.Contains(name, "*") {
		return RealitySpec{}, errors.New("REALITY requires one explicit server_name")
	}
	private, err := base64.RawURLEncoding.DecodeString(settings.PrivateKey)
	if err != nil || len(private) != 32 {
		return RealitySpec{}, errors.New("REALITY private key must be a 32-byte URL-safe base64 key")
	}
	key, err := ecdh.X25519().NewPrivateKey(private)
	if err != nil {
		return RealitySpec{}, errors.New("invalid REALITY private key")
	}
	if settings.PublicKey != "" {
		public, err := base64.RawURLEncoding.DecodeString(settings.PublicKey)
		if err != nil || !bytes.Equal(public, key.PublicKey().Bytes()) {
			return RealitySpec{}, errors.New("REALITY public/private keys do not match")
		}
	}
	short, err := hex.DecodeString(settings.ShortID)
	if err != nil || len(short) == 0 || len(short) > 8 {
		return RealitySpec{}, errors.New("REALITY short_id must contain 2..16 hexadecimal digits")
	}
	short = append(short, make([]byte, 8-len(short))...)
	port := int64(443) // The panel admin controller defaults this field to "443".
	if !emptySetting(settings.Port) {
		port, err = integerSetting(settings.Port)
		if err != nil || port < 1 || port > 65535 {
			return RealitySpec{}, errors.New("invalid REALITY target port")
		}
	}
	destination := settings.Destination
	if destination == "" {
		destination = name
	} // Panel UI explicitly documents SNI fallback.
	if host, existingPort, err := net.SplitHostPort(destination); err == nil {
		if host == "" {
			return RealitySpec{}, errors.New("invalid REALITY target")
		}
		p, err := strconv.Atoi(existingPort)
		if err != nil || p < 1 || p > 65535 {
			return RealitySpec{}, errors.New("invalid REALITY target port")
		}
	} else {
		if strings.ContainsAny(destination, " /\\\t\r\n") || (strings.Contains(destination, ":") && net.ParseIP(destination) == nil) {
			return RealitySpec{}, errors.New("invalid REALITY target address")
		}
		destination = net.JoinHostPort(destination, strconv.FormatInt(port, 10))
	}
	xver := int64(0)
	if !emptySetting(settings.Xver) {
		xver, err = integerSetting(settings.Xver)
		if err != nil || xver < 0 || xver > 2 {
			return RealitySpec{}, errors.New("REALITY xver must be 0, 1 or 2")
		}
	}
	return RealitySpec{Destination: destination, ServerName: name, PrivateKey: private, ShortID: short, Xver: uint64(xver)}, nil
}

func integerSetting(raw []byte) (int64, error) {
	s := string(raw)
	if len(s) > 0 && s[0] == '"' {
		if json.Unmarshal(raw, &s) != nil {
			return 0, errors.New("invalid integer")
		}
	}
	return strconv.ParseInt(s, 10, 64)
}
