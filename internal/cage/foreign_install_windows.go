//go:build windows

package cage

import (
	"errors"
	"os"
	"path/filepath"

	"golang.org/x/sys/windows/registry"
)

// Read the 64-bit uninstall registration even when a 32-bit installer calls us.
func OtherWindowsInstallDir() (string, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall\ClaudeCage`, registry.QUERY_VALUE|registry.WOW64_64KEY)
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	defer k.Close()
	dir, _, err := k.GetStringValue("InstallLocation")
	if errors.Is(err, registry.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !filepath.IsAbs(dir) {
		return "", errors.New("无法核对已有 Claude Cage 的安装目录")
	}
	real, err := StoragePhysicalPath(dir)
	if os.IsNotExist(err) {
		return dir, nil
	}
	return real, err
}
