package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestLinuxProcessEpochUsesBootIDPIDAndStarttime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test.pid")
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0600); err != nil {
		t.Fatal(err)
	}
	first, err := ProcessEpoch(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ProcessEpoch(path)
	if err != nil || first != second || len(strings.Split(first, ":")) != 3 {
		t.Fatal("unstable process epoch", err)
	}
	if err := os.Chmod(path, 0666); err != nil {
		t.Fatal(err)
	}
	if _, err := ProcessEpoch(path); err == nil {
		t.Fatal("writable PID file trusted")
	}
	if err := os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(filepath.Dir(path), "link.pid")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ProcessEpoch(link); err == nil {
		t.Fatal("symlink PID file trusted")
	}
}
