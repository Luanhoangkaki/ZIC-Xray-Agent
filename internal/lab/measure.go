package lab

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/proxy"

	"zic-xray-agent/internal/accounting"
	"zic-xray-agent/internal/durable"
	"zic-xray-agent/internal/xrayapi"
)

type Bytes struct {
	Upload   uint64 `json:"upload"`
	Download uint64 `json:"download"`
}
type UUIDResult struct {
	UUID     string `json:"uuid"`
	UserID   int64  `json:"synthetic_user_id"`
	Expected Bytes  `json:"expected"`
	Stats    Bytes  `json:"stats_delta"`
	Exact    bool   `json:"exact"`
}
type MeasureOptions struct {
	Connections        int
	Duration, Interval time.Duration
	Chunk              int
	AgentPID, CorePID  int
	Output, Collector  string
	FunctionalOnly     bool
}
type Summary struct {
	Synthetic         bool         `json:"synthetic"`
	ProductionReady   bool         `json:"production_ready"`
	SendingEnabled    bool         `json:"sending_enabled"`
	LabID             string       `json:"lab_id"`
	Workload          string       `json:"workload"`
	Connections       int          `json:"connections"`
	DurationSeconds   float64      `json:"duration_seconds"`
	ResourcesMeasured bool         `json:"resources_measured"`
	Errors            uint64       `json:"connection_errors"`
	UUIDs             []UUIDResult `json:"per_uuid"`
	Passed            bool         `json:"passed"`
}

func Stats(ctx context.Context, c Config) (map[int64]Bytes, error) {
	client, err := xrayapi.Connect(c.Address(1))
	if err != nil {
		return nil, err
	}
	defer client.Close()
	counters, err := client.Counters(ctx, NodeID)
	if err != nil {
		return nil, err
	}
	out := map[int64]Bytes{}
	for _, counter := range counters {
		value := out[counter.UserID]
		if counter.Direction == "upload" {
			value.Upload += counter.Value
		} else {
			value.Download += counter.Value
		}
		out[counter.UserID] = value
	}
	return out, nil
}
func difference(current, previous Bytes) (Bytes, error) {
	if current.Upload < previous.Upload || current.Download < previous.Download {
		return Bytes{}, errors.New("counter reset during phase")
	}
	return Bytes{current.Upload - previous.Upload, current.Download - previous.Download}, nil
}

