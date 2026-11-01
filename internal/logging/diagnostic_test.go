package logging

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestDiagnosticNeverRendersUntrustedInput(t *testing.T) {
	// Canaries are compared only in memory and never included in test output.
	canaries := []string{"198.51.100.73", "7ad49170-a0be-4a3e-b903-9e8b71933087", "audit-user@example.invalid", "private-api-canary", "private-key-canary", "visited-canary.invalid", "dns-canary.invalid", "hwid-canary"}
	for _, value := range canaries {
		for _, err := range []error{errors.New(value), fmt.Errorf("network failure: %s", value), &os.PathError{Op: "open", Path: value, Err: os.ErrPermission}} {
			output := Diagnostic(err)
			if !strings.HasPrefix(output, "E_") || strings.Contains(output, value) {
				t.Fatal("unsafe diagnostic boundary")
			}
		}
	}
	if !strings.HasPrefix(Diagnostic(errors.New("Server Token bị từ chối bởi V2B-ZIC (HTTP 500)")), "E_AUTH:") {
		t.Fatal("safe authentication classification lost")
	}
}

func TestSuppressUnstructuredLogger(t *testing.T) {
	old := log.Writer()
	defer log.SetOutput(old)
	SuppressUnstructured()
	if log.Writer() != io.Discard {
		t.Fatal("stdlib logging bypass remains enabled")
	}
}

func TestPanicHelper(t *testing.T) {
	if os.Getenv("ZIC_AUDIT_PANIC_HELPER") != "1" {
		return
	}
	defer RecoverMain()
	panic("private-panic-canary")
}

func TestPanicBoundaryDoesNotPrintValueOrStack(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestPanicHelper$")
	command.Env = append(os.Environ(), "ZIC_AUDIT_PANIC_HELPER=1")
	output, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(output), "E_INTERNAL:") || strings.Contains(string(output), "private-panic-canary") || strings.Contains(string(output), "goroutine ") {
		t.Fatal("unsafe panic output; payload suppressed")
	}
}

func TestErrorLevelRetainsFixedTechnicalFailure(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir)
	if err != nil {
		t.Fatal("logger setup failed")
	}
	l.Warnings = false
	if l.Event("panel_sync_failed") != nil || l.Event("agent_operation_failed") != nil {
		t.Fatal("logger write failed")
	}
	if l.Close() != nil {
		t.Fatal("logger close failed")
	}
	raw, err := os.ReadFile(l.path)
	if err != nil || !strings.Contains(string(raw), "ERROR agent_operation_failed") || strings.Contains(string(raw), "panel_sync_failed") {
		t.Fatal("incorrect safe error-level logging")
	}
}
