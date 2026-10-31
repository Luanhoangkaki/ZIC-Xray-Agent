// Package accounting checkpoints non-resetting Xray counters. It does not submit
// traffic: the verified panel has no durable per-batch receipt/deduplication.
package accounting

import (
	"errors"
	"fmt"
	"math"
	"os"
	"time"

	"zic-xray-agent/internal/durable"
)

const MaxCounters = 20000

var ErrObservation = errors.New("counter observation requires reconciliation")

func observationError(reason string) error { return fmt.Errorf("%w: %s", ErrObservation, reason) }

type Counter struct {
	UserID    int64  `json:"user_id"`
	Direction string `json:"direction"`
	Value     uint64 `json:"value"`
	Pending   uint64 `json:"pending"`
}

type Ledger struct {
	Version  int                `json:"version"`
	Identity string             `json:"identity"`
	Epoch    string             `json:"epoch"`
	Counters map[string]Counter `json:"counters"`
	// The initial observation is a baseline, not a claim about prior traffic.
	BaselineOnly bool       `json:"initial_sample_is_baseline"`
	LastObserved time.Time  `json:"last_observed,omitempty"`
	NextSequence uint64     `json:"next_sequence,omitempty"`
	Queue        []Batch    `json:"queue,omitempty"`
	Incidents    []Incident `json:"incidents,omitempty"`
}

func Open(path, identity string) (*Ledger, error) {
	return read(path, identity, true)
}

// Inspect reads an atomic checkpoint without changing SENDING, taking a lock,
// or recovering/migrating anything on disk. It is safe while an observer runs.
func Inspect(path string) (*Ledger, error) { return read(path, "", false) }
func read(path, identity string, recoverInterrupted bool) (*Ledger, error) {
	l := &Ledger{}
	if err := durable.Load(path, l); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if identity == "" {
				return nil, err
			}
			return &Ledger{Version: 2, Identity: identity, Counters: make(map[string]Counter), BaselineOnly: true}, nil
		}
		return nil, err
	}
	if identity == "" {
		identity = l.Identity
	}
	if identity == "" {
		return nil, errors.New("empty ledger identity")
	}
	if (l.Version != 1 && l.Version != 2) || l.Identity != identity || len(l.Counters) > MaxCounters || !l.BaselineOnly || (l.Epoch == "" && len(l.Counters) > 0) {
		return nil, errors.New("ledger identity/version invalid")
	}
	for _, c := range l.Counters {
		if c.UserID <= 0 || (c.Direction != "upload" && c.Direction != "download") || c.Value > math.MaxInt64 || c.Pending > math.MaxInt64 {
			return nil, errors.New("invalid ledger counter")
		}
	}
	if l.Counters == nil {
		l.Counters = make(map[string]Counter)
	}
	if err := l.validateQueue(); err != nil {
		return nil, err
	}
	l.Version = 2 // v1 pending/baselines are preserved; migration writes on next commit.
	if recoverInterrupted {
		if err := l.RecoverInterrupted(path); err != nil {
			return nil, err
		}
	}
	return l, nil
}

// Observe commits all baselines and pending deltas together before changing RAM.
// A core epoch change, vanished counter or decrease requires reconciliation;
// none is silently treated as zero. Caller must stop on checkpoint write error.
func (l *Ledger) Observe(path, epoch string, sample map[string]Counter) error {
	if epoch == "" || (l.Epoch != "" && l.Epoch != epoch) {
		return observationError("Xray epoch changed; traffic gap requires reconciliation")
	}
	if len(sample) > MaxCounters {
		return observationError("counter limit exceeded")
	}
	for key := range l.Counters {
		if _, ok := sample[key]; !ok {
			return observationError("Xray counter disappeared; reconciliation required")
		}
	}
	next := *l
	next.LastObserved = time.Now().UTC()
	next.Epoch = epoch
	next.Counters = make(map[string]Counter, len(sample))
	for key, current := range sample {
		if current.UserID <= 0 || current.Value > math.MaxInt64 || current.Pending != 0 || (current.Direction != "upload" && current.Direction != "download") {
			return observationError("invalid observed counter")
		}
		old, exists := l.Counters[key]
		if exists && (old.UserID != current.UserID || old.Direction != current.Direction || current.Value < old.Value) {
			return observationError("Xray counter reset/identity changed; reconciliation required")
		}
		if l.Epoch == "" {
			current.Pending = 0
		} else {
			// New counters within a known epoch start at zero. The daemon owns the
			// managed email namespace, so unrelated/historical counters are excluded.
			delta := current.Value - old.Value
			if old.Pending > math.MaxInt64-delta {
				return observationError("pending traffic overflow")
			}
			current.Pending = old.Pending + delta
		}
		next.Counters[key] = current
	}
	return l.commit(path, &next)
}

func (l *Ledger) commit(path string, next *Ledger) error {
	if err := durable.Save(path, next); err != nil {
		return err
	}
	*l = *next
	return nil
}

// TrafficPayload returns the verified V2B-ZIC wire shape, without applying panel
// rate. This is an export only, not a send/ack operation or a safe retry token.
func (l *Ledger) TrafficPayload() (map[int64][2]uint64, error) {
	out := make(map[int64][2]uint64)
	for _, c := range l.Counters {
		if c.Pending == 0 {
			continue
		}
		value := out[c.UserID]
		i := 0
		if c.Direction == "download" {
			i = 1
		}
		if value[i] > math.MaxInt64-c.Pending {
			return nil, errors.New("traffic total overflow")
		}
		value[i] += c.Pending
		out[c.UserID] = value
	}
	return out, nil
}
