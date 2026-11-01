// Package lab provides synthetic, loopback-only acceptance tooling. It is never
// imported by the agent and cannot send traffic reports to a panel.
package lab

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"path/filepath"
	"time"

	"zic-xray-agent/internal/daemon"
	"zic-xray-agent/internal/durable"
	"zic-xray-agent/internal/panel"
)

const NodeID int64 = 990001

type Config struct {
	Version     int          `json:"version"`
	Synthetic   bool         `json:"synthetic"`
	LabID       string       `json:"lab_id"`
	Root        string       `json:"root"`
	BasePort    int          `json:"base_port"`
	Token       string       `json:"token"`
	Users       []panel.User `json:"users"`
	AgentState  string       `json:"agent_state"`
	CorePIDFile string       `json:"core_pid_file"`
}

func (c Config) Address(offset int) string {
	return net.JoinHostPort("127.0.0.1", fmt.Sprint(c.BasePort+offset))
}
func (c Config) Credentials() panel.Credentials {
	return panel.Credentials{PanelURL: "https://" + c.Address(0), Token: c.Token, NodeID: NodeID, NodeType: "vless"}
}
func (c Config) Validate() error {
	if c.Version != 1 || !c.Synthetic || c.LabID == "" || len(c.Token) < 32 || len(c.Users) != 2 || c.BasePort < 1024 || c.BasePort > 65529 || !filepath.IsAbs(c.Root) || !filepath.IsAbs(c.AgentState) || !filepath.IsAbs(c.CorePIDFile) {
		return errors.New("invalid synthetic lab configuration")
	}
	for _, u := range c.Users {
		if !panel.ValidUUID(u.UUID) || u.ID < 10001 || u.ID > 10002 {
			return errors.New("lab accepts only its two generated synthetic identities")
		}
	}
	if c.Users[0].ID == c.Users[1].ID || c.Users[0].UUID == c.Users[1].UUID {
		return errors.New("duplicate synthetic identity")
	}
	return nil
}
func Load(path string) (Config, error) {
	var c Config
	raw, err := durable.ReadPrivate(path, 32<<10)
	if err != nil {
		return c, errors.New("cannot read private lab manifest")
	}
	if json.Unmarshal(raw, &c) != nil {
		return c, errors.New("invalid lab manifest")
	}
	return c, c.Validate()
}
func writeJSON(path string, value any) error {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return durable.Write(path, raw)
}
func randomHex(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func randomUUID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	b[6] = b[6]&15 | 64
	b[8] = b[8]&63 | 128
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[:4], b[4:6], b[6:8], b[8:10], b[10:]), nil
}

