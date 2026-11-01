// Package installcheck performs offline validation with an independently
// verified OFFICIAL core executable. It never starts an Xray server.
package installcheck

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"

	"zic-xray-agent/internal/logging"
	"zic-xray-agent/internal/panel"
)

// SHA256 of the executable extracted from official v26.10.29 assets, whose
// archive digests were checked against official GitHub release metadata.
var coreHashes = map[string]string{
	"linux/amd64":   "e783b9d2b46ed1ab3617c59b04dfbce38c999322d018c6e060f18d88bf6158aa",
	"linux/arm64":   "f02fa77fa2f1a0f8f2b4fd69af5000494bcc3fa4e97899caf023c7f72fd74474",
	"windows/amd64": "07a332b714a3c0eee047a22a9c0ea749b64df33e4e1ff6d28a8c7f9ef943ce10", // native tests only
}

func VerifyCore(binary string) error {
	info, err := os.Lstat(binary)
	if err != nil || !filepath.IsAbs(binary) || !info.Mode().IsRegular() || info.Size() > 128<<20 {
		return logging.Failure(logging.CoreBinary, nil)
	}
	f, err := os.Open(binary)
	if err != nil {
		return logging.Failure(logging.CoreRead, nil)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, io.LimitReader(f, (128<<20)+1)); err != nil {
		return logging.Failure(logging.CoreRead, nil)
	}
	want := coreHashes[runtime.GOOS+"/"+runtime.GOARCH]
	if want == "" || hex.EncodeToString(h.Sum(nil)) != want {
		return logging.Failure(logging.CoreVerify, nil)
	}
	return nil
}

func CoreConfig(spec panel.VLESSSpec, nodeID int64, users []panel.User) ([]byte, error) {
	if spec.Port == 0 || spec.Port > 65535 || (spec.TLS != 0 && (spec.TLS != 2 || spec.Reality == nil)) {
		return nil, logging.Failure(logging.CoreConfig, nil)
	}
	clients := []any{}
	for _, u := range users {
		clients = append(clients, map[string]any{"id": u.UUID, "email": u.Email(nodeID), "flow": spec.Flow, "level": 0})
	}
	stream := map[string]any{"network": "tcp", "security": "none"}
	if r := spec.Reality; spec.TLS == 2 && r != nil {
		stream["security"] = "reality"
		stream["realitySettings"] = map[string]any{"show": false, "dest": r.Destination, "xver": r.Xver, "serverNames": []string{r.ServerName}, "privateKey": base64.RawURLEncoding.EncodeToString(r.PrivateKey), "shortIds": []string{hex.EncodeToString(r.ShortID)}}
	}
	return json.Marshal(map[string]any{
		"log":       map[string]any{"loglevel": "none", "access": "none", "error": "none", "dnsLog": false},
		"inbounds":  []any{map[string]any{"tag": "zic-install-validation-only", "listen": "127.0.0.1", "port": spec.Port, "protocol": "vless", "settings": map[string]any{"clients": clients, "decryption": "none"}, "streamSettings": stream}},
		"outbounds": []any{map[string]any{"protocol": "blackhole"}},
		"stats":     map[string]any{},
		"policy":    map[string]any{"levels": map[string]any{"0": map[string]any{"statsUserUplink": true, "statsUserDownlink": true}}},
	})
}

func Validate(ctx context.Context, binary, directory string, spec panel.VLESSSpec, nodeID int64, users []panel.User) error {
	if err := VerifyCore(binary); err != nil {
		return err
	}
	// Execute a private verified copy, preventing a writable source binary from
	// being swapped between checksum verification and execution. Never modify
	// the installed core executable or its running service.
	copyPath := filepath.Join(directory, "core-validation-executable")
	if runtime.GOOS == "windows" {
		copyPath += ".exe"
	}
	copyFile, err := os.OpenFile(copyPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return logging.Failure(logging.CoreCopy, nil)
	}
	defer os.Remove(copyPath)
	// Only the private copy is executed; the verified source is read-only input
	// and need not have execute bits. Do not inherit a restrictive umask here.
	if copyFile.Chmod(0700) != nil {
		copyFile.Close()
		return logging.Failure(logging.CoreCopy, nil)
	}
	source, err := os.Open(binary)
	if err != nil {
		copyFile.Close()
		return logging.Failure(logging.CoreRead, nil)
	}
	_, copyErr := io.Copy(copyFile, io.LimitReader(source, (128<<20)+1))
	source.Close()
	closeErr := copyFile.Close()
	if copyErr != nil || closeErr != nil {
		return logging.Failure(logging.CoreCopy, nil)
	}
	if err := VerifyCore(copyPath); err != nil {
		return err
	}
	binary = copyPath
	raw, err := CoreConfig(spec, nodeID, users)
	if err != nil {
		return err
	}
	// Empty confdir prevents inherited XRAY_LOCATION_CONFDIR from appending any
	// existing Xray config. A minimal environment also removes proxy variables.
	confdir, err := os.MkdirTemp(directory, "empty-confdir-")
	if err != nil {
		return logging.Failure(logging.CoreCopy, nil)
	}
	defer os.Remove(confdir)
	return runValidation(ctx, binary, directory, confdir, raw)
}

// Called only after checksum verification of the private executable copy.
func runValidation(ctx context.Context, binary, directory, confdir string, raw []byte) error {
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(bounded, binary, "run", "-test", "-format", "json", "-config", "stdin:", "-confdir", confdir)
	command.Stdin = bytes.NewReader(raw) // No UUID/private key JSON written to disk.
	command.Dir = directory
	command.Env = []string{"PATH=/usr/bin:/bin", "GOMAXPROCS=1", "GOMEMLIMIT=32MiB", "HOME=" + directory, "TMP=" + directory, "TEMP=" + directory}
	if runtime.GOOS == "windows" {
		command.Env = append(command.Env, "SystemRoot="+os.Getenv("SystemRoot"))
	}
	// Core diagnostics may contain private keys/users. Discard both streams;
	// command arguments contain only paths, never API Key or JSON payload.
	command.Stdout, command.Stderr = io.Discard, io.Discard
	if err := command.Run(); err != nil {
		if bounded.Err() != nil {
			return logging.Failure(logging.CoreTimeout, nil)
		}
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return logging.Failure(logging.CoreConfig, nil)
		}
		return logging.Failure(logging.CoreExec, nil)
	}
	return nil
}
