//go:build windows

package backup

func setPermissiveUmask() func() { return func() {} }
