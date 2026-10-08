//go:build unix

package app

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func tryLockFile(f *os.File) (bool, error) {
	if f == nil {
		return false, errors.New("nil file pointer")
	}
	err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, unix.EWOULDBLOCK) || errors.Is(err, unix.EAGAIN) {
		return false, nil
	}
	return false, fmt.Errorf("flock: %w", err)
}

func unlockFile(f *os.File) error {
	if f == nil {
		return nil
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
		return fmt.Errorf("flock unlock: %w", err)
	}
	return nil
}
