// Package daemon runs an observer, not a proxy/core or a production node manager.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"zic-xray-agent/internal/accounting"
	"zic-xray-agent/internal/control"
	"zic-xray-agent/internal/durable"
	"zic-xray-agent/internal/logging"
	"zic-xray-agent/internal/panel"
)

type Panel interface {
	FetchConfig(context.Context, panel.Credentials, string) (panel.Result, error)
	FetchUsers(context.Context, panel.Credentials, string) (panel.UsersResult, error)
}
type Stats interface {
	Counters(context.Context, int64) (map[string]accounting.Counter, error)
}
type Events interface{ Event(string) error }

type Snapshot struct {
	Version         int             `json:"version"`
	Identity        string          `json:"identity"`
	Config          json.RawMessage `json:"config"`
	ConfigETag      string          `json:"config_etag"`
	Users           []panel.User    `json:"users"`
	UsersETag       string          `json:"users_etag"`
	LastSuccess     time.Time       `json:"last_success"`
	Pull            int             `json:"pull_interval_seconds"`
	Push            int             `json:"push_interval_seconds,omitempty"`
	PolicySupported bool            `json:"policy_supported"`
}

type Runner struct {
	Config      Config
	Panel       Panel
	Stats       Stats
	Epoch       func() (string, error)
	Log         Events
	StateDir    string
	ControlCore control.Core
	snapshot    Snapshot
	ledger      *accounting.Ledger
	controller  *control.Controller
}

func (r *Runner) Initialize() (func(), error) {
	if err := r.Config.Validate(); err != nil {
		return nil, err
	}
	if err := durable.PrivateDir(r.StateDir); err != nil {
		return nil, err
	}
	unlock, err := durable.Lock(filepath.Join(r.StateDir, "agent.lock"))
	if err != nil {
		return nil, err
	}
	if err := r.load(); err != nil {
		unlock()
		return nil, err
	}
	if r.Config.Mode == "manage-test" {
		r.controller = &control.Controller{Core: r.ControlCore, Path: filepath.Join(r.StateDir, "controller.json"), Identity: r.Config.Identity(), ListenIP: r.Config.ListenIP, NodeID: r.Config.NodeID, Epoch: r.Epoch, BeforeRemove: func(ctx context.Context) error {
			if err := r.Collect(ctx); err != nil {
				var fatal *AccountingError
				if !errors.As(err, &fatal) {
					_ = r.ledger.RecordIncident(filepath.Join(r.StateDir, "traffic.json"), "pre_mutation_checkpoint_failed", r.ledger.Epoch)
				}
				return err
			}
			return nil
		}}
		if err := r.controller.Load(); err != nil {
			unlock()
			return nil, err
		}
	}
	return unlock, nil
}

func (r *Runner) load() error {
	err := durable.Load(filepath.Join(r.StateDir, "snapshot.json"), &r.snapshot)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("invalid synchronization checkpoint; refusing to overwrite")
	}
	if err == nil {
		if r.snapshot.Version != 1 || r.snapshot.Identity != r.Config.Identity() {
			return errors.New("snapshot identity/version mismatch")
		}
		pull, push, err := panel.ValidateNodeConfig(r.snapshot.Config)
		if err != nil || r.snapshot.Pull != pull {
			return errors.New("invalid cached node configuration")
		}
		if r.snapshot.Push != 0 && r.snapshot.Push != push {
			return errors.New("invalid cached push interval")
		}
		r.snapshot.Push = push
		raw, _ := json.Marshal(struct {
			Users []panel.User `json:"users"`
		}{r.snapshot.Users})
		if _, err := panel.ParseUsers(raw); err != nil {
			return errors.New("invalid cached users")
		}
	}
	ledger, err := accounting.Open(filepath.Join(r.StateDir, "traffic.json"), r.Config.Identity())
	if err != nil {
		return err
	}
	r.ledger = ledger
	return nil
}

