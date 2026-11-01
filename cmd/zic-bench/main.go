// zic-bench is an optional Linux measurement tool, never an agent subprocess.
package main

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"zic-xray-agent/internal/logging"
)

type process struct {
	ticks, start        uint64
	rss                 uint64
	writeBytes, fdCount uint64
}
type sample struct {
	at                                       time.Time
	processes                                [4]process
	total, idle, memory, available, swapUsed uint64
	hasLoad                                  bool
	hasDetails                               bool
}

func parseWriteBytes(data string) (uint64, error) {
	for _, line := range strings.Split(data, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "write_bytes:" {
			return strconv.ParseUint(fields[1], 10, 64)
		}
	}
	return 0, errors.New("missing process IO counter")
}

func countFDs(pid int) (uint64, error) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/fd", pid))
	if err != nil {
		return 0, errors.New("process FD counters unavailable; check permissions")
	}
	defer f.Close()
	var count uint64
	for {
		entries, err := f.ReadDir(128)
		count += uint64(len(entries))
		if count > 16384 {
			return 0, errors.New("FD sampling bound exceeded")
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return 0, errors.New("cannot read FD counters")
		}
	}
	if pid == os.Getpid() && count > 0 {
		count--
	} // Exclude this sampling directory FD.
	return count, nil
}

func parseProcess(data string, pageSize uint64) (process, error) {
	end := strings.LastIndex(data, ")")
	if end < 0 {
		return process{}, errors.New("invalid proc stat")
	}
	fields := strings.Fields(data[end+1:])
	if len(fields) < 22 {
		return process{}, errors.New("short proc stat")
	}
	var values [4]uint64
	for i, index := range []int{11, 12, 19, 21} {
		value, err := strconv.ParseUint(fields[index], 10, 64)
		if err != nil {
			return process{}, errors.New("invalid process counter")
		}
		values[i] = value
	}
	return process{ticks: values[0] + values[1], start: values[2], rss: values[3] * pageSize}, nil
}

func collect(pids [4]int) (sample, error) {
	return collectDetails(pids, false)
}

func collectDetails(pids [4]int, details bool) (sample, error) {
	var s sample
	s.hasLoad = pids[3] > 0
	s.hasDetails = details
	for i, pid := range pids {
		if i == 3 && pid == 0 {
			continue
		}
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
		if err != nil {
			return s, fmt.Errorf("PID %d unavailable; restart collector after process restart", pid)
		}
		p, err := parseProcess(string(data), uint64(os.Getpagesize()))
		if err != nil {
			return s, err
		}
		s.processes[i] = p
		if details {
			info, err := os.ReadFile(fmt.Sprintf("/proc/%d/io", pid))
			if err != nil {
				return s, errors.New("process IO counters unavailable; check permissions")
			}
			written, err := parseWriteBytes(string(info))
			if err != nil {
				return s, err
			}
			fds, err := countFDs(pid)
			if err != nil {
				return s, err
			}
			// Reject PID reuse across the detail reads, not just across rows.
			again, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
			if err != nil {
				return s, errors.New("process identity unavailable")
			}
			verified, err := parseProcess(string(again), uint64(os.Getpagesize()))
			if err != nil || verified.start != p.start {
				return s, errors.New("process identity changed during detail sample")
			}
			s.processes[i].writeBytes = written
			s.processes[i].fdCount = fds
		}
	}
	stat, err := os.ReadFile("/proc/stat")
	if err != nil {
		return s, err
	}
	fields := strings.Fields(strings.SplitN(string(stat), "\n", 2)[0])
	if len(fields) < 9 || fields[0] != "cpu" {
		return s, errors.New("invalid CPU stat")
	}
	// First eight fields include steal and exclude duplicated guest counters.
	for i := 1; i <= 8; i++ {
		n, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			return s, err
		}
		s.total += n
		if i == 4 || i == 5 {
			s.idle += n
		}
	}
	info, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return s, err
	}
	mem := make(map[string]uint64, 4)
	for _, line := range strings.Split(string(info), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "MemTotal:", "MemAvailable:", "SwapTotal:", "SwapFree:":
			n, err := strconv.ParseUint(f[1], 10, 64)
			if err != nil {
				return s, err
			}
			mem[f[0]] = n * 1024
		}
	}
	available, exists := mem["MemAvailable:"]
	if !exists || mem["MemTotal:"] == 0 || available > mem["MemTotal:"] || mem["SwapFree:"] > mem["SwapTotal:"] {
		return s, errors.New("invalid memory counters")
	}
	s.memory, s.available, s.swapUsed = mem["MemTotal:"], available, mem["SwapTotal:"]-mem["SwapFree:"]
	s.at = time.Now()
	return s, nil
}

