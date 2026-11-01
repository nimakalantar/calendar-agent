//go:build !unix

package disk

import "errors"

// ErrUnsupported is returned on platforms without statfs.
var ErrUnsupported = errors.New("disk space checks are not supported on this platform")

// FreeBytes is unavailable on this platform.
func FreeBytes(string) (uint64, error) { return 0, ErrUnsupported }
