//go:build unix

// Package lock provides a small exclusive advisory file lock.
package lock

import (
	"fmt"
	"os"
	"syscall"
)

// Acquire takes an exclusive advisory lock on path. The returned function
// releases the lock and closes the file.
func Acquire(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		return nil, fmt.Errorf("another sync is already running (lock: %s)", path)
	}
	return func() {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
	}, nil
}