func row(previous, current sample, hz float64) ([]string, error) {
	elapsed := current.at.Sub(previous.at).Seconds()
	if elapsed <= 0 || current.total <= previous.total || current.idle < previous.idle {
		return nil, errors.New("invalid sample interval")
	}
	values := []string{current.at.UTC().Format(time.RFC3339Nano), fmt.Sprintf("%.6f", elapsed)}
	for i, p := range current.processes[:3] {
		old := previous.processes[i]
		if p.start != old.start || p.ticks < old.ticks {
			return nil, errors.New("PID identity/counter changed; stop rather than merge different processes")
		}
		cpu := float64(p.ticks-old.ticks) / hz / elapsed * 100
		values = append(values, fmt.Sprintf("%.4f", cpu), strconv.FormatUint(p.rss, 10))
	}
	totalDelta, idleDelta := current.total-previous.total, current.idle-previous.idle
	if idleDelta > totalDelta {
		return nil, errors.New("inconsistent system CPU counters")
	}
	values = append(values, fmt.Sprintf("%.4f", 100*float64(totalDelta-idleDelta)/float64(totalDelta)), strconv.FormatUint(current.memory-current.available, 10), strconv.FormatUint(current.available, 10), strconv.FormatUint(current.swapUsed, 10))
	if previous.hasLoad != current.hasLoad {
		return nil, errors.New("load process schema changed")
	}
	if current.hasLoad {
		p, old := current.processes[3], previous.processes[3]
		if p.start != old.start || p.ticks < old.ticks {
			return nil, errors.New("load process identity/counter changed")
		}
		values = append(values, fmt.Sprintf("%.4f", float64(p.ticks-old.ticks)/hz/elapsed*100), strconv.FormatUint(p.rss, 10))
	}
	if current.hasDetails != previous.hasDetails {
		return nil, errors.New("resource detail schema changed")
	}
	if current.hasDetails {
		count := 3
		if current.hasLoad {
			count = 4
		}
		for i := 0; i < count; i++ {
			p, old := current.processes[i], previous.processes[i]
			if p.writeBytes < old.writeBytes {
				return nil, errors.New("process IO counter reset")
			}
			values = append(values, strconv.FormatUint(p.writeBytes, 10), strconv.FormatUint(p.writeBytes-old.writeBytes, 10), strconv.FormatUint(p.fdCount, 10))
		}
	}
	return values, nil
}

func run() error {
	flag.CommandLine.SetOutput(io.Discard)
	agent := flag.Int("agent-pid", 0, "agent PID")
	xray := flag.Int("xray-pid", 0, "official Xray PID")
	load := flag.Int("load-pid", 0, "optional load/mock controller PID measured separately")
	duration := flag.Duration("duration", 30*time.Minute, "measurement duration, at most 24h")
	interval := flag.Duration("interval", time.Second, "sampling interval, at least 1s")
	details := flag.Bool("io-fd", false, "read-only kernel write_bytes and FD counts; no FD targets; NOT physical SSD wear")
	flag.Parse()
	if runtime.GOOS != "linux" {
		return errors.New("collector requires Linux /proc")
	}
	if *agent <= 0 || *xray <= 0 || *agent == *xray || *load < 0 || (*load > 0 && (*load == *agent || *load == *xray)) || *interval < time.Second || *duration < *interval || *duration > 24*time.Hour {
		return errors.New("require distinct positive PIDs, interval >=1s and interval <= duration <=24h")
	}
	// Resolve the kernel tick unit once. Do not assume CLK_TCK is 100.
	hzText, err := exec.Command("getconf", "CLK_TCK").Output()
	if err != nil {
		return errors.New("getconf CLK_TCK failed")
	}
	hz, err := strconv.ParseFloat(strings.TrimSpace(string(hzText)), 64)
	if err != nil || hz <= 0 {
		return errors.New("invalid CLK_TCK")
	}
	pids := [4]int{*agent, *xray, os.Getpid(), *load}
	previous, err := collectDetails(pids, *details)
	if err != nil {
		return err
	}
	w := csv.NewWriter(os.Stdout)
	header := []string{"utc", "elapsed_seconds", "agent_cpu_one_core_percent", "agent_rss_bytes", "xray_cpu_one_core_percent", "xray_rss_bytes", "collector_cpu_one_core_percent", "collector_rss_bytes", "system_busy_percent", "system_memory_used_bytes", "system_memory_available_bytes", "system_swap_used_bytes"}
	if *load > 0 {
		header = append(header, "load_cpu_one_core_percent", "load_rss_bytes")
	}
	if *details {
		names := []string{"agent", "xray", "collector"}
		if *load > 0 {
			names = append(names, "load")
		}
		for _, name := range names {
			header = append(header, name+"_kernel_write_bytes_total", name+"_kernel_write_bytes_delta", name+"_fd_count")
		}
	}
	if err := w.Write(header); err != nil {
		return err
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	ticker := time.NewTicker(*interval)
	defer ticker.Stop()
	for i := 0; i < int(*duration / *interval); i++ {
		<-ticker.C
		current, err := collectDetails(pids, *details)
		if err != nil {
			return err
		}
		values, err := row(previous, current, hz)
		if err != nil {
			return err
		}
		if err := w.Write(values); err != nil {
			return err
		}
		w.Flush()
		if err := w.Error(); err != nil {
			return err
		}
		previous = current
	}
	return nil
}

func main() {
	logging.SuppressUnstructured()
	defer logging.RecoverMain()
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, logging.Diagnostic(err))
		os.Exit(1)
	}
}
