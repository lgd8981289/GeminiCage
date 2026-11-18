package workbench

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAntigravityTerminalStartsInstalledCLIInSelectedProject(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "bin")
	project := filepath.Join(home, "Gemini's first project")
	for _, dir := range []string{bin, project} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// A local CLI reports its working directory, with no installer or OAuth needed.
	if err := os.WriteFile(filepath.Join(bin, "agy"), []byte("#!/bin/sh\npwd -P\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	command := exec.Command("/bin/sh", "-c", antigravityTerminalScript(project))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("launch: %v: %s", err, output)
	}
	want, err := filepath.EvalSymlinks(project)
	if err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(home, "removed-project")
	if err := exec.Command("/bin/sh", "-c", antigravityTerminalScript(missing)).Run(); err == nil {
		t.Fatal("terminal started outside the selected project")
	}
	if got := strings.TrimSpace(string(output)); got != want {
		t.Errorf("CLI opened in %q, want %q", got, want)
	}
}
