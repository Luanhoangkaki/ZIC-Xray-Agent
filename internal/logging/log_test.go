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
	if err := l.Event("secret-token"); err == nil {
		t.Fatal("arbitrary data accepted as log event")
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
		if err != nil || len(data) > 128 || strings.Contains(string(data), "secret-token") {
			t.Fatal("unbounded or unsafe log", err)
		}
	}
}
