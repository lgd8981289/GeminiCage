package cage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGeminiDownloadsOutsideLibrary(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	profile := filepath.Join(home, "Library", "GeminiCage")
	downloads := filepath.Join(home, "Documents", "Gemini Cage", "Downloads")
	for _, suffix := range []string{"", filepath.Join("profiles", "second")} {
		dir := DefaultEnvironmentDir(filepath.Join(profile, suffix))
		if want := filepath.Join(downloads, suffix); dir != want {
			t.Errorf("download directory = %q, want %q", dir, want)
		}
	}
}

func TestEnvironmentIdentitySurvivesProjectSelectionAndLegacyUpgrade(t *testing.T) {
	profile := t.TempDir()
	legacy, _ := filepath.EvalSymlinks(t.TempDir())
	prefs, _ := json.Marshal(map[string]string{"workspace": legacy})
	if err := os.WriteFile(filepath.Join(profile, "workbench.json"), prefs, 0600); err != nil {
		t.Fatal(err)
	}
	if EnvironmentDir(profile) == legacy {
		t.Fatal("selected project without an existing VM became the environment")
	}
	vmDir := filepath.Join(profile, "state", "lima", InstanceName(profile, legacy))
	if err := os.MkdirAll(vmDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vmDir, "lima.yaml"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	if EnvironmentDir(profile) != legacy {
		t.Fatal("existing VM disk was not retained during upgrade")
	}
	saved, _ := json.Marshal(map[string]string{"directory": legacy})
	if err := os.WriteFile(filepath.Join(profile, "environment.json"), saved, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(profile, "workbench.json"), []byte(`{"workspace":"/different-project"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if EnvironmentDir(profile) != legacy {
		t.Fatal("changing project changed the environment VM")
	}
}