func Measure(ctx context.Context, c Config, o MeasureOptions) (Summary, error) {
	s := Summary{Synthetic: true, LabID: c.LabID, Workload: "loopback TCP VLESS echo; no REALITY encryption benchmark", Connections: o.Connections, DurationSeconds: o.Duration.Seconds(), UUIDs: []UUIDResult{}}
	if err := c.Validate(); err != nil {
		return s, err
	}
	if (o.Connections != 0 && o.Connections != 10 && o.Connections != 20 && o.Connections != 30) || o.Duration < time.Second || o.Duration > 24*time.Hour || o.Interval < 10*time.Millisecond || o.Chunk < 1 || o.Chunk > 64<<10 {
		return s, errors.New("invalid bounded lab workload")
	}
	if err := durable.PrivateDir(o.Output); err != nil {
		return s, err
	}
	before, err := Stats(ctx, c)
	if err != nil {
		return s, err
	}
	var collector *exec.Cmd
	var resourceFile, errorFile *os.File
	if !o.FunctionalOnly {
		if o.AgentPID <= 0 || o.CorePID <= 0 || o.Collector == "" {
			return s, errors.New("separate agent/core PIDs and collector required")
		}
		resourceFile, err = os.OpenFile(filepath.Join(o.Output, "resources.csv"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return s, err
		}
		defer resourceFile.Close()
		errorFile, err = os.OpenFile(filepath.Join(o.Output, "collector-error.txt"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return s, err
		}
		defer errorFile.Close()
		collector = exec.CommandContext(ctx, o.Collector, "-agent-pid", strconv.Itoa(o.AgentPID), "-xray-pid", strconv.Itoa(o.CorePID), "-load-pid", strconv.Itoa(os.Getpid()), "-duration", o.Duration.String(), "-interval", "1s")
		collector.Stdout = resourceFile
		collector.Stderr = errorFile
		if err := collector.Start(); err != nil {
			return s, errors.New("cannot start resource collector")
		}
	}
	phase, cancel := context.WithTimeout(ctx, o.Duration)
	defer cancel()
	var sent, received [2]atomic.Uint64
	var failures atomic.Uint64
	var group sync.WaitGroup
	for worker := 0; worker < o.Connections; worker++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			user := index % 2
			dialer, err := proxy.SOCKS5("tcp", c.Address(3+user), nil, &net.Dialer{Timeout: 5 * time.Second})
			if err != nil {
				failures.Add(1)
				return
			}
			contextDialer, ok := dialer.(proxy.ContextDialer)
			if !ok {
				failures.Add(1)
				return
			}
			conn, err := contextDialer.DialContext(phase, "tcp", c.Address(2))
			if err != nil {
				failures.Add(1)
				return
			}
			defer conn.Close()
			payload := bytes.Repeat([]byte{byte(index + 1)}, o.Chunk)
			reply := make([]byte, o.Chunk)
			ticker := time.NewTicker(o.Interval)
			defer ticker.Stop()
			for {
				select {
				case <-phase.Done():
					return
				case <-ticker.C:
				}
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				n, err := conn.Write(payload)
				sent[user].Add(uint64(n))
				if err != nil || n != len(payload) {
					failures.Add(1)
					return
				}
				n, err = io.ReadFull(conn, reply)
				received[user].Add(uint64(n))
				if err != nil || !bytes.Equal(reply, payload) {
					failures.Add(1)
					return
				}
			}
		}(worker)
	}
	if o.Connections == 0 {
		<-phase.Done()
	}
	group.Wait()
	if collector != nil {
		if err := collector.Wait(); err != nil {
			_ = writeJSON(filepath.Join(o.Output, "summary.json"), s)
			return s, errors.New("collector failed; phase is not a resource-measurement PASS")
		}
		s.ResourcesMeasured = true
		if err := summarizeCSV(filepath.Join(o.Output, "resources.csv"), filepath.Join(o.Output, "resources-summary.json")); err != nil {
			return s, err
		}
	}
	// All echo replies have drained. Give official Stats a bounded settle window.
	until := time.Now().Add(5 * time.Second)
	var after map[int64]Bytes
	for {
		after, err = Stats(ctx, c)
		if err != nil {
			return s, err
		}
		matched := true
		for i, u := range c.Users {
			delta, e := difference(after[u.ID], before[u.ID])
			if e != nil {
				return s, e
			}
			if delta != (Bytes{sent[i].Load(), received[i].Load()}) {
				matched = false
			}
		}
		if matched || time.Now().After(until) {
			break
		}
		select {
		case <-ctx.Done():
			return s, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	s.Errors = failures.Load()
	s.Passed = s.Errors == 0
	for i, u := range c.Users {
		delta, err := difference(after[u.ID], before[u.ID])
		if err != nil {
			return s, err
		}
		expected := Bytes{sent[i].Load(), received[i].Load()}
		exact := delta == expected
		s.UUIDs = append(s.UUIDs, UUIDResult{u.UUID, u.ID, expected, delta, exact})
		s.Passed = s.Passed && exact
	}
	if o.Connections > 0 && sent[0].Load()+sent[1].Load() == 0 {
		s.Passed = false
	}
	if err := writeJSON(filepath.Join(o.Output, "summary.json"), s); err != nil {
		return s, err
	}
	if !s.Passed {
		return s, errors.New("load/UUID accounting failed; inspect summary.json")
	}
	return s, nil
}

// Percentile storage is bounded by the CLI limit of 24h at >=1s/sample.
func summarizeCSV(path, out string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	reader := csv.NewReader(f)
	header, err := reader.Read()
	if err != nil {
		return err
	}
	type metric struct {
		Mean    float64 `json:"mean"`
		P95     float64 `json:"p95"`
		Peak    float64 `json:"peak"`
		Samples uint64  `json:"samples"`
	}
	values := map[string]metric{}
	samples := map[string][]float64{}
	for {
		row, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		for i := 2; i < len(row); i++ {
			v, err := strconv.ParseFloat(row[i], 64)
			if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
				return errors.New("invalid resource CSV")
			}
			m := values[header[i]]
			if m.Samples >= 86400 {
				return errors.New("resource CSV exceeds bounded 24h sample count")
			}
			samples[header[i]] = append(samples[header[i]], v)
			m.Samples++
			m.Mean += (v - m.Mean) / float64(m.Samples)
			if v > m.Peak {
				m.Peak = v
			}
			values[header[i]] = m
		}
	}
	if len(values) == 0 {
		return errors.New("resource CSV contains no samples")
	}
	for key, observations := range samples {
		sort.Float64s(observations)
		m := values[key]
		m.P95 = observations[int(math.Ceil(float64(len(observations))*0.95))-1]
		values[key] = m
	}
	return writeJSON(out, map[string]any{"units": "RSS=bytes; process CPU percent=one vCPU; system busy includes steal", "metrics": values})
}

func LedgerTotals(path string) (map[int64]Bytes, *accounting.Ledger, error) {
	l, err := accounting.Inspect(path)
	if err != nil {
		return nil, nil, err
	}
	totals := map[int64]Bytes{}
	add := func(uid int64, u, d uint64) error {
		v := totals[uid]
		if v.Upload > math.MaxInt64-u || v.Download > math.MaxInt64-d {
			return errors.New("ledger total overflow")
		}
		v.Upload += u
		v.Download += d
		totals[uid] = v
		return nil
	}
	for _, counter := range l.Counters {
		u, d := uint64(0), uint64(0)
		if counter.Direction == "upload" {
			u = counter.Pending
		} else {
			d = counter.Pending
		}
		if err := add(counter.UserID, u, d); err != nil {
			return nil, nil, err
		}
	}
	for _, batch := range l.Queue {
		if batch.State != accounting.Pending {
			return nil, nil, errors.New("lab queue contains an attempted batch; real sending must remain disabled")
		}
		for uid, v := range batch.Payload {
			if err := add(uid, v[0], v[1]); err != nil {
				return nil, nil, err
			}
		}
	}
	return totals, l, nil
}

func safeError(stage string) error {
	return fmt.Errorf("isolated lab %s failed; inspect private lab results/logs", stage)
}
