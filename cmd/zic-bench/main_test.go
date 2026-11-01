package main

import (
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestProcNameWithSpacesAndParentheses(t *testing.T) {
	fields := strings.Fields("S 0 0 0 0 0 0 0 0 0 0 25 15 0 0 0 0 0 0 12345 1000000 12")
	p, err := parseProcess("42 (worker (task) name) "+strings.Join(fields, " "), 4096)
	if err != nil || p.ticks != 40 || p.start != 12345 || p.rss != 12*4096 {
		t.Fatal("incorrect /proc field parsing", p, err)
	}
}

func TestLinuxReadOnlyFDAndIOPlateau(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux /proc FD/IO execution required; NOT RUN on this host")
	}
	pid := os.Getpid()
	before, err := countFDs(pid)
	if err != nil {
		t.Fatal("FD measurement unavailable")
	}
	for i := 0; i < 200; i++ {
		if _, err := countFDs(pid); err != nil {
			t.Fatal("FD sampling failed")
		}
		raw, err := os.ReadFile("/proc/self/io")
		if err != nil {
			t.Fatal("IO sampling failed")
		}
		if _, err := parseWriteBytes(string(raw)); err != nil {
			t.Fatal("IO sample malformed")
		}
	}
	after, err := countFDs(pid)
	if err != nil || after > before+2 {
		t.Fatal("collector sampling FD leak")
	}
	t.Logf("linux_sampler_fd_before=%d linux_sampler_fd_after=%d", before, after)
}

func TestOptionalLoadProcessDoesNotPolluteAgentOrCoreMetrics(t *testing.T) {
	before := sample{at: time.Unix(1000, 0), total: 100, idle: 50, hasLoad: true}
	after := sample{at: before.at.Add(time.Second), total: 200, idle: 100, hasLoad: true}
	for i := range before.processes {
		before.processes[i] = process{start: 42}
		after.processes[i] = process{start: 42}
	}
	after.processes[3] = process{start: 42, ticks: 50, rss: 1234}
	values, err := row(before, after, 100)
	if err != nil || len(values) != 14 || values[2] != "0.0000" || values[4] != "0.0000" || values[12] != "50.0000" || values[13] != "1234" {
		t.Fatal("load metrics mixed into agent/core", values, err)
	}
}

func TestCPUUsesElapsedTimeAndRejectsPIDReuse(t *testing.T) {
	before := sample{at: time.Unix(1000, 0), total: 1000, idle: 500}
	after := sample{at: before.at.Add(2 * time.Second), total: 1200, idle: 650, memory: 1024, available: 100}
	for i := range before.processes {
		before.processes[i] = process{ticks: 100, start: 42, rss: 1000}
		after.processes[i] = process{ticks: 110, start: 42, rss: 2000}
	}
	values, err := row(before, after, 100)
	if err != nil || values[2] != "5.0000" || values[3] != "2000" || values[8] != "25.0000" {
		t.Fatal("incorrect interval CPU", values, err)
	}
	after.processes[0].start++
	if _, err := row(before, after, 100); err == nil {
		t.Fatal("PID reuse accepted")
	}
}

func TestOptionalIOAndFDCountsDoNotChangeDefaultCSV(t *testing.T) {
	before := sample{at: time.Unix(1000, 0), total: 100, idle: 40, hasDetails: true}
	after := sample{at: before.at.Add(time.Second), total: 200, idle: 80, hasDetails: true}
	for i := range before.processes {
		before.processes[i] = process{start: 42, writeBytes: 100}
		after.processes[i] = process{start: 42, writeBytes: 140, fdCount: 7}
	}
	values, err := row(before, after, 100)
	if err != nil || len(values) != 21 || values[12] != "140" || values[13] != "40" || values[14] != "7" {
		t.Fatal("incorrect resource detail columns")
	}
	after.processes[0].writeBytes = 99
	if _, err := row(before, after, 100); err == nil {
		t.Fatal("IO counter reset accepted")
	}
	if v, err := parseWriteBytes("rchar: 99\nwrite_bytes: 140\n"); err != nil || v != 140 {
		t.Fatal("IO counter parsing failed")
	}
	if _, err := parseWriteBytes("rchar: 99\n"); err == nil {
		t.Fatal("missing IO counter accepted")
	}
}
