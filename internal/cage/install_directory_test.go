package cage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallDirectorySeparatesProfilesAndExistingApps(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "user")
	for _, dir := range []string{home, filepath.Join(root, "old"), filepath.Join(root, "ours")} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for dir, id := range map[string]string{"old": "io.github.lee-xeo.claude-cage", "ours": "local.sunday.gemini-cage"} {
		if err := os.WriteFile(filepath.Join(root, dir, "Cage.install-id"), []byte(id), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, target := range []string{home, filepath.Join(home, ".claude-cage", "child"), filepath.Join(DefaultProfileDir(home), "install"), filepath.Join(root, "old"), filepath.Join(root, "old", "sub"), filepath.Join(root, "ours", "sub")} {
		if err := CheckInstallDirectory(target, home); err == nil {
			t.Errorf("accepted protected directory %q", target)
		}
	}
	for _, target := range []string{filepath.Join(root, "ours"), filepath.Join(root, "old-sibling"), filepath.Join(root, "new", "app")} {
		if err := CheckInstallDirectory(target, home); err != nil {
			t.Errorf("rejected independent directory %q: %v", target, err)
		}
	}
}
