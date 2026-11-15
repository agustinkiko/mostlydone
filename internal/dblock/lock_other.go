//go:build !unix

package dblock

import "os"

// On non-unix platforms the lock is a no-op: there is no flock, and the
// supported deployment is a Linux container. Do not rely on it there.
func lockFile(f *os.File) error { return nil }

func unlockFile(f *os.File) {}
