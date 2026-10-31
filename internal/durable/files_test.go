package durable

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCheckpointAndCorruption(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	value := struct {
		Value int `json:"value"`
	}{42}
	if err := Save(path, value); err != nil {
		t.Fatal(err)
	}
	value.Value = 0
	if err := Load(path, &value); err != nil || value.Value != 42 {
		t.Fatal("checkpoint not recovered", err)
	}
	if err := os.WriteFile(path, []byte(`{"sha256":"bad","payload":{"value":99}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if Load(path, &value) == nil {
		t.Fatal("corrupt checkpoint accepted")
	}
}

func TestKernelLockIsReleased(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	unlock, err := Lock(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := Lock(path); err == nil {
		second()
		unlock()
		t.Fatal("concurrent daemon lock acquired")
	}
	unlock()
	third, err := Lock(path)
	if err != nil {
		t.Fatal("lock not released", err)
	}
	third()
}
