package installcheck

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"zic-xray-agent/internal/logging"
	"zic-xray-agent/internal/panel"
)

func TestValidationRejectsUnverifiedInputWithFixedCodes(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "unverified")
	if os.WriteFile(bad, []byte("private-api-canary"), 0600) != nil {
		t.Fatal("fixture setup failed")
	}
	for _, tc := range []struct{ path, code string }{
		{filepath.Join(dir, "missing"), "E_CORE_BINARY"},
		{dir, "E_CORE_BINARY"},
		{"relative", "E_CORE_BINARY"},
		{bad, "E_CORE_VERIFY"},
	} {
		if logging.Diagnostic(VerifyCore(tc.path)) != tc.code {
			t.Fatal("incorrect core classification; values suppressed")
		}
	}
	if _, err := CoreConfig(panel.VLESSSpec{TLS: 1, Port: 443}, 42, nil); logging.Diagnostic(err) != "E_CORE_CONFIG" {
		t.Fatal("unsupported TLS accepted")
	}
}

func TestPrivateValidationCopyAcceptsReadOnlySourceAndCleansUp(t *testing.T) {
	source := os.Getenv("XRAY_BINARY")
	if source == "" {
		t.Skip("requires verified official Xray executable")
	}
	raw, err := os.ReadFile(source)
	if err != nil {
		t.Fatal("official fixture unavailable")
	}
	dir := t.TempDir()
	name := "source-core"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	input := filepath.Join(dir, name)
	if os.WriteFile(input, raw, 0600) != nil || os.Chmod(input, 0600) != nil {
		t.Fatal("read-only fixture setup failed")
	}
	stage := t.TempDir()
	if Validate(context.Background(), input, stage, panel.VLESSSpec{Port: 1443}, 42, nil) != nil {
		t.Fatal("nonexecutable source must validate through private executable copy")
	}
	entries, err := os.ReadDir(stage)
	if err != nil || len(entries) != 0 {
		t.Fatal("validation artifacts leaked")
	}
	info, err := os.Stat(input)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatal("source executable mode was modified")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if logging.Diagnostic(Validate(ctx, input, stage, panel.VLESSSpec{Port: 1443}, 42, nil)) != "E_CORE_TIMEOUT" {
		t.Fatal("cancelled validation classification failed")
	}
	copyName := "core-validation-executable"
	if runtime.GOOS == "windows" {
		copyName += ".exe"
	}
	if os.WriteFile(filepath.Join(stage, copyName), nil, 0600) != nil {
		t.Fatal("copy conflict fixture failed")
	}
	if logging.Diagnostic(Validate(context.Background(), input, stage, panel.VLESSSpec{Port: 1443}, 42, nil)) != "E_CORE_COPY" {
		t.Fatal("copy conflict classification failed")
	}
}

func TestValidationExecutionFailuresDoNotExposeStreams(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	if logging.Diagnostic(runValidation(context.Background(), missing, dir, dir, nil)) != "E_CORE_EXEC" {
		t.Fatal("spawn failure classification failed")
	}
	if core := os.Getenv("XRAY_BINARY"); core != "" {
		if VerifyCore(core) != nil || logging.Diagnostic(runValidation(context.Background(), core, dir, dir, []byte("private-api-canary private-key-canary hwid-canary"))) != "E_CORE_CONFIG" {
			t.Fatal("official rejected-config diagnostic failed; values suppressed")
		}
	}
	// Real Unix permission behavior is exercised only on Linux. Windows builds
	// compile this case but do not claim to execute its permission assertions.
	if runtime.GOOS == "linux" {
		fixture := filepath.Join(dir, "fake-core")
		body := []byte("#!/bin/sh\nprintf '%s' private-api-canary\nprintf '%s' private-key-canary >&2\nexit 1\n")
		if os.WriteFile(fixture, body, 0600) != nil || os.Chmod(fixture, 0600) != nil {
			t.Fatal("fixture setup failed")
		}
		if logging.Diagnostic(runValidation(context.Background(), fixture, dir, dir, nil)) != "E_CORE_EXEC" {
			t.Fatal("missing execute permission not classified")
		}
		if os.Chmod(fixture, 0700) != nil {
			t.Fatal("fixture mode setup failed")
		}
		if logging.Diagnostic(runValidation(context.Background(), fixture, dir, dir, nil)) != "E_CORE_CONFIG" {
			t.Fatal("exit failure classification failed")
		}
		t.Log("Linux missing-execute and rejected-config checks PASS")
	}
}
