package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBoundedRotationAndFixedSafeEvents(t *testing.T) {
	dir := t.TempDir()
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	l.maxBytes = 128
	for i := 0; i < 50; i++ {
		if err := l.Event("panel_sync_failed"); err != nil {
			t.Fatal(err)
		}
	}
	canaries := []string{"secret-token", "198.51.100.73", "7ad49170-a0be-4a3e-b903-9e8b71933087", "audit-user@example.invalid", "hwid-canary", "visited-canary.invalid", "dns-canary.invalid", "private-key-canary"}
	for _, value := range canaries {
		if err := l.Event(value); err == nil {
			t.Fatal("arbitrary data accepted as log event")
		}
	}
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	files, err := os.ReadDir(dir)
	if err != nil || len(files) != Backups+1 {
		t.Fatal("incorrect rotation retention", err)
	}
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(dir, file.Name()))
		if err != nil || len(data) > 128 {
			t.Fatal("unbounded or unsafe log", err)
		}
		for _, value := range canaries {
			if strings.Contains(string(data), value) {
				t.Fatal("sensitive input reached a log/rotated log; value suppressed")
			}
		}
	}
}