// Sync commits a matching config/user snapshot before advancing either ETag.
// It never applies a user, listens on a proxy port, or sends traffic to panel.
func (r *Runner) Sync(ctx context.Context) error {
	next := r.snapshot
	config, err := r.Panel.FetchConfig(ctx, r.Config.Credentials, r.snapshot.ConfigETag)
	if err != nil {
		return err
	}
	if config.NotModified {
		if len(next.Config) == 0 {
			return errors.New("304 without a durable node snapshot")
		}
	} else {
		next.Config, next.ConfigETag, next.Pull, next.Push = config.Raw, config.ETag, config.PullInterval, config.PushInterval
	}
	users, err := r.Panel.FetchUsers(ctx, r.Config.Credentials, r.snapshot.UsersETag)
	if err != nil {
		return err
	}
	if users.NotModified {
		if r.snapshot.LastSuccess.IsZero() {
			return errors.New("304 without a durable user snapshot")
		}
	} else {
		next.Users, next.UsersETag = users.Users, users.ETag
	}
	next.PolicySupported = panel.RequireSupportedPolicies(next.Users) == nil
	if _, err := panel.ParseVLESS(next.Config); err != nil {
		next.PolicySupported = false
	}
	next.Version, next.Identity, next.LastSuccess = 1, r.Config.Identity(), time.Now().UTC()
	if err := durable.Save(filepath.Join(r.StateDir, "snapshot.json"), next); err != nil {
		return &StorageError{}
	}
	r.snapshot = next
	if !next.PolicySupported {
		return r.Log.Event("policy_not_supported")
	}
	return nil
}

type StorageError struct{}

func (*StorageError) Unwrap() error { return logging.Failure(logging.Storage, nil) }

func (*StorageError) Error() string {
	return "durable state write failed; stop and reload before continuing"
}

func (r *Runner) Collect(ctx context.Context) error {
	if r.Stats == nil {
		return nil
	}
	if r.Epoch == nil {
		return errors.New("trusted core epoch source is required")
	}
	before, err := r.Epoch()
	if err != nil {
		return err
	}
	counters, err := r.Stats.Counters(ctx, r.Config.NodeID)
	if err != nil {
		return err
	}
	after, err := r.Epoch()
	if err != nil || before != after {
		return errors.New("Xray restarted during Stats read; sample rejected")
	}
	// Any accounting anomaly is fatal: repeatedly retrying/rebaselining it would
	// hide a gap. Existing checkpoint remains available for manual reconciliation.
	path := filepath.Join(r.StateDir, "traffic.json")
	if r.ledger.Epoch != "" && r.ledger.Epoch != before {
		if err := r.ledger.TransitionEpoch(path, before, counters); err != nil {
			return &AccountingError{}
		}
		return nil
	}
	if err := r.ledger.Observe(path, before, counters); err != nil {
		if errors.Is(err, accounting.ErrObservation) {
			_ = r.ledger.RecordIncident(path, "counter_anomaly", before)
		}
		return &AccountingError{}
	}
	return nil
}

type AccountingError struct{}

func (*AccountingError) Unwrap() error { return logging.Failure(logging.Accounting, nil) }

func (*AccountingError) Error() string {
	return "traffic checkpoint/reset/epoch anomaly; reconciliation required; existing ledger retained"
}

