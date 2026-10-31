package control

import (
	"context"
	"errors"
)

// Fence is best-effort fail-closed containment after a fatal checkpoint/control
// error. It never retries a ledger write or calls BeforeRemove: RAM may lag a
// rename that already succeeded. Existing durable ownership is still required.
// No global/core service is stopped; only the proven-owned test inbound closes.
func (c *Controller) Fence(ctx context.Context) error {
	epoch, err := c.Epoch()
	if err != nil {
		return err
	}
	hash, exists, err := c.Core.InspectInbound(ctx, c.state.Tag)
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if err := c.verifyOwnership(hash, epoch); err != nil {
		return err
	}
	_ = c.Core.RemoveInbound(ctx, c.state.Tag)
	_, exists, err = c.Core.InspectInbound(ctx, c.state.Tag)
	if err != nil || exists {
		return errors.New("fault containment could not verify inbound closure")
	}
	return nil
}
