//go:build !windows

package store

import "syscall"

// setPermissiveUmask clears the umask and returns a func that restores it.
func setPermissiveUmask() func() {
	old := syscall.Umask(0)
	return func() { syscall.Umask(old) }
}
