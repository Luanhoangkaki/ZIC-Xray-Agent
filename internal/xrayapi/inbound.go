package xrayapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"zic-xray-agent/internal/panel"
)

func (c *Client) AddInbound(ctx context.Context, tag, listenIP string, spec panel.VLESSSpec) error {
	inbound, err := buildInbound(tag, listenIP, spec)
	if err != nil {
		return err
	}
	req := message("AddInbound")
	set(req, "inbound", protoreflect.ValueOfMessage(inbound))
	return c.invoke(ctx, handlerService+"AddInbound", req, message("Empty"))
}

func buildInbound(tag, listenIP string, spec panel.VLESSSpec) (*dynamicpb.Message, error) {
	if !strings.HasPrefix(tag, "zic-node-") || spec.Port < 1 || spec.Port > 65535 {
		return nil, errors.New("invalid managed inbound identity/port")
	}
	ip := net.ParseIP(listenIP)
	if ip == nil {
		return nil, errors.New("listen address must be an explicit IP")
	}
	if spec.TLS == 0 && !ip.IsLoopback() {
		return nil, errors.New("plaintext VLESS is restricted to local tests")
	}
	if spec.TLS != 0 && (spec.TLS != 2 || spec.Reality == nil) {
		return nil, errors.New("unsupported VLESS security; refusing fallback")
	}
	address := message("Address")
	if v4 := ip.To4(); v4 != nil {
		ip = v4
	}
	set(address, "ip", protoreflect.ValueOfBytes(ip))
	rangeMessage := message("PortRange")
	set(rangeMessage, "from", protoreflect.ValueOfUint32(spec.Port))
	set(rangeMessage, "to", protoreflect.ValueOfUint32(spec.Port))
	ports := message("PortList")
	appendMessage(ports, "range", rangeMessage)
	stream := message("Stream")
	set(stream, "protocol_name", protoreflect.ValueOfString("tcp"))
	if spec.Reality != nil {
		reality := message("Reality")
		r := spec.Reality
		if len(r.PrivateKey) != 32 || len(r.ShortID) != 8 || r.Destination == "" || r.ServerName == "" || r.Xver > 2 {
			return nil, errors.New("invalid REALITY specification")
		}
		set(reality, "dest", protoreflect.ValueOfString(r.Destination))
		set(reality, "type", protoreflect.ValueOfString("tcp"))
		set(reality, "xver", protoreflect.ValueOfUint64(r.Xver))
		set(reality, "private_key", protoreflect.ValueOfBytes(r.PrivateKey))
		reality.Mutable(reality.Descriptor().Fields().ByName("server_names")).List().Append(protoreflect.ValueOfString(r.ServerName))
		reality.Mutable(reality.Descriptor().Fields().ByName("short_ids")).List().Append(protoreflect.ValueOfBytes(r.ShortID))
		set(stream, "security_type", protoreflect.ValueOfString("xray.transport.internet.reality.Config"))
		appendMessage(stream, "security_settings", typed("xray.transport.internet.reality.Config", reality))
	}
	receiver := message("Receiver")
	set(receiver, "port_list", protoreflect.ValueOfMessage(ports))
	set(receiver, "listen", protoreflect.ValueOfMessage(address))
	set(receiver, "stream_settings", protoreflect.ValueOfMessage(stream))
	proxy := message("VLESS")
	set(proxy, "decryption", protoreflect.ValueOfString("none"))
	inbound := message("Inbound")
	set(inbound, "tag", protoreflect.ValueOfString(tag))
	set(inbound, "receiver_settings", protoreflect.ValueOfMessage(typed("xray.app.proxyman.ReceiverConfig", receiver)))
	set(inbound, "proxy_settings", protoreflect.ValueOfMessage(typed("xray.proxy.vless.inbound.Config", proxy)))
	return inbound, nil
}

func (c *Client) RemoveInbound(ctx context.Context, tag string) error {
	if !strings.HasPrefix(tag, "zic-node-") {
		return errors.New("refusing to remove an unmanaged inbound")
	}
	req := message("RemoveInbound")
	set(req, "tag", protoreflect.ValueOfString(tag))
	return c.invoke(ctx, handlerService+"RemoveInbound", req, message("Empty"))
}

