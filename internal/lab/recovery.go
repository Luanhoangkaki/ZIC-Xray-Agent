package lab

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"zic-xray-agent/internal/daemon"
	"zic-xray-agent/internal/durable"
	"zic-xray-agent/internal/xrayapi"
)

func Ready(ctx context.Context, c Config, ca string, panelOnly, coreOnly bool) error {
	for {
		ok := true
		if !coreOnly {
			client, err := HTTPClient(c, ca)
			if err != nil {
				return err
			}
			requestURL := c.Credentials().PanelURL + "/api/v1/server/UniProxy/config?node_type=vless&node_id=" + strconv.FormatInt(NodeID, 10) + "&token=" + c.Token
			req, err := newGet(ctx, requestURL)
			if err != nil {
				return safeError("readiness")
			}
			response, err := client.Do(req)
			if err != nil {
				ok = false
			} else {
				io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
				response.Body.Close()
				ok = response.StatusCode == 200
			}
			client.CloseIdleConnections()
		}
		if !panelOnly {
			client, err := xrayapi.Connect(c.Address(1))
			if err != nil {
				return err
			}
			users, e := client.Users(ctx, "zic-node-"+strconv.FormatInt(NodeID, 10))
			client.Close()
			if e != nil || len(users) != 2 {
				ok = false
			} else {
				for _, u := range c.Users {
					found := false
					for _, actual := range users {
						if actual.UUID == u.UUID && actual.Email == u.Email(NodeID) {
							found = true
						}
					}
					ok = ok && found
				}
			}
		}
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return safeError("readiness timeout")
		case <-time.After(250 * time.Millisecond):
		}
	}
}

type RecoverySnapshot struct {
	LabID         string          `json:"lab_id"`
	BootID        string          `json:"boot_id"`
	CoreEpoch     string          `json:"core_epoch"`
	AgentIdentity string          `json:"agent_process_identity"`
	Known         map[int64]Bytes `json:"known_ledger_bytes"`
	Stats         map[int64]Bytes `json:"stats_bytes"`
	Incidents     int             `json:"incident_count"`
}

func Snapshot(ctx context.Context, c Config, agentPID int, output string) (RecoverySnapshot, error) {
	var s RecoverySnapshot
	s.LabID = c.LabID
	epoch, err := daemon.ProcessEpoch(c.CorePIDFile)
	if err != nil {
		return s, err
	}
	s.CoreEpoch = epoch
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return s, err
	}
	s.BootID = strings.TrimSpace(string(boot))
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(agentPID) + "/stat")
	if err != nil {
		return s, err
	}
	end := strings.LastIndex(string(stat), ")")
	if end < 0 {
		return s, errors.New("invalid agent process stat")
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 20 {
		return s, errors.New("short agent process stat")
	}
	s.AgentIdentity = strconv.Itoa(agentPID) + ":" + fields[19]
	for {
		stats, err := Stats(ctx, c)
		if err != nil {
			return s, err
		}
		known, l, err := LedgerTotals(filepath.Join(c.AgentState, "traffic.json"))
		if err == nil && l.Epoch == epoch {
			matched := true
			for _, u := range c.Users {
				if l.Counters["user>>>"+u.Email(NodeID)+">>>traffic>>>uplink"].Value != stats[u.ID].Upload || l.Counters["user>>>"+u.Email(NodeID)+">>>traffic>>>downlink"].Value != stats[u.ID].Download {
					matched = false
				}
			}
			if matched {
				s.Known = known
				s.Stats = stats
				s.Incidents = len(l.Incidents)
				if output != "" {
					if err := writeJSON(output, s); err != nil {
						return s, err
					}
				}
				return s, nil
			}
		}
		select {
		case <-ctx.Done():
			return s, safeError("waiting for an up-to-date agent checkpoint")
		case <-time.After(time.Second):
		}
	}
}
func CompareRecovery(ctx context.Context, c Config, agentPID int, beforePath, kind, output string) error {
	raw, err := durable.ReadPrivate(beforePath, 1<<20)
	if err != nil {
		return err
	}
	var before RecoverySnapshot
	if json.Unmarshal(raw, &before) != nil || before.LabID != c.LabID {
		return errors.New("recovery snapshot belongs to another lab")
	}
	after, err := Snapshot(ctx, c, agentPID, "")
	if err != nil {
		return err
	}
	passed := true
	for _, u := range c.Users {
		passed = passed && before.Known[u.ID] == after.Known[u.ID]
	}
	switch kind {
	case "restart":
		passed = passed && before.BootID == after.BootID && before.CoreEpoch == after.CoreEpoch && before.AgentIdentity != after.AgentIdentity && before.Incidents == after.Incidents
		for _, u := range c.Users {
			passed = passed && before.Stats[u.ID] == after.Stats[u.ID]
		}
	case "reboot":
		passed = passed && before.BootID != after.BootID && before.CoreEpoch != after.CoreEpoch && after.Incidents == before.Incidents+1
	case "core-restart":
		passed = passed && before.BootID == after.BootID && before.CoreEpoch != after.CoreEpoch && after.Incidents == before.Incidents+1
	default:
		return errors.New("recovery kind must be restart, core-restart or reboot")
	}
	if err := writeJSON(output, map[string]any{"synthetic": true, "production_ready": false, "kind": kind, "passed": passed, "before": before, "after": after, "warning": "retained known bytes does not prove absence of an unobserved old-core tail"}); err != nil {
		return err
	}
	if !passed {
		return errors.New("recovery invariants failed")
	}
	return nil
}
