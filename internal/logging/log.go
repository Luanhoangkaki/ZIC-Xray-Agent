// Package logging writes bounded warning/error logs without request/user data.
package logging

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const MaxFileBytes = 1 << 20
const Backups = 3

type Logger struct {
	path     string
	file     *os.File
	size     int64
	Warnings bool
	maxBytes int64
}

func Open(dir string) (*Logger, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, errors.New("cannot create private log directory")
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("log directory must be a real directory")
	}
	if os.Chmod(dir, 0700) != nil {
		return nil, errors.New("cannot protect log directory")
	}
	l := &Logger{path: filepath.Join(dir, "agent.log"), Warnings: true, maxBytes: MaxFileBytes}
	if err := l.open(); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Logger) open() error {
	if info, err := os.Lstat(l.path); err == nil && !info.Mode().IsRegular() {
		return errors.New("log must be a regular file")
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("cannot open log file")
	}
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return errors.New("cannot protect log file")
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	l.file, l.size = f, info.Size()
	return nil
}

// Event accepts only a fixed code, never an arbitrary error, URL, body or user.
func (l *Logger) Event(code string) error {
	level := "WARN"
	switch code {
	case "agent_operation_failed":
		level = "ERROR"
	case "panel_sync_failed", "xray_unavailable", "policy_not_supported", "traffic_collection_disabled", "control_sync_failed", "stale_users_disabled":
	default:
		return errors.New("unknown safe log event")
	}
	if !l.Warnings && level != "ERROR" {
		return nil
	}
	line := fmt.Sprintf("%s %s %s\n", time.Now().UTC().Format(time.RFC3339), level, code)
	if l.size+int64(len(line)) > l.maxBytes {
		if err := l.file.Close(); err != nil {
			return err
		}
		for i := Backups; i >= 1; i-- {
			target := fmt.Sprintf("%s.%d", l.path, i)
			source := l.path
			if i > 1 {
				source = fmt.Sprintf("%s.%d", l.path, i-1)
			}
			if info, err := os.Lstat(target); err == nil {
				if !info.Mode().IsRegular() {
					return errors.New("unsafe log rotation target")
				}
				if os.Remove(target) != nil {
					return errors.New("cannot rotate log")
				}
			}
			if err := os.Rename(source, target); err != nil && !os.IsNotExist(err) {
				return errors.New("cannot rotate log")
			}
		}
		if err := l.open(); err != nil {
			return err
		}
	}
	n, err := l.file.WriteString(line)
	l.size += int64(n)
	return err
}

func (l *Logger) Close() error { return l.file.Close() }