// ReconcileUsers observes actual core state each time, including after restart
// or a lost mutation response. No retry is based on a stale local user cache.
// It requires a dedicated managed inbound and a checkpoint before removals.
func (c *Client) ReconcileUsers(ctx context.Context, tag string, nodeID int64, flow string, desired []panel.User, beforeRemove func() error) error {
	if tag != "zic-node-"+strconv.FormatInt(nodeID, 10) || nodeID <= 0 || len(desired) > panel.MaxUsers {
		return errors.New("invalid managed inbound")
	}
	if err := panel.RequireSupportedPolicies(desired); err != nil {
		return err
	}
	current, err := c.Users(ctx, tag)
	if err != nil {
		return err
	}
	wanted := make(map[string]User, len(desired))
	for _, u := range desired {
		if !panel.ValidUUID(u.UUID) || u.ID <= 0 {
			return errors.New("invalid desired user identity")
		}
		email := u.Email(nodeID)
		if _, exists := wanted[email]; exists {
			return errors.New("duplicate desired user")
		}
		wanted[email] = User{Email: email, UUID: u.UUID, Flow: flow}
	}
	actual := make(map[string]User, len(current))
	for _, u := range current {
		if _, ok := panel.StatsUserID(u.Email, nodeID); !ok {
			return errors.New("inbound contains unmanaged users; refusing to take ownership")
		}
		actual[u.Email] = u
	}
	checkpointed := false
	for email, u := range actual {
		if target, exists := wanted[email]; exists && target == u {
			continue
		}
		if !checkpointed {
			if beforeRemove == nil {
				return errors.New("traffic checkpoint required before user removal")
			}
			if err := beforeRemove(); err != nil {
				return err
			}
			checkpointed = true
		}
		if err := c.RemoveUser(ctx, tag, email); err != nil {
			return err
		}
		delete(actual, email)
	}
	for email, u := range wanted {
		if actual[email] == u {
			continue
		}
		if err := c.AddUser(ctx, tag, u); err != nil {
			return err
		}
	}
	verified, err := c.Users(ctx, tag)
	if err != nil || len(verified) != len(wanted) {
		return errors.New("user reconciliation has not been verified")
	}
	for _, u := range verified {
		if wanted[u.Email] != u {
			return errors.New("core users differ from desired users")
		}
	}
	return nil
}
func fingerprint(inbound protoreflect.Message) (string, error) {
	receiver := child(inbound, "receiver_settings")
	proxy := child(inbound, "proxy_settings")
	if textField(receiver, "type") != "xray.app.proxyman.ReceiverConfig" || textField(proxy, "type") != "xray.proxy.vless.inbound.Config" {
		return "", errors.New("inbound is not an agent-supported VLESS receiver")
	}
	decoded := message("VLESS")
	if err := proto.Unmarshal(proxy.Get(proxy.Descriptor().Fields().ByName("value")).Bytes(), decoded); err != nil {
		return "", errors.New("invalid VLESS proxy settings")
	}
	decoded.Clear(decoded.Descriptor().Fields().ByName("users"))
	normalized := message("Inbound")
	set(normalized, "receiver_settings", protoreflect.ValueOfMessage(receiver))
	set(normalized, "proxy_settings", protoreflect.ValueOfMessage(typed("xray.proxy.vless.inbound.Config", decoded)))
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(normalized)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func (c *Client) PlanFingerprint(tag, listenIP string, spec panel.VLESSSpec) (string, error) {
	inbound, err := buildInbound(tag, listenIP, spec)
	if err != nil {
		return "", err
	}
	return fingerprint(inbound)
}
func (c *Client) InspectInbound(ctx context.Context, tag string) (string, bool, error) {
	req, resp := message("ListInbounds"), message("Inbounds")
	if err := c.invoke(ctx, handlerService+"ListInbounds", req, resp); err != nil {
		return "", false, err
	}
	inbounds := list(resp, "inbounds")
	if inbounds.Len() > 1024 {
		return "", false, errors.New("inbound count limit exceeded")
	}
	for i := 0; i < inbounds.Len(); i++ {
		in := inbounds.Get(i).Message()
		if textField(in, "tag") == tag {
			hash, err := fingerprint(in)
			return hash, true, err
		}
	}
	return "", false, nil
}
