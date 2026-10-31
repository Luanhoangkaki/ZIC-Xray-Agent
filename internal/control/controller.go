// Package control reconciles one dedicated inbound in an explicitly enabled
// loopback test environment. It never starts/stops a core or edits Xray files.
package control

import (
	"context"
	"errors"
	"net"
	"os"
	"strconv"
	"time"

	"zic-xray-agent/internal/durable"
	"zic-xray-agent/internal/panel"
)

type Core interface {
	PlanFingerprint(string, string, panel.VLESSSpec) (string, error)
	InspectInbound(context.Context, string) (string, bool, error)
	AddInbound(context.Context, string, string, panel.VLESSSpec) error
	RemoveInbound(context.Context, string) error
	ReconcileUsers(context.Context, string, int64, string, []panel.User, func() error) error
}
type Intent struct {
	From  string `json:"from"`
	To    string `json:"to"`
	Epoch string `json:"epoch"`
}
type Manifest struct {
	Version  int       `json:"version"`
	Identity string    `json:"identity"`
	Tag      string    `json:"tag"`
	Epoch    string    `json:"epoch"`
	Owned    bool      `json:"owned"`
	Applied  string    `json:"applied"`
	Pending  *Intent   `json:"pending,omitempty"`
	Phase    string    `json:"phase"`
	Updated  time.Time `json:"updated"`
}
type Controller struct {
	Core                     Core
	Path, Identity, ListenIP string
	NodeID                   int64
	Epoch                    func() (string, error)
	BeforeRemove             func(context.Context) error
	state                    Manifest
}

func (c *Controller) Load() error {
	if c.Core == nil || c.Epoch == nil || c.Identity == "" || c.NodeID <= 0 {
		return errors.New("incomplete controller configuration")
	}
	ip := net.ParseIP(c.ListenIP)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("test controller may listen only on loopback")
	}
	c.state = Manifest{Version: 1, Identity: c.Identity, Tag: "zic-node-" + strconv.FormatInt(c.NodeID, 10), Phase: "idle"}
	var stored Manifest
	if err := durable.Load(c.Path, &stored); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return errors.New("controller manifest corrupt; refusing to take ownership")
	}
	if stored.Version != 1 || stored.Identity != c.Identity || stored.Tag != c.state.Tag {
		return errors.New("controller identity mismatch")
	}
	if stored.Pending != nil && (stored.Pending.To == "" || stored.Pending.Epoch == "") {
		return errors.New("invalid controller intent")
	}
	c.state = stored
	return nil
}

type PersistenceError struct{}

func (*PersistenceError) Error() string {
	return "controller checkpoint failed; stop and reload before continuing"
}
func (c *Controller) save(next Manifest) error {
	next.Updated = time.Now().UTC()
	if err := durable.Save(c.Path, next); err != nil {
		return &PersistenceError{}
	}
	c.state = next
	return nil
}
func (c *Controller) verifyOwnership(hash, epoch string) error {
	if c.state.Owned && c.state.Epoch == epoch && hash == c.state.Applied {
		return nil
	}
	if p := c.state.Pending; p != nil && p.Epoch == epoch && (hash == p.To || (c.state.Owned && hash == p.From)) {
		return nil
	}
	return errors.New("unowned or externally changed inbound; refusing mutation")
}
func (c *Controller) checkpoint(ctx context.Context) error {
	if c.BeforeRemove == nil {
		return errors.New("traffic checkpoint callback required")
	}
	return c.BeforeRemove(ctx)
}

