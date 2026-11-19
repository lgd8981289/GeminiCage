//go:build !windows

package cage

func OtherWindowsInstallDir() (string, error) { return "", nil }
