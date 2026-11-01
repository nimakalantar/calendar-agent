//go:build unix

// Package disk reports free space for preflight and readiness checks.
package disk

import "golang.org/x/sys/unix"

// FreeBytes returns the bytes available to an unprivileged process on the
// filesystem containing path.
func FreeBytes(path string) (uint64, error) {
	var stats unix.Statfs_t
	if err := unix.Statfs(path, &stats); err != nil {
		return 0, err
	}
	return stats.Bavail * uint64(stats.Bsize), nil
}
