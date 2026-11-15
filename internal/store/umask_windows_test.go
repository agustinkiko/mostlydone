//go:build windows

package store

func setPermissiveUmask() func() { return func() {} }
