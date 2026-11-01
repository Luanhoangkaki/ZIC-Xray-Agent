package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"zic-xray-agent/internal/durable"
	"zic-xray-agent/internal/lab"
	"zic-xray-agent/internal/logging"
)

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("lab requires init, serve, ready, measure, snapshot, compare or write-pid")
	}
	f := flag.NewFlagSet("lab", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	root := f.String("root", "/opt/zic-xray-lab", "isolated kit root")
	config := f.String("config", "/opt/zic-xray-lab/config/lab.json", "private synthetic manifest")
	base := f.Int("base-port", 38080, "first of six loopback lab ports")
	state := f.String("state-dir", "/var/lib/zic-lab-agent/data", "isolated agent state")
	pidFile := f.String("pid-file", "/run/zic-lab-core/xray.pid", "isolated core PID file")
	cert := f.String("cert", "", "server certificate")
	key := f.String("key", "", "server key")
	ca := f.String("ca", "", "lab CA override")
	panelOnly := f.Bool("panel-only", false, "readiness only HTTPS mock")
	coreOnly := f.Bool("core-only", false, "readiness only lab core")
	connections := f.Int("connections", 0, "0/10/20/30 concurrent synthetic sessions")
	duration := f.Duration("duration", 30*time.Minute, "phase duration")
	out := f.String("out", "", "private output path")
	agentPID := f.Int("agent-pid", 0, "isolated agent main PID")
	corePID := f.Int("core-pid", 0, "isolated core main PID")
	pid := f.Int("pid", 0, "PID to publish")
	before := f.String("before", "", "before recovery snapshot")
	after := f.String("after", "", "after phase snapshot")
	summary := f.String("summary", "", "phase summary")
	kind := f.String("kind", "restart", "restart/reboot")
	if err := f.Parse(args[1:]); err != nil || f.NArg() != 0 {
		return errors.New("invalid lab arguments")
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if args[0] == "init" {
		_, err := lab.Initialize(*root, *base, *state, *pidFile)
		return err
	}
	if args[0] == "write-pid" {
		if *pid <= 0 || !filepath.IsAbs(*out) {
			return errors.New("explicit PID and absolute output required")
		}
		if err := durable.Write(*out, []byte(strconv.Itoa(*pid)+"\n")); err != nil {
			return err
		}
		return os.Chmod(*out, 0644)
	}
	c, err := lab.Load(*config)
	if err != nil {
		return err
	}
	switch args[0] {
	case "serve":
		if *cert == "" || *key == "" {
			return errors.New("explicit lab certificate/key paths required")
		}
		return lab.Serve(ctx, c, *cert, *key)
	case "ready":
		bounded, stop := context.WithTimeout(ctx, 45*time.Second)
		defer stop()
		return lab.Ready(bounded, c, *ca, *panelOnly, *coreOnly)
	case "measure":
		if *out == "" {
			return errors.New("output directory required")
		}
		_, err := lab.Measure(ctx, c, lab.MeasureOptions{Connections: *connections, Duration: *duration, Interval: 100 * time.Millisecond, Chunk: 4096, AgentPID: *agentPID, CorePID: *corePID, Output: *out, Collector: filepath.Join(c.Root, "bin", "zic-bench")})
		return err
	case "snapshot":
		if *out == "" {
			return errors.New("output snapshot required")
		}
		bounded, stop := context.WithTimeout(ctx, 90*time.Second)
		defer stop()
		_, err := lab.Snapshot(bounded, c, *agentPID, *out)
		return err
	case "compare":
		if *out == "" || *before == "" {
			return errors.New("before/output paths required")
		}
		bounded, stop := context.WithTimeout(ctx, 90*time.Second)
		defer stop()
		return lab.CompareRecovery(bounded, c, *agentPID, *before, *kind, *out)
	case "check-phase":
		return lab.CheckPhase(c, *before, *after, *summary, *out)
	default:
		return errors.New("unsupported lab action; real panel sending is unavailable")
	}
}
func main() {
	logging.SuppressUnstructured()
	defer logging.RecoverMain()
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, logging.Diagnostic(err))
		os.Exit(1)
	}
}
