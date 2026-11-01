package panel

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"zic-xray-agent/internal/logging"
	"zic-xray-agent/internal/strictjson"
)

const ProductionCommit = "f778400907da4c47f0466abcab21030009e2770e"
const MaxUsers = 5000

type User struct {
	ID          int64  `json:"id"`
	UUID        string `json:"uuid"`
	SpeedLimit  *int64 `json:"speed_limit,omitempty"`
	DeviceLimit *int64 `json:"device_limit,omitempty"`
}

type UsersResult struct {
	Users       []User
	ETag        string
	NotModified bool
}

func (c *Client) FetchUsers(ctx context.Context, auth Credentials, etag string) (UsersResult, error) {
	result, err := c.fetch(ctx, auth, "user", etag, 2<<20)
	if err != nil {
		return UsersResult{}, err
	}
	if result.NotModified {
		return UsersResult{NotModified: true, ETag: result.ETag}, nil
	}
	users, err := ParseUsers(result.Raw)
	if err != nil {
		return UsersResult{}, err
	}
	return UsersResult{Users: users, ETag: result.ETag}, nil
}

func ParseUsers(raw []byte) ([]User, error) {
	if err := strictjson.Validate(raw); err != nil {
		return nil, err
	}
	var response struct {
		Users json.RawMessage `json:"users"`
	}
	if json.Unmarshal(raw, &response) != nil || len(response.Users) == 0 || response.Users[0] != '[' {
		return nil, errors.New("panel must return an explicit users array")
	}
	var users []User
	decoder := json.NewDecoder(bytes.NewReader(response.Users))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&users) != nil || len(users) > MaxUsers {
		return nil, errors.New("invalid/unsupported user fields or user limit exceeded")
	}
	ids := make(map[int64]bool, len(users))
	uuids := make(map[string]bool, len(users))
	for i := range users {
		u := &users[i]
		u.UUID = strings.ToLower(u.UUID)
		if u.ID <= 0 || !ValidUUID(u.UUID) || ids[u.ID] || uuids[u.UUID] {
			return nil, errors.New("invalid or duplicate user identity")
		}
		if (u.SpeedLimit != nil && *u.SpeedLimit < 0) || (u.DeviceLimit != nil && *u.DeviceLimit < 0) {
			return nil, errors.New("invalid user limits")
		}
		ids[u.ID], uuids[u.UUID] = true, true
	}
	return users, nil
}

func ValidUUID(s string) bool {
	if len(s) != 36 || s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	bytes, err := hex.DecodeString(strings.ReplaceAll(s, "-", ""))
	if err != nil || len(bytes) != 16 {
		return false
	}
	for _, b := range bytes {
		if b != 0 {
			return true
		}
	}
	return false
}

// Email is an internal Xray stats identity, not a customer's email. Including a
// credential generation prevents UUID rotations from aliasing old counters.
func (u User) Email(nodeID int64) string {
	hash := sha256.Sum256([]byte(u.UUID))
	return fmt.Sprintf("zic.%d.%d.%x@agent", nodeID, u.ID, hash[:8])
}

func StatsUserID(email string, nodeID int64) (int64, bool) {
	prefix := "zic." + strconv.FormatInt(nodeID, 10) + "."
	if !strings.HasPrefix(email, prefix) || !strings.HasSuffix(email, "@agent") {
		return 0, false
	}
	parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(email, prefix), "@agent"), ".")
	if len(parts) != 2 || len(parts[1]) != 16 {
		return 0, false
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return 0, false
	}
	id, err := strconv.ParseInt(parts[0], 10, 64)
	return id, err == nil && id > 0
}

func RequireSupportedPolicies(users []User) error {
	for _, u := range users {
		if (u.SpeedLimit != nil && *u.SpeedLimit > 0) || (u.DeviceLimit != nil && *u.DeviceLimit > 0) {
			return logging.Failure(logging.Policy, nil)
		}
	}
	return nil
}
