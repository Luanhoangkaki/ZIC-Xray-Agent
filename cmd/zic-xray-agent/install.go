package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"zic-xray-agent/internal/daemon"
	"zic-xray-agent/internal/durable"
	"zic-xray-agent/internal/installcheck"
	"zic-xray-agent/internal/logging"
	"zic-xray-agent/internal/panel"
)

func configureInstallCommand(args []string, input io.Reader, output io.Writer) error {
	f := flag.NewFlagSet("configure-install", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	dir := f.String("output-dir", "", "private staging directory")
	choice := f.String("selection", "", "1..4")
	core := f.String("xray-binary", "", "existing official core executable")
	if f.Parse(args) != nil || f.NArg() != 0 || *dir == "" {
		return logging.At(logging.InstallArgs, logging.Input, nil)
	}
	if _, err := panel.InstallationType(*choice); err != nil {
		return logging.At(logging.InstallArgs, logging.Input, err)
	}
	client := panel.NewClient()
	defer client.HTTP.CloseIdleConnections()
	return configureInstallation(input, output, *dir, *choice, *core, client)
}

func configureInstallation(input io.Reader, output io.Writer, dir, choice, core string, client *panel.Client) error {
	info, statErr := os.Lstat(dir)
	entries, readErr := os.ReadDir(dir)
	if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || readErr != nil || len(entries) != 0 {
		return logging.At(logging.InstallStaging, logging.Storage, nil)
	}
	if err := durable.PrivateDir(dir); err != nil {
		return logging.At(logging.InstallStaging, logging.Storage, err)
	}
	auth, err := readCredentials(input)
	if err != nil {
		return logging.At(logging.InstallCredentials, logging.Input, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	probe, err := client.ProbeInstallation(ctx, auth, choice)
	if err != nil {
		return logging.At(logging.InstallProbe, logging.PanelResponse, err)
	}
	if err := probe.RequireAgentAdapter(); err != nil {
		return logging.At(logging.InstallAdapter, logging.Unsupported, err)
	}
	spec, err := panel.ParseVLESS(probe.Config.Raw)
	if err != nil {
		return logging.At(logging.InstallVLESS, logging.VLESSConfig, err)
	}
	if spec.TLS == 0 && !panel.IsLoopbackPanel(auth.PanelURL) {
		return logging.At(logging.InstallTLS, logging.TLSRequired, nil)
	}
	users, err := client.FetchUsers(ctx, auth, "")
	if err != nil {
		return logging.At(logging.InstallUsers, logging.PanelResponse, err)
	}
	if err := panel.RequireSupportedPolicies(users.Users); err != nil {
		return logging.At(logging.InstallPolicy, logging.Policy, err)
	}
	if err := installcheck.Validate(ctx, core, dir, spec, auth.NodeID, users.Users); err != nil {
		return logging.At(logging.InstallCore, logging.CoreConfig, err)
	}
	if err := saveNew(dir, auth, probe.Config); err != nil {
		return logging.At(logging.InstallSave, logging.Storage, err)
	}
	fmt.Fprintln(output, "API và Xray tiền kiểm hợp lệ. Agent được cấu hình observe; không bật proxy hoặc gửi traffic POST.")
	return nil
}

// installationStatus is a local, read-only startup check. It never sends a
// request, recovers a ledger, or exposes the private snapshot payload.
func installationStatus(args []string) error {
	f := flag.NewFlagSet("install-status", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	config := f.String("config", "", "private agent config")
	state := f.String("state-dir", "", "agent state directory")
	if f.Parse(args) != nil || f.NArg() != 0 || *config == "" || *state == "" {
		return errors.New("cần config/state-dir cho tiền kiểm startup")
	}
	c, err := daemon.LoadConfig(*config)
	if err != nil || c.Mode != "observe" || c.TrafficSendEnabled {
		return errors.New("cấu hình observer không hợp lệ")
	}
	var snapshot daemon.Snapshot
	if durable.Load(filepath.Join(*state, "snapshot.json"), &snapshot) != nil || snapshot.Version != 1 || snapshot.Identity != c.Identity() || snapshot.LastSuccess.IsZero() || snapshot.LastSuccess.After(time.Now().Add(time.Second)) || time.Since(snapshot.LastSuccess) > 2*time.Minute || !snapshot.PolicySupported {
		return errors.New("observer chưa có snapshot mới được xác thực")
	}
	if _, err := panel.ParseVLESS(snapshot.Config); err != nil {
		return errors.New("snapshot node chưa được hỗ trợ")
	}
	if err := panel.RequireSupportedPolicies(snapshot.Users); err != nil {
		return errors.New("snapshot có chính sách chưa được hỗ trợ")
	}
	return nil
}
