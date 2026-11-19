package cage

import (
	"errors"
	"os"
	"path/filepath"
)

func CheckInstallDirectory(target, home string) error {
	actual, err := StoragePhysicalPath(target)
	if err != nil {
		return err
	}
	other, err := OtherWindowsInstallDir()
	if err != nil {
		return err
	}
	original := filepath.Join(home, ".claude-cage")
	private, err := StoragePrivatePaths(original)
	if err != nil {
		return err
	}
	own := DefaultProfileDir(home)
	ownStorage, err := StoragePrivatePaths(own)
	if err != nil {
		return err
	}
	// The default Windows data directory of a packaged installation is
	// <install>/CageData. Its installer preserves that directory on upgrade.
	marker, _ := os.ReadFile(filepath.Join(actual, "Cage.install-id"))
	for _, directory := range ownStorage {
		if string(marker) == "local.sunday.gemini-cage" {
			real, err := StoragePhysicalPath(directory)
			if err == nil && sameStoragePath(real, filepath.Join(actual, "CageData")) {
				continue
			}
		}
		private = append(private, directory)
	}
	for _, p := range append(private, original, own, other) {
		if p == "" {
			continue
		}
		real, err := StoragePhysicalPath(p)
		if err != nil {
			if !os.IsNotExist(err) {
				return err
			}
			real = p
		}
		if StoragePathsOverlap(actual, real) {
			return errors.New("请选择独立于已有应用、配置和虚拟磁盘的安装目录。")
		}
	}
	// Include unregistered installs and descendants reached through a junction.
	for parent := actual; parent != filepath.Dir(parent); parent = filepath.Dir(parent) {
		id, err := os.ReadFile(filepath.Join(parent, "Cage.install-id"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if parent != actual || string(id) != "local.sunday.gemini-cage" {
			return errors.New("此目录属于已有安装，请选择独立的 Gemini Cage 目录。")
		}
	}
	return nil
}
