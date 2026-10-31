// Package traffic contains an opt-in loopback-only lab sender. The daemon has
// no send switch and no runtime reference to Sender; real traffic stays local.
package traffic

import (
	"context"
	"errors"

	"zic-xray-agent/internal/accounting"
	"zic-xray-agent/internal/panel"
)

type Sender struct {
	Ledger      *accounting.Ledger
	Path        string
	Panel       *panel.Client
	Credentials panel.Credentials
	LabEnabled  bool
}

// Caller must hold the same OS state-directory lock used by the daemon and
// serialize access to Ledger. No worker/goroutine or automatic retry is created.
func (s *Sender) SendNext(ctx context.Context) error {
	if !s.LabEnabled || s.Panel == nil || s.Ledger == nil || !panel.IsLoopbackPanel(s.Credentials.PanelURL) {
		return errors.New("real traffic sending is disabled")
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	batch, err := s.Ledger.BeginNext(s.Path)
	if err != nil {
		return err
	}
	outcome := s.Panel.PushTrafficLab(ctx, s.Credentials, batch.Payload)
	// If recording this result fails, durable SENDING is recovered to UNKNOWN
	// after restart, even when HTTP had returned data=true. Never replay it.
	if err := s.Ledger.FinishAttempt(s.Path, batch.ID, outcome == nil); err != nil {
		return err
	}
	return outcome
}