// Initialize writes NEW lab files only. It never opens existing Xray configs or
// a real panel and never accepts imported credentials/customer UUIDs.
func Initialize(root string, basePort int, agentState, pidFile string) (Config, error) {
	c := Config{Version: 1, Synthetic: true, Root: root, BasePort: basePort, AgentState: agentState, CorePIDFile: pidFile}
	var err error
	c.LabID, err = randomHex(12)
	if err != nil {
		return c, err
	}
	c.Token, err = randomHex(24)
	if err != nil {
		return c, err
	}
	for i := 0; i < 2; i++ {
		uuid, err := randomUUID()
		if err != nil {
			return c, err
		}
		c.Users = append(c.Users, panel.User{ID: int64(10001 + i), UUID: uuid})
	}
	if err := c.Validate(); err != nil {
		return c, err
	}
	dir := filepath.Join(root, "config")
	if err := durable.PrivateDir(dir); err != nil {
		return c, err
	}
	if entries, err := readEmptyDirectory(dir); err != nil || !entries {
		return c, errors.New("lab config directory must be empty; refusing overwrite")
	}
	if err := CheckPorts(c); err != nil {
		return c, err
	}
	if err := certificates(dir); err != nil {
		return c, err
	}
	if err := writeJSON(filepath.Join(dir, "lab.json"), c); err != nil {
		return c, err
	}
	agent := daemon.Config{Credentials: c.Credentials(), Pull: 60, Push: 60, LogLevel: "warn", Mode: "observe", PanelCommit: panel.ProductionCommit, XrayEndpoint: c.Address(1), XrayPIDFile: pidFile}
	if err := writeJSON(filepath.Join(dir, "agent.json"), agent); err != nil {
		return c, err
	}
	if err := writeJSON(filepath.Join(dir, "xray.json"), c.CoreConfig()); err != nil {
		return c, err
	}
	return c, nil
}
func CheckPorts(c Config) error {
	var listeners []net.Listener
	defer func() {
		for _, l := range listeners {
			l.Close()
		}
	}()
	for i := 0; i < 6; i++ {
		l, err := net.Listen("tcp", c.Address(i))
		if err != nil {
			return fmt.Errorf("lab loopback port %d unavailable; nothing was stopped", c.BasePort+i)
		}
		listeners = append(listeners, l)
	}
	return nil
}
func (c Config) NodeConfig() map[string]any {
	return map[string]any{"server_port": c.BasePort + 5, "network": "tcp", "networkSettings": nil, "tls": 0, "flow": nil, "tls_settings": nil, "encryption": nil, "encryption_settings": nil, "base_config": map[string]int{"pull_interval": 60, "push_interval": 60}}
}
func (c Config) CoreConfig() map[string]any {
	clients := []any{}
	for _, u := range c.Users {
		clients = append(clients, map[string]any{"id": u.UUID, "email": u.Email(NodeID), "level": 0})
	}
	inbounds := []any{map[string]any{"tag": "lab-api-in", "listen": "127.0.0.1", "port": c.BasePort + 1, "protocol": "dokodemo-door", "settings": map[string]any{"address": "127.0.0.1"}}, map[string]any{"tag": fmt.Sprintf("zic-node-%d", NodeID), "listen": "127.0.0.1", "port": c.BasePort + 5, "protocol": "vless", "settings": map[string]any{"clients": clients, "decryption": "none"}, "streamSettings": map[string]any{"network": "tcp", "security": "none"}}}
	outbounds := []any{map[string]any{"tag": "lab-direct", "protocol": "freedom", "settings": map[string]any{"finalRules": []any{map[string]any{"action": "allow", "ip": []string{"127.0.0.1/32"}}}}}}
	rules := []any{map[string]any{"type": "field", "inboundTag": []string{"lab-api-in"}, "outboundTag": "lab-api"}}
	for i, u := range c.Users {
		tag := fmt.Sprintf("lab-socks-%d", i)
		out := fmt.Sprintf("lab-client-%d", i)
		inbounds = append(inbounds, map[string]any{"tag": tag, "listen": "127.0.0.1", "port": c.BasePort + 3 + i, "protocol": "socks", "settings": map[string]any{"auth": "noauth", "udp": false}})
		outbounds = append(outbounds, map[string]any{"tag": out, "protocol": "vless", "settings": map[string]any{"vnext": []any{map[string]any{"address": "127.0.0.1", "port": c.BasePort + 5, "users": []any{map[string]any{"id": u.UUID, "encryption": "none"}}}}}, "streamSettings": map[string]any{"network": "tcp", "security": "none"}})
		rules = append(rules, map[string]any{"type": "field", "inboundTag": []string{tag}, "outboundTag": out})
	}
	// The synthetic server may reach only the lab echo, never another local
	// service or an external panel, even if a lab UUID is copied accidentally.
	rules = append(rules,
		map[string]any{"type": "field", "inboundTag": []string{fmt.Sprintf("zic-node-%d", NodeID)}, "ip": []string{"127.0.0.1/32"}, "port": fmt.Sprint(c.BasePort + 2), "outboundTag": "lab-direct"},
		map[string]any{"type": "field", "inboundTag": []string{fmt.Sprintf("zic-node-%d", NodeID)}, "outboundTag": "lab-block"})
	outbounds = append(outbounds, map[string]any{"tag": "lab-block", "protocol": "blackhole"})
	return map[string]any{"log": map[string]any{"loglevel": "none", "access": "none", "error": "none", "dnsLog": false}, "api": map[string]any{"tag": "lab-api", "services": []string{"HandlerService", "StatsService"}}, "stats": map[string]any{}, "policy": map[string]any{"levels": map[string]any{"0": map[string]any{"statsUserUplink": true, "statsUserDownlink": true}}}, "inbounds": inbounds, "outbounds": outbounds, "routing": map[string]any{"rules": rules}}
}
func certificates(dir string) error {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return err
	}
	cert := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: "ZIC isolated synthetic lab"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(30 * 24 * time.Hour), IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"zic-lab.invalid"}, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		return err
	}
	pk, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	for name, data := range map[string][]byte{"ca.pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), "cert.pem": pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), "key.pem": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk})} {
		if err := durable.Write(filepath.Join(dir, name), data); err != nil {
			return err
		}
	}
	return nil
}
func HTTPClient(c Config, caPath string) (*http.Client, error) {
	if caPath == "" {
		caPath = filepath.Join(c.Root, "config", "ca.pem")
	}
	raw, err := durable.ReadPrivate(caPath, 64<<10)
	if err != nil {
		return nil, errors.New("cannot read lab CA")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		return nil, errors.New("invalid lab CA")
	}
	client := panel.NewClient().HTTP
	client.Transport.(*http.Transport).TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	return client, nil
}
func digest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }
