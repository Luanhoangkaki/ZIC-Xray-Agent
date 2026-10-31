package durable

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

func privateMode(mode os.FileMode) bool { return mode.Perm()&0077 == 0 }
func syncDirectory(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// Lock is held by an open descriptor and released by the kernel on process exit.
func Lock(path string) (func(), error) {
	fd, err := unix.Open(path, unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, errors.New("cannot open daemon lock")
	}
	if unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB) != nil {
		unix.Close(fd)
		return nil, errors.New("another agent owns this state directory")
	}
	return func() { unix.Close(fd) }, nil
}
