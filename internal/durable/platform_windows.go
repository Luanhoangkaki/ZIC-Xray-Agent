package durable

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// Windows is a development/test host only; Linux permission/directory-fsync
// guarantees are not claimed by these compatibility implementations.
func privateMode(os.FileMode) bool { return true }
func syncDirectory(string) error   { return nil }
func Lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, errors.New("cannot open daemon lock")
	}
	var overlap windows.Overlapped
	if windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlap) != nil {
		f.Close()
		return nil, errors.New("another agent owns this state directory")
	}
	return func() { f.Close() }, nil
}
