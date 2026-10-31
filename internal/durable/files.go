// Package durable contains bounded, atomic checkpoints and an OS process lock.
package durable

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
)

const MaxBytes = 8 << 20

func PrivateDir(path string) error {
	if err := os.MkdirAll(path, 0700); err != nil {
		return errors.New("cannot create private state directory")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("state directory must be a real directory")
	}
	if err := os.Chmod(path, 0700); err != nil {
		return err
	}
	if err := syncDirectory(path); err != nil {
		return err
	}
	return syncDirectory(filepath.Dir(path))
}

// ReadPrivate refuses symlinks/nonregular files and insecure Unix permissions.
func ReadPrivate(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || !privateMode(info.Mode()) {
		return nil, errors.New("file must be regular and private")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil || int64(len(b)) > limit {
		return nil, errors.New("file unreadable or exceeds size limit")
	}
	return b, nil
}

// Write is atomic on the supported Linux filesystems. A sync/rename error is
// fatal to the caller: reload the checkpoint before attempting another update.
func Write(path string, data []byte) error {
	if len(data) > MaxBytes {
		return errors.New("checkpoint exceeds size limit")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".checkpoint-*")
	if err != nil {
		return errors.New("cannot create checkpoint")
	}
	name := f.Name()
	defer os.Remove(name)
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return errors.New("cannot protect checkpoint")
	}
	_, writeErr := f.Write(data)
	syncErr := f.Sync()
	closeErr := f.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("checkpoint write/fsync failed")
	}
	if err := os.Rename(name, path); err != nil {
		return errors.New("checkpoint rename failed")
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return errors.New("checkpoint directory fsync failed; reload required")
	}
	return nil
}

type envelope struct {
	SHA256  string          `json:"sha256"`
	Payload json.RawMessage `json:"payload"`
}

func Save(path string, value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return errors.New("cannot encode checkpoint")
	}
	hash := sha256.Sum256(data)
	wrapped, err := json.Marshal(envelope{hex.EncodeToString(hash[:]), data})
	if err != nil {
		return errors.New("cannot encode checkpoint envelope")
	}
	return Write(path, wrapped)
}

func Load(path string, value any) error {
	data, err := ReadPrivate(path, MaxBytes)
	if err != nil {
		return err
	}
	var e envelope
	if err := json.Unmarshal(data, &e); err != nil {
		return errors.New("invalid checkpoint envelope")
	}
	hash := sha256.Sum256(e.Payload)
	if e.SHA256 != hex.EncodeToString(hash[:]) {
		return errors.New("checkpoint checksum mismatch")
	}
	d := json.NewDecoder(bytes.NewReader(e.Payload))
	d.DisallowUnknownFields()
	if d.Decode(value) != nil {
		return errors.New("invalid checkpoint payload")
	}
	return nil
}
