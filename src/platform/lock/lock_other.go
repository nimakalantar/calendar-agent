//go:build !unix

package lock

import (
	"fmt"
	"os"
	"strconv"
)

// Acquire takes an exclusive lock by creating a lock file with O_EXCL. This
// fallback is used on platforms without flock and can leave a stale lock after
// an unclean exit; remove the file after confirming no instance is running.
func Acquire(path string) (func(), error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		if os.IsExist(err) {
			return nil, fmt.Errorf("another sync is already running (lock: %s)", path)
		}
		return nil, err
	}
	_, _ = file.WriteString(strconv.Itoa(os.Getpid()))
	return func() {
		_ = file.Close()
		_ = os.Remove(path)
	}, nil
}