// Run uses one timer and one worker. Panel backoff never stretches Stats or
// queue sampling intervals, and stale authorization has its own deadline.
func (r *Runner) Run(ctx context.Context) error {
	unlock, err := r.Initialize()
	if err != nil {
		return err
	}
	defer unlock()
	// No Stats endpoint is normal in the default observer, not a fault to log.
	nextSync, nextCollect, nextQueue := time.Now(), time.Now(), time.Now()
	failures := 0
	staleDisabled := false
	for {
		if ctx.Err() != nil {
			return r.shutdown()
		}
		now := time.Now()
		if r.Stats != nil && !now.Before(nextCollect) {
			if err := r.Collect(ctx); err != nil {
				if ctx.Err() != nil {
					return r.shutdown()
				}
				var fatal *AccountingError
				if errors.As(err, &fatal) {
					return r.abort(err)
				}
				if err := r.Log.Event("xray_unavailable"); err != nil {
					return err
				}
			}
			nextCollect = time.Now().Add(r.pushInterval())
		}
		if !time.Now().Before(nextSync) {
			syncErr := r.Sync(ctx)
			if ctx.Err() != nil {
				return r.shutdown()
			}
			if syncErr != nil {
				var fatal *StorageError
				if errors.As(syncErr, &fatal) {
					return r.abort(syncErr)
				}
				failures++
				if failures > 5 {
					failures = 5
				}
				if err := r.Log.Event("panel_sync_failed"); err != nil {
					return err
				}
			} else {
				failures = 0
				staleDisabled = false
				if r.controller != nil {
					if err := r.Apply(ctx); err != nil {
						var ioErr *control.PersistenceError
						var accountingErr *AccountingError
						if errors.As(err, &ioErr) || errors.As(err, &accountingErr) {
							return r.abort(err)
						}
						fenceContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						fenceErr := r.controller.Fence(fenceContext)
						cancel()
						if fenceErr != nil {
							return errors.New("control update failed and inbound containment could not be verified")
						}
						if err := r.Log.Event("control_sync_failed"); err != nil {
							return err
						}
					}
				}
			}
			multiplier := failures
			if multiplier < 1 {
				multiplier = 1
			}
			interval := r.Config.Pull
			if r.snapshot.Pull > 0 {
				interval = r.snapshot.Pull
			}
			nextSync = time.Now().Add(time.Duration(interval*multiplier) * time.Second)
		}
		if r.controller != nil && !staleDisabled && r.authorizationStale(time.Now()) {
			if err := r.controller.Disable(ctx); err != nil {
				return errors.New("stale authorization disable failed; verify test core manually")
			}
			staleDisabled = true
			if err := r.Log.Event("stale_users_disabled"); err != nil {
				return err
			}
		}
		if r.Stats != nil && !time.Now().Before(nextQueue) {
			if err := r.ledger.Enqueue(filepath.Join(r.StateDir, "traffic.json")); err != nil {
				return r.abort(&StorageError{})
			}
			nextQueue = time.Now().Add(r.pushInterval())
		}
		deadline := nextSync
		if r.Stats != nil {
			if nextCollect.Before(deadline) {
				deadline = nextCollect
			}
			if nextQueue.Before(deadline) {
				deadline = nextQueue
			}
		}
		if r.controller != nil && !staleDisabled && !r.snapshot.LastSuccess.IsZero() {
			expiry := r.snapshot.LastSuccess.Add(time.Duration(r.Config.MaxStaleSeconds) * time.Second)
			if expiry.Before(deadline) {
				deadline = expiry
			}
		}
		delay := time.Until(deadline)
		if delay < time.Millisecond {
			delay = time.Millisecond
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return r.shutdown()
		case <-timer.C:
		}
	}
}

func (r *Runner) abort(cause error) error {
	if r.controller != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if r.controller.Fence(ctx) != nil {
			return errors.New("fatal state error; ledger retained and test inbound closure not verified")
		}
	}
	return cause
}
func (r *Runner) pushInterval() time.Duration {
	seconds := r.Config.Push
	if r.snapshot.Push > 0 {
		seconds = r.snapshot.Push
	}
	return time.Duration(seconds) * time.Second
}
func (r *Runner) authorizationStale(now time.Time) bool {
	last := r.snapshot.LastSuccess
	return last.IsZero() || last.After(now.Add(time.Second)) || !now.Before(last.Add(time.Duration(r.Config.MaxStaleSeconds)*time.Second))
}
func (r *Runner) Apply(ctx context.Context) error {
	if r.Config.Mode != "manage-test" || r.controller == nil {
		return nil
	}
	if r.authorizationStale(time.Now()) {
		return r.controller.Disable(ctx)
	}
	return r.controller.Apply(ctx, r.snapshot.Config, r.snapshot.Users)
}
func (r *Runner) shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if r.controller != nil {
		if err := r.controller.Disable(ctx); err != nil {
			return errors.New("test inbound shutdown could not be verified; ledger retained")
		}
	}
	if r.Stats != nil {
		if err := r.Collect(ctx); err != nil {
			return err
		}
		if err := r.ledger.Enqueue(filepath.Join(r.StateDir, "traffic.json")); err != nil {
			return &StorageError{}
		}
	}
	return nil
}
