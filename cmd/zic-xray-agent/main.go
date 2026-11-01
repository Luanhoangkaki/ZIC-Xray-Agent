package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"

	"zic-xray-agent/internal/buildinfo"
	"zic-xray-agent/internal/daemon"
	"zic-xray-agent/internal/logging"
	"zic-xray-agent/internal/panel"
	"zic-xray-agent/internal/xrayapi"
)

// configure and the observe daemon never start a proxy or mutate Xray.
func main() {
	logging.SuppressUnstructured()
	defer logging.RecoverMain()
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, logging.Diagnostic(err))
		os.Exit(1)
	}
}

func run(args []string, input io.Reader, output io.Writer) error {
	if len(args) > 0 && args[0] == "version" {
		if len(args) != 1 {
			return logging.Failure(logging.Input, nil)
		}
		_, err := fmt.Fprintln(output, "ZIC-Xray-Agent "+buildinfo.Tag)
		return err
	}
	if len(args) > 0 && args[0] == "configure-install" {
		return configureInstallCommand(args[1:], input, output)
	}
	if len(args) > 0 && args[0] == "install-status" {
		return installationStatus(args[1:])
	}
	if len(args) > 0 && args[0] == "traffic" {
		return trafficCommand(args[1:], output)
	}
	if len(args) > 0 && args[0] == "run" {
		return runDaemon(args[1:])
	}
	if len(args) == 0 || args[0] != "configure" {
		return errors.New("cú pháp: zic-xray-agent configure --output-dir THƯ_MỤC hoặc run --config FILE; daemon hiện chỉ chạy observe")
	}
	flags := flag.NewFlagSet("configure", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	dir := flags.String("output-dir", "", "private configuration output directory")
	if flags.Parse(args[1:]) != nil || *dir == "" || flags.NArg() != 0 {
		return errors.New("cần --output-dir; không nhận token qua tham số dòng lệnh")
	}
	return configure(input, output, *dir, panel.NewClient())
}

func configure(input io.Reader, output io.Writer, dir string, client *panel.Client) error {
	defer client.HTTP.CloseIdleConnections()
	auth, err := readCredentials(input)
	if err != nil {
		return err
	}
	result, err := client.FetchConfig(context.Background(), auth, "")
	if err != nil {
		return err
	}
	if _, err := client.FetchUsers(context.Background(), auth, ""); err != nil {
		return err
	}
	if err := saveNew(dir, auth, result); err != nil {
		return err
	}
	fmt.Fprintln(output, "API UniProxy V1 chấp nhận thông tin node và trả cấu hình VLESS. Đã lưu cấu hình riêng; chưa khởi động hoặc sửa Xray.")
	return nil
}

func readCredentials(input io.Reader) (panel.Credentials, error) {
	r := bufio.NewReader(io.LimitReader(input, 8193))
	values := make([]string, 3)
	for i := range values {
		value, err := r.ReadString(0)
		if err != nil {
			return panel.Credentials{}, errors.New("thiếu dữ liệu cài đặt qua stdin")
		}
		values[i] = strings.TrimSuffix(value, "\x00")
	}
	if _, err := r.ReadByte(); err != io.EOF {
		return panel.Credentials{}, errors.New("dữ liệu cài đặt dư hoặc quá dài")
	}
	id, err := strconv.ParseInt(values[2], 10, 64)
	if err != nil {
		return panel.Credentials{}, errors.New("Node ID phải là số nguyên dương")
	}
	auth := panel.Credentials{PanelURL: values[0], Token: values[1], NodeID: id, NodeType: "vless"}
	if err := auth.Validate(); err != nil {
		return panel.Credentials{}, err
	}
	return auth, nil
}

// saveNew only accepts a new/empty real directory; credentials are never replaced
// on a failed validation. install.sh publishes the completed staging directory.
func saveNew(dir string, auth panel.Credentials, result panel.Result) error {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("thư mục cấu hình phải tồn tại và không phải symlink")
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		return errors.New("chỉ ghi vào thư mục cấu hình trống")
	}
	if os.Chmod(dir, 0700) != nil {
		return errors.New("không đặt được quyền thư mục cấu hình")
	}
	config := daemon.Config{Credentials: auth, Pull: result.PullInterval, Push: result.PushInterval, LogLevel: "warn", Mode: "observe", PanelCommit: panel.ProductionCommit}
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return errors.New("không mã hóa được cấu hình")
	}
	for _, entry := range []struct {
		name string
		data []byte
	}{{"panel-config.json", result.Raw}, {"config.json", append(data, '\n')}} {
		f, err := os.OpenFile(filepath.Join(dir, entry.name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			return errors.New("không tạo được file cấu hình riêng")
		}
		_, writeErr := f.Write(entry.data)
		syncErr := f.Sync()
		closeErr := f.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			return errors.New("không ghi bền vững được cấu hình")
		}
	}
	return nil
}

func runDaemon(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	configPath := flags.String("config", "/etc/zic-xray-agent/config.json", "private config file")
	stateDir := flags.String("state-dir", "/var/lib/zic-xray-agent/data", "private state directory")
	logDir := flags.String("log-dir", "/var/log/zic-xray-agent/files", "private log directory")
	if flags.Parse(args) != nil || flags.NArg() != 0 {
		return errors.New("invalid daemon flags; credentials must be in the private config file")
	}
	if runtime.GOOS != "linux" {
		return errors.New("daemon deployment requires Debian/Ubuntu Linux")
	}
	config, err := daemon.LoadConfig(*configPath)
	if err != nil {
		return err
	}
	logger, err := logging.Open(*logDir)
	if err != nil {
		return err
	}
	defer logger.Close()
	logger.Warnings = config.LogLevel == "warn"
	client := panel.NewClient()
	defer client.HTTP.CloseIdleConnections()
	runner := daemon.Runner{Config: config, Panel: client, Log: logger, StateDir: *stateDir}
	if config.XrayEndpoint != "" {
		core, err := xrayapi.Connect(config.XrayEndpoint)
		if err != nil {
			return err
		}
		defer core.Close()
		runner.Stats = core
		runner.ControlCore = core
		runner.Epoch = func() (string, error) { return daemon.ProcessEpoch(config.XrayPIDFile) }
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	err = runner.Run(ctx)
	if err != nil {
		_ = logger.Event("agent_operation_failed")
	}
	return err
}
