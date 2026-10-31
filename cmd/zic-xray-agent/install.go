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
	"zic-xray-agent/internal/panel"
)

func configureInstallCommand(args []string, input io.Reader, output io.Writer) error {
	f := flag.NewFlagSet("configure-install", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	dir := f.String("output-dir", "", "private staging directory")
	choice := f.String("selection", "", "1..4")
	core := f.String("xray-binary", "", "existing official core executable")
	if f.Parse(args) != nil || f.NArg() != 0 || *dir == "" {
		return errors.New("tham số cài đặt không hợp lệ; không nhận API Key qua argv")
	}
	if _, err := panel.InstallationType(*choice); err != nil {
		return err
	}
	client := panel.NewClient()
	defer client.HTTP.CloseIdleConnections()
	return configureInstallation(input, output, *dir, *choice, *core, client)
}

func configureInstallation(input io.Reader, output io.Writer, dir, choice, core string, client *panel.Client) error {
	info, statErr := os.Lstat(dir)
	entries, readErr := os.ReadDir(dir)
	if statErr != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || readErr != nil || len(entries) != 0 {
		return errors.New("tiền kiểm cần thư mục staging trống, không phải symlink")
	}
	if err := durable.PrivateDir(dir); err != nil {
		return err
	}
	auth, err := readCredentials(input)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	probe, err := client.ProbeInstallation(ctx, auth, choice)
	if err != nil {
		return err
	}
	if err := probe.RequireAgentAdapter(); err != nil {
		return err
	}
	spec, err := panel.ParseVLESS(probe.Config.Raw)
	if err != nil {
		return err
	}
	if spec.TLS == 0 && !panel.IsLoopbackPanel(auth.PanelURL) {
		return errors.New("VLESS không TLS chỉ được dùng trong kiểm thử loopback")
	}
	users, err := client.FetchUsers(ctx, auth, "")
	if err != nil {
		return err
	}
	if err := panel.RequireSupportedPolicies(users.Users); err != nil {
		return err
	}
	if err := installcheck.Validate(ctx, core, dir, spec, auth.NodeID, users.Users); err != nil {
		return err
	}
	if err := saveNew(dir, auth, probe.Config); err != nil {
		return err
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
