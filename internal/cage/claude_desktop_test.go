package cage

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestDesktopCallbackFocusAfterHandoff(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node unavailable")
	}
	for _, identity := range []string{"cage", "cage-browser"} {
		for _, tc := range []struct {
			name, url, path, login string
			fail, parameter        bool
		}{
			{"callback", "antigravity://login?code=private-test-value", "/desktop-focus", "", false, false},
			{"failed-callback", "antigravity://login?code=private-test-value", "", "", true, false},
			{"web-link", "https://example.com/login", "/new", "", false, false},
			{"cli-login", "https://claude.ai/oauth/authorize?state=private-test-value", "/login-open", "1234567890abcdef", false, false},
			{"finish-cli-login", "--finish-login", "/login-finish", "1234567890abcdef", false, false},
			{"cli-login-argument", "https://claude.ai/oauth/authorize?state=private-test-value", "/login-open", strings.Repeat("a", 24), false, true},
			{"finish-cli-login-argument", "--finish-login", "/login-finish", strings.Repeat("a", 24), false, true},
		} {
			t.Run(identity+"/"+tc.name, func(t *testing.T) {
				dir := t.TempDir()
				type request struct{ Path, Auth, URL, Login string }
				requests := make(chan request, 2)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body struct {
						URL   string `json:"url"`
						Login string `json:"login"`
					}
					_ = json.NewDecoder(r.Body).Decode(&body)
					requests <- request{r.URL.Path, r.Header.Get("Authorization"), body.URL, body.Login}
					w.Write([]byte(`{"ok":true}`))
				}))
				defer server.Close()
				config := filepath.Join(dir, "control.json")
				if err := os.WriteFile(config, []byte(`{"token":"test-only"}`), 0600); err != nil {
					t.Fatal(err)
				}
				// Preserve the production shell flow; sudo only substitutes the two
				// guest identities and the app. Node performs the real HTTP request.
				sudo := "#!/bin/sh\nshift\nuser=$1\nshift\nif [ \"$user\" = cage ]; then [ \"$#\" = 1 ] && [ \"$(cat)\" = \"antigravity://login?code=private-test-value\" ] || exit 98; exit \"$CALLBACK_EXIT\"; fi\nif [ \"$TEST_IDENTITY\" = cage-browser ]; then echo 'sudo denied' >&2; exit 1; fi\nshift\nexec \"$TEST_NODE\" \"$@\"\n"
				if err := os.WriteFile(filepath.Join(dir, "sudo"), []byte(sudo), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "id"), []byte("#!/bin/sh\nprintf '%s\\n' \"$TEST_IDENTITY\"\n"), 0755); err != nil {
					t.Fatal(err)
				}
				script := strings.ReplaceAll(guestOpen, "/etc/claude-cage/browser-control.json", config)
				script = strings.ReplaceAll(script, "http://127.0.0.1:18500", server.URL)
				script = strings.ReplaceAll(script, "/usr/bin/node", "\"$TEST_NODE\"")
				code := "0"
				if tc.fail {
					code = "1"
				}
				args := []string{"-c", script, "open"}
				login := tc.login
				if tc.parameter {
					args = append(args, "--login-id", tc.login)
					login = ""
				}
				args = append(args, tc.url)
				cmd := exec.Command("sh", args...)
				cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"), "TEST_NODE="+node, "TEST_IDENTITY="+identity, "CALLBACK_EXIT="+code, "CAGE_CLAUDE_LOGIN="+login)
				out, err := cmd.CombinedOutput()
				if tc.fail {
					if err == nil {
						t.Fatal("failed handoff reported success")
					}
					select {
					case r := <-requests:
						t.Fatalf("failed handoff emitted focus: %+v", r)
					default:
					}
					return
				}
				if err != nil {
					t.Fatalf("%v: %s", err, out)
				}
				select {
				case r := <-requests:
					wantURL := ""
					if tc.path == "/new" || tc.path == "/login-open" {
						wantURL = tc.url
					}
					if r.Path != tc.path || r.Auth != "Bearer test-only" || r.URL != wantURL || r.Login != tc.login {
						t.Fatalf("unexpected request: %+v", r)
					}
				default:
					t.Fatal("handoff notification missing")
				}
			})
		}
	}
}

func TestGeminiRegistersBrowserCallback(t *testing.T) {
	script := ClaudeDesktopProvisionScript()
	for _, need := range []string{"MimeType=x-scheme-handler/antigravity;", "Exec=/usr/local/bin/cage-open %u", "xdg-mime default gemini-antigravity.desktop x-scheme-handler/antigravity"} {
		if !strings.Contains(script, need) {
			t.Errorf("missing in installed desktop provision: %s", need)
		}
	}
}

func TestTailnetBrowserOpenPassesOnlyReturnSignal(t *testing.T) {
	bin := t.TempDir()
	helper := filepath.Join(bin, "return-helper")
	if err := os.WriteFile(helper, []byte("#!/bin/sh\nprintf '%s\\n' \"$@\""), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "sudo"), []byte("#!/bin/sh\nshift 4\nexec \"$@\""), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, "id"), []byte("#!/bin/sh\nprintf '%s' \"$TEST_IDENTITY\""), 0755); err != nil {
		t.Fatal(err)
	}
	script := strings.ReplaceAll(guestOpen, "/usr/local/bin/cage-tailnet", helper)
	for _, identity := range []string{"cage", "cage-browser"} {
		cmd := exec.Command("sh", "-c", script, "open", "tailscale://return?code=private-fixture")
		cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin", "TEST_IDENTITY="+identity)
		if out, err := cmd.CombinedOutput(); err != nil || string(out) != "--browser-return\n" {
			t.Fatalf("unexpected handoff for %s: %q %v", identity, out, err)
		}
	}
}

func TestAntigravityInstallerRejectsWrongArchiveBeforeExtraction(t *testing.T) {
	dir := t.TempDir()
	commands := map[string]string{
		"sudo":      "#!/bin/sh\nif [ \"$1\" = env ]; then exec \"$@\"; fi\necho premature-privileged-write >&2; exit 97\n",
		"apt-get":   "#!/bin/sh\nexit 0\n",
		"apt-cache": "#!/bin/sh\nexit 1\n",
		"systemctl": "#!/bin/sh\nexit 3\n",
		"flock":     "#!/bin/sh\nexit 0\n",
		"dpkg":      "#!/bin/sh\necho arm64\n",
	}
	if _, err := exec.LookPath("sha256sum"); err != nil {
		commands["sha256sum"] = "#!/bin/sh\nexec shasum -a 256 \"$@\"\n"
	}
	for name, body := range commands {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0755); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command("bash", "-s")
	cmd.Stdin = strings.NewReader("cage_curl_download() { printf corrupt > \"$1\"; }\n" + claudeDesktopInstall)
	cmd.Env = append(os.Environ(), "HOME="+dir, "PATH="+dir+":"+os.Getenv("PATH"), "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if err == nil || !strings.Contains(string(out), "FAILED") || strings.Contains(string(out), "premature-privileged-write") {
		t.Fatalf("wrong archive not rejected before privileged extraction: %v: %s", err, out)
	}
}

func TestClaudeDesktopScriptsParse(t *testing.T) {
	for name, script := range map[string]string{"install": ClaudeDesktopInstallScript(), "provision": ClaudeDesktopProvisionScript(), "session": claudeDesktopSession, "start": claudeDesktopStart, "open": claudeDesktopOpen, "browser-open": guestOpen} {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command("bash", "-n")
			cmd.Stdin = strings.NewReader(script)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
		})
	}
}
