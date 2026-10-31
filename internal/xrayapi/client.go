// Package xrayapi is a bounded local gRPC client for the official Xray API.
package xrayapi

import (
	"context"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"

	"zic-xray-agent/internal/accounting"
	"zic-xray-agent/internal/panel"
)

const handlerService = "/xray.app.proxyman.command.HandlerService/"

type Client struct{ connection *grpc.ClientConn }

func Connect(endpoint string) (*Client, error) {
	host, port, err := net.SplitHostPort(endpoint)
	p, parseErr := strconv.Atoi(port)
	if err != nil || parseErr != nil || p < 1 || p > 65535 || (host != "127.0.0.1" && host != "::1") {
		return nil, errors.New("Xray API must be an explicit loopback IP:port")
	}
	conn, err := grpc.NewClient("passthrough:///"+endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDisableServiceConfig(), grpc.WithDisableRetry(),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(4<<20), grpc.MaxCallSendMsgSize(4<<20)),
	)
	if err != nil {
		return nil, errors.New("cannot initialize Xray API client")
	}
	return &Client{conn}, nil
}

func (c *Client) Close() error { return c.connection.Close() }
func (c *Client) invoke(ctx context.Context, method string, request, response *dynamicpb.Message) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if c.connection.Invoke(ctx, method, request, response) != nil {
		return errors.New("Xray API request failed; inspect availability/API permissions locally")
	}
	return nil
}

type User struct{ Email, UUID, Flow string }

func userMessage(u User) *dynamicpb.Message {
	account := message("Account")
	set(account, "id", protoreflect.ValueOfString(u.UUID))
	set(account, "flow", protoreflect.ValueOfString(u.Flow))
	user := message("User")
	set(user, "email", protoreflect.ValueOfString(u.Email))
	set(user, "account", protoreflect.ValueOfMessage(typed("xray.proxy.vless.Account", account)))
	return user
}

func (c *Client) AddUser(ctx context.Context, tag string, u User) error {
	if tag == "" || u.Email == "" || !panel.ValidUUID(u.UUID) || (u.Flow != "" && u.Flow != "xtls-rprx-vision") {
		return errors.New("invalid VLESS API user")
	}
	op := message("AddUser")
	set(op, "user", protoreflect.ValueOfMessage(userMessage(u)))
	return c.alter(ctx, tag, "xray.app.proxyman.command.AddUserOperation", op)
}
func (c *Client) RemoveUser(ctx context.Context, tag, email string) error {
	if tag == "" || email == "" {
		return errors.New("invalid VLESS API identity")
	}
	op := message("RemoveUser")
	set(op, "email", protoreflect.ValueOfString(email))
	return c.alter(ctx, tag, "xray.app.proxyman.command.RemoveUserOperation", op)
}
func (c *Client) alter(ctx context.Context, tag, kind string, operation *dynamicpb.Message) error {
	req := message("AlterInbound")
	set(req, "tag", protoreflect.ValueOfString(tag))
	set(req, "operation", protoreflect.ValueOfMessage(typed(kind, operation)))
	return c.invoke(ctx, handlerService+"AlterInbound", req, message("Empty"))
}

func (c *Client) Users(ctx context.Context, tag string) ([]User, error) {
	req, resp := message("GetUsers"), message("Users")
	set(req, "tag", protoreflect.ValueOfString(tag))
	if err := c.invoke(ctx, handlerService+"GetInboundUsers", req, resp); err != nil {
		return nil, err
	}
	users := list(resp, "users")
	if users.Len() > panel.MaxUsers {
		return nil, errors.New("Xray user limit exceeded")
	}
	result := make([]User, 0, users.Len())
	for i := 0; i < users.Len(); i++ {
		u := users.Get(i).Message()
		account := child(u, "account")
		if textField(account, "type") != "xray.proxy.vless.Account" {
			return nil, errors.New("inbound has an unsupported account type")
		}
		value := message("Account")
		if proto.Unmarshal(account.Get(account.Descriptor().Fields().ByName("value")).Bytes(), value) != nil || len(value.GetUnknown()) != 0 {
			return nil, errors.New("unsupported VLESS account settings")
		}
		result = append(result, User{Email: textField(u, "email"), UUID: textField(value, "id"), Flow: textField(value, "flow")})
	}
	return result, nil
}

func (c *Client) Counters(ctx context.Context, nodeID int64) (map[string]accounting.Counter, error) {
	if nodeID <= 0 {
		return nil, errors.New("invalid stats node ID")
	}
	req, resp := message("StatsQuery"), message("Stats")
	prefix := "user>>>zic." + strconv.FormatInt(nodeID, 10) + "."
	set(req, "pattern", protoreflect.ValueOfString(prefix))
	// reset is deliberately false, including on errors/retries.
	if err := c.invoke(ctx, "/xray.app.stats.command.StatsService/QueryStats", req, resp); err != nil {
		return nil, err
	}
	stats := list(resp, "stat")
	if stats.Len() > accounting.MaxCounters {
		return nil, errors.New("Xray counter limit exceeded")
	}
	result := make(map[string]accounting.Counter, stats.Len())
	for i := 0; i < stats.Len(); i++ {
		stat := stats.Get(i).Message()
		name := textField(stat, "name")
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		parts := strings.Split(name, ">>>")
		if len(parts) != 4 || parts[2] != "traffic" {
			return nil, errors.New("invalid managed counter name")
		}
		uid, ok := panel.StatsUserID(parts[1], nodeID)
		if !ok {
			return nil, errors.New("unrecognized managed counter identity")
		}
		direction := ""
		switch parts[3] {
		case "uplink":
			direction = "upload"
		case "downlink":
			direction = "download"
		default:
			continue
		}
		value := stat.Get(stat.Descriptor().Fields().ByName("value")).Int()
		if value < 0 {
			return nil, errors.New("negative Xray traffic counter")
		}
		if _, exists := result[name]; exists {
			return nil, errors.New("duplicate Xray traffic counter")
		}
		result[name] = accounting.Counter{UserID: uid, Direction: direction, Value: uint64(value)}
	}
	return result, nil
}
