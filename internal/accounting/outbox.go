package accounting

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
)

const MaxBatches = 256
const MaxBatchUsers = 5000
const MaxIncidents = 64

type BatchState string

const (
	Pending             BatchState = "PENDING"
	Sending             BatchState = "SENDING"
	Unknown             BatchState = "UNKNOWN"
	Accepted            BatchState = "ACCEPTED" // HTTP data=true only; NOT proof of durable billing.
	ConfirmedApplied    BatchState = "CONFIRMED_APPLIED"
	ConfirmedNotApplied BatchState = "CONFIRMED_NOT_APPLIED"
)

type Batch struct {
	ID         string              `json:"id"`
	Sequence   uint64              `json:"sequence"`
	Epoch      string              `json:"epoch"`
	Digest     string              `json:"digest"`
	State      BatchState          `json:"state"`
	Attempts   int                 `json:"attempts"`
	Created    time.Time           `json:"created"`
	Updated    time.Time           `json:"updated"`
	Payload    map[int64][2]uint64 `json:"payload"`
	Resolution string              `json:"resolution,omitempty"`
}
type Incident struct {
	ID            string    `json:"id"`
	Reason        string    `json:"reason"`
	PreviousEpoch string    `json:"previous_epoch"`
	NewEpoch      string    `json:"new_epoch"`
	LastObserved  time.Time `json:"last_observed"`
	Detected      time.Time `json:"detected"`
	Resolved      bool      `json:"resolved"`
	Evidence      string    `json:"evidence,omitempty"`
}

