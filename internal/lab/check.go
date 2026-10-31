package lab

import (
	"encoding/json"
	"errors"

	"zic-xray-agent/internal/durable"
)

func CheckPhase(c Config, beforePath, afterPath, summaryPath, output string) error {
	if err := c.Validate(); err != nil {
		return err
	}
	load := func(path string, value any) error {
		raw, err := durable.ReadPrivate(path, 1<<20)
		if err != nil {
			return err
		}
		return json.Unmarshal(raw, value)
	}
	var before, after RecoverySnapshot
	var summary Summary
	if err := load(beforePath, &before); err != nil {
		return err
	}
	if err := load(afterPath, &after); err != nil {
		return err
	}
	if err := load(summaryPath, &summary); err != nil {
		return err
	}
	if before.LabID != c.LabID || after.LabID != c.LabID || summary.LabID != c.LabID {
		return errors.New("phase evidence belongs to another lab")
	}
	passed := summary.Synthetic && !summary.ProductionReady && summary.Passed && summary.Errors == 0 && summary.ResourcesMeasured && !summary.SendingEnabled && len(summary.UUIDs) == len(c.Users) && before.CoreEpoch == after.CoreEpoch && before.AgentIdentity == after.AgentIdentity && before.BootID == after.BootID && before.Incidents == after.Incidents
	type result struct {
		UUID     string `json:"uuid"`
		Expected Bytes  `json:"expected"`
		Ledger   Bytes  `json:"ledger_delta"`
		Exact    bool   `json:"exact"`
	}
	rows := []result{}
	for _, u := range c.Users {
		var expected Bytes
		found := false
		for _, r := range summary.UUIDs {
			if r.UserID == u.ID && r.UUID == u.UUID {
				passed = passed && !found && r.Exact && r.Expected == r.Stats
				expected = r.Expected
				found = true
			}
		}
		delta, err := difference(after.Known[u.ID], before.Known[u.ID])
		if err != nil {
			return err
		}
		exact := found && delta == expected
		passed = passed && exact
		rows = append(rows, result{u.UUID, expected, delta, exact})
	}
	if err := writeJSON(output, map[string]any{"synthetic": true, "production_ready": false, "sending_enabled": false, "passed": passed, "per_uuid": rows, "resource_budgets_evaluated": false}); err != nil {
		return err
	}
	if !passed {
		return errors.New("agent-ledger phase verification failed")
	}
	return nil
}