// Apply records intent BEFORE changing Xray, checks actual state after unknown
// mutation outcomes, and resumes interrupted work by inspection after restart.
// It does not roll back to revoked UUIDs after a failed update.
func (c *Controller) Apply(ctx context.Context, raw []byte, users []panel.User) error {
	spec, err := panel.ParseVLESS(raw)
	if err != nil {
		return c.rejectPolicy(ctx)
	}
	if err := panel.RequireSupportedPolicies(users); err != nil {
		return c.rejectPolicy(ctx)
	}
	if spec.Reality != nil {
		host, _, err := net.SplitHostPort(spec.Reality.Destination)
		if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
			return c.rejectPolicy(ctx)
		}
	}
	want, err := c.Core.PlanFingerprint(c.state.Tag, c.ListenIP, spec)
	if err != nil {
		return err
	}
	epoch, err := c.Epoch()
	if err != nil {
		return err
	}
	hash, exists, err := c.Core.InspectInbound(ctx, c.state.Tag)
	if err != nil {
		return err
	}
	if exists {
		if err := c.verifyOwnership(hash, epoch); err != nil {
			return err
		}
	}
	next := c.state
	if exists {
		next.Owned = true
		next.Applied = hash
		next.Epoch = epoch
	}
	next.Pending = &Intent{From: hash, To: want, Epoch: epoch}
	next.Phase = "applying"
	if err := c.save(next); err != nil {
		return err
	}
	if exists && hash != want {
		if err := c.checkpoint(ctx); err != nil {
			return err
		}
		removeErr := c.Core.RemoveInbound(ctx, c.state.Tag)
		hash, exists, err = c.Core.InspectInbound(ctx, c.state.Tag)
		if err != nil {
			return err
		}
		if exists {
			return errors.New("inbound removal not verified")
		}
		_ = removeErr // Inspection proved absent; never blindly replay Remove.
	}
	if !exists {
		addErr := c.Core.AddInbound(ctx, c.state.Tag, c.ListenIP, spec)
		hash, exists, err = c.Core.InspectInbound(ctx, c.state.Tag)
		if err != nil {
			return err
		}
		if !exists || hash != want {
			return errors.New("inbound creation not verified")
		}
		_ = addErr // Lost response is resolved by observed fingerprint.
	}
	if err := c.Core.ReconcileUsers(ctx, c.state.Tag, c.NodeID, spec.Flow, users, func() error { return c.checkpoint(ctx) }); err != nil {
		return err
	}
	after, err := c.Epoch()
	if err != nil || after != epoch {
		return errors.New("core restarted during reconciliation; inspect before resuming")
	}
	hash, exists, err = c.Core.InspectInbound(ctx, c.state.Tag)
	if err != nil || !exists || hash != want {
		return errors.New("final inbound state not verified")
	}
	next = c.state
	next.Owned = true
	next.Epoch = epoch
	next.Applied = want
	next.Pending = nil
	next.Phase = "applied"
	return c.save(next)
}
func (c *Controller) rejectPolicy(ctx context.Context) error {
	if err := c.Disable(ctx); err != nil {
		return err
	}
	return errors.New("unsupported policy; managed test inbound disabled")
}

// Disable gives access revocation priority. A failed Stats checkpoint is
// reported even if Remove succeeded; the caller must record an accounting gap.
func (c *Controller) Disable(ctx context.Context) error {
	epoch, err := c.Epoch()
	if err != nil {
		return err
	}
	hash, exists, err := c.Core.InspectInbound(ctx, c.state.Tag)
	if err != nil {
		return err
	}
	if exists {
		if err := c.verifyOwnership(hash, epoch); err != nil {
			return err
		}
		next := c.state
		next.Phase = "disabling"
		if err := c.save(next); err != nil {
			return err
		}
		checkpointErr := c.checkpoint(ctx)
		_ = c.Core.RemoveInbound(ctx, c.state.Tag)
		_, stillExists, verifyErr := c.Core.InspectInbound(ctx, c.state.Tag)
		if verifyErr != nil || stillExists {
			return errors.New("managed inbound disable outcome not verified")
		}
		if checkpointErr != nil {
			return errors.New("inbound disabled with an uncheckpointed traffic gap")
		}
	}
	next := c.state
	next.Phase = "disabled"
	next.Pending = nil
	return c.save(next)
}