func payloadDigest(payload map[int64][2]uint64) (string, error) {
	if len(payload) == 0 || len(payload) > MaxBatchUsers {
		return "", errors.New("invalid batch size")
	}
	for uid, v := range payload {
		if uid <= 0 || v == [2]uint64{} || v[0] > math.MaxInt64 || v[1] > math.MaxInt64 {
			return "", errors.New("invalid batch traffic")
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func (l *Ledger) validateQueue() error {
	if len(l.Queue) > MaxBatches || len(l.Incidents) > MaxIncidents {
		return errors.New("queue/incident limit exceeded")
	}
	seen := make(map[string]bool)
	last := uint64(0)
	for _, b := range l.Queue {
		digest, err := payloadDigest(b.Payload)
		identity := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", l.Identity, b.Sequence)))
		if err != nil || digest != b.Digest || b.ID != hex.EncodeToString(identity[:]) || seen[b.ID] || b.Sequence <= last || b.Sequence > l.NextSequence || b.Epoch == "" {
			return errors.New("invalid batch identity/payload/checksum")
		}
		seen[b.ID] = true
		last = b.Sequence
		switch b.State {
		case Pending:
			if b.Attempts != 0 {
				return errors.New("pending batch was already attempted")
			}
		case Sending, Unknown, Accepted:
			if b.Attempts != 1 {
				return errors.New("invalid batch attempts")
			}
		case ConfirmedApplied, ConfirmedNotApplied:
			if b.Attempts != 1 || !validEvidence(b.Resolution) {
				return errors.New("missing manual reconciliation evidence")
			}
		default:
			return errors.New("invalid batch state")
		}
	}
	for _, i := range l.Incidents {
		if i.ID == "" || i.Reason == "" || (i.Resolved && !validEvidence(i.Evidence)) {
			return errors.New("invalid incident")
		}
	}
	return nil
}
func clonePayload(p map[int64][2]uint64) map[int64][2]uint64 {
	out := make(map[int64][2]uint64, len(p))
	for k, v := range p {
		out[k] = v
	}
	return out
}
func (l *Ledger) clone() *Ledger {
	n := *l
	n.Counters = make(map[string]Counter, len(l.Counters))
	for k, v := range l.Counters {
		n.Counters[k] = v
	}
	n.Queue = append([]Batch(nil), l.Queue...)
	n.Incidents = append([]Incident(nil), l.Incidents...)
	return &n
}

// Enqueue moves pending deltas and their baselines in ONE durable checkpoint.
// An unattempted tail may coalesce for bounded offline RAM/disk usage; its
// payload freezes at SENDING. Attempted/UNKNOWN payloads are never modified.
func (l *Ledger) Enqueue(path string) error {
	n := l.clone()
	changed, err := n.enqueuePending()
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return l.commit(path, n)
}
func (l *Ledger) enqueuePending() (bool, error) {
	payload, err := l.TrafficPayload()
	if err != nil {
		return false, err
	}
	if len(payload) == 0 {
		return false, nil
	}
	ids := make([]int64, 0, len(payload))
	for id := range payload {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for offset := 0; offset < len(ids); {
		var target *Batch
		if len(l.Queue) > 0 {
			tail := &l.Queue[len(l.Queue)-1]
			if tail.State == Pending && tail.Attempts == 0 && tail.Epoch == l.Epoch {
				tail.Payload = clonePayload(tail.Payload)
				target = tail
			}
		}
		if target == nil {
			if len(l.Queue) >= MaxBatches || l.NextSequence == math.MaxUint64 {
				return false, errors.New("outbox capacity exhausted; no pending traffic discarded")
			}
			l.NextSequence++
			now := time.Now().UTC()
			sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", l.Identity, l.NextSequence)))
			l.Queue = append(l.Queue, Batch{ID: hex.EncodeToString(sum[:]), Sequence: l.NextSequence, Epoch: l.Epoch, State: Pending, Created: now, Payload: make(map[int64][2]uint64)})
			target = &l.Queue[len(l.Queue)-1]
		}
		start := offset
		for offset < len(ids) {
			uid := ids[offset]
			old, exists := target.Payload[uid]
			if !exists && len(target.Payload) >= MaxBatchUsers {
				break
			}
			incoming := payload[uid]
			for i := 0; i < 2; i++ {
				if old[i] > math.MaxInt64-incoming[i] {
					return false, errors.New("batch counter overflow")
				}
				old[i] += incoming[i]
			}
			target.Payload[uid] = old
			offset++
		}
		// A full unsent tail gets sealed as-is; force another pending batch.
		if offset == start {
			if len(l.Queue) >= MaxBatches || l.NextSequence == math.MaxUint64 {
				return false, errors.New("outbox capacity exhausted")
			}
			l.NextSequence++
			sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%d", l.Identity, l.NextSequence)))
			l.Queue = append(l.Queue, Batch{ID: hex.EncodeToString(sum[:]), Sequence: l.NextSequence, Epoch: l.Epoch, State: Pending, Created: time.Now().UTC(), Payload: map[int64][2]uint64{ids[offset]: payload[ids[offset]]}})
			target = &l.Queue[len(l.Queue)-1]
			offset++
		}
		target.Updated = time.Now().UTC()
		target.Digest, err = payloadDigest(target.Payload)
		if err != nil {
			return false, err
		}
	}
	for k, c := range l.Counters {
		c.Pending = 0
		l.Counters[k] = c
	}
	return true, nil
}

// ResolveIncident acknowledges a known data gap; it does not manufacture bytes
// or assert that missing traffic has been recovered.
func (l *Ledger) ResolveIncident(path, id, evidence string) error {
	if !validEvidence(evidence) {
		return errors.New("manual incident evidence required")
	}
	n := l.clone()
	for i, incident := range n.Incidents {
		if incident.ID == id {
			if incident.Resolved {
				return errors.New("incident already acknowledged")
			}
			n.Incidents[i].Resolved = true
			n.Incidents[i].Evidence = evidence
			return l.commit(path, n)
		}
	}
	return errors.New("incident not found")
}

func (l *Ledger) RecoverInterrupted(path string) error {
	n := l.clone()
	changed := false
	for i := range n.Queue {
		if n.Queue[i].State == Sending {
			n.Queue[i].State = Unknown
			n.Queue[i].Updated = time.Now().UTC()
			changed = true
		}
	}
	if !changed {
		return nil
	}
	return l.commit(path, n)
}

// BeginNext must complete its fsync before the caller touches the network.
// Every unknown/unfinished attempt blocks all later automatic attempts.
func (l *Ledger) BeginNext(path string) (Batch, error) {
	for _, incident := range l.Incidents {
		if !incident.Resolved {
			return Batch{}, errors.New("unresolved accounting incident blocks sending")
		}
	}
	for _, b := range l.Queue {
		if b.State == Unknown || b.State == Sending {
			return Batch{}, errors.New("UNKNOWN/in-progress traffic requires manual reconciliation")
		}
	}
	for i, b := range l.Queue {
		if b.State == Pending {
			n := l.clone()
			n.Queue[i].State = Sending
			n.Queue[i].Attempts = 1
			n.Queue[i].Updated = time.Now().UTC()
			if err := l.commit(path, n); err != nil {
				return Batch{}, err
			}
			b = l.Queue[i]
			b.Payload = clonePayload(b.Payload)
			return b, nil
		}
	}
	return Batch{}, errors.New("no pending batch")
}

func (l *Ledger) FinishAttempt(path, id string, accepted bool) error {
	n := l.clone()
	for i, b := range n.Queue {
		if b.ID == id {
			if b.State != Sending || b.Attempts != 1 {
				return errors.New("batch is not in its only attempt")
			}
			n.Queue[i].State = Unknown
			if accepted {
				n.Queue[i].State = Accepted
			}
			n.Queue[i].Updated = time.Now().UTC()
			return l.commit(path, n)
		}
	}
	return errors.New("batch not found")
}
func validEvidence(s string) bool {
	return len(strings.TrimSpace(s)) >= 3 && len(s) <= 256 && !strings.ContainsAny(s, "\r\n\x00")
}

func (l *Ledger) ResolveUnknown(path, id, digest string, applied bool, evidence string) error {
	if !validEvidence(evidence) {
		return errors.New("manual evidence reference required (3..256 bytes, one line)")
	}
	n := l.clone()
	for i, b := range n.Queue {
		if b.ID == id {
			if b.State != Unknown || b.Digest != digest {
				return errors.New("batch is not UNKNOWN or digest changed")
			}
			n.Queue[i].State = ConfirmedNotApplied
			if applied {
				n.Queue[i].State = ConfirmedApplied
			}
			n.Queue[i].Resolution = evidence
			n.Queue[i].Updated = time.Now().UTC()
			// A not-applied decision does NOT requeue/retry. Export for a separately
			// authorized manual workflow; never silently convert UNKNOWN to PENDING.
			return l.commit(path, n)
		}
	}
	return errors.New("batch not found")
}

func (l *Ledger) RecordIncident(path, reason, newEpoch string) error {
	for _, i := range l.Incidents {
		if !i.Resolved && i.Reason == reason && i.NewEpoch == newEpoch {
			return nil
		}
	}
	n := l.clone()
	if err := n.addIncident(reason, newEpoch); err != nil {
		return err
	}
	return l.commit(path, n)
}
func (l *Ledger) addIncident(reason, newEpoch string) error {
	if len(l.Incidents) >= MaxIncidents {
		return errors.New("incident capacity exhausted; manual reconciliation required")
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d", l.Identity, l.Epoch, newEpoch, len(l.Incidents))))
	l.Incidents = append(l.Incidents, Incident{ID: hex.EncodeToString(sum[:]), Reason: reason, PreviousEpoch: l.Epoch, NewEpoch: newEpoch, LastObserved: l.LastObserved, Detected: time.Now().UTC()})
	return nil
}

// TransitionEpoch continues collecting a proven new core process after reboot.
// Unobserved bytes of the old process remain an explicit OPEN incident. Known
// old pending traffic is queued atomically; new process counters start at zero.
func (l *Ledger) TransitionEpoch(path, epoch string, sample map[string]Counter) error {
	if l.Epoch == "" || epoch == "" || epoch == l.Epoch || len(sample) > MaxCounters {
		return errors.New("invalid core epoch transition")
	}
	n := l.clone()
	if _, err := n.enqueuePending(); err != nil {
		return err
	}
	if err := n.addIncident("core_epoch_changed", epoch); err != nil {
		return err
	}
	n.Epoch = epoch
	n.LastObserved = time.Now().UTC()
	n.Counters = make(map[string]Counter, len(sample))
	for k, c := range sample {
		if c.UserID <= 0 || c.Value > math.MaxInt64 || c.Pending != 0 || (c.Direction != "upload" && c.Direction != "download") {
			return errors.New("invalid new-core counter")
		}
		c.Pending = c.Value
		n.Counters[k] = c
	}
	return l.commit(path, n)
}
