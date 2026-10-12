package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// Exercise the actual composite-action install block, with go replaced by a
// shell function. No installation, network access or scanned code is executed.
func TestActionInstallPreservesRequestedRevision(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("composite action requires Bash; exercised by Unix CI")
	}
	_, file, _, _ := runtime.Caller(0)
	data, err := os.ReadFile(filepath.Join(filepath.Dir(file), "..", "..", "action.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var action struct {
		Runs struct {
			Steps []struct{ Name, Run string }
		}
	}
	if err := yaml.Unmarshal(data, &action); err != nil {
		t.Fatal(err)
	}
	script := ""
	for _, step := range action.Runs.Steps {
		if step.Name == "Install agentbrief" {
			script = step.Run
		}
	}
	if script == "" {
		t.Fatal("missing composite install step")
	}
	sha := strings.Repeat("a", 40)
	for _, tc := range []struct {
		name, version, ref, want, proxy string
		invalid                         bool
	}{
		{"tag", "v0.1.0", "", "v0.1.0", "https://proxy.golang.org", false},
		{"commit", sha, "", sha, "https://proxy.golang.org", false},
		{"action commit", "", sha, sha, "https://proxy.golang.org", false},
		{"main", "main", "", "main", "direct", false},
		{"action main", "", "main", "main", "direct", false},
		{"local default", "", "", "latest", "https://proxy.golang.org", false},
		{"explicit override", sha, "v0.1.0", sha, "https://proxy.golang.org", false},
		{"unknown ref", "codex/branch", "", "", "", true},
		{"invalid shell text", "$(touch SHOULD_NOT_EXIST)", "", "", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(bash, "-c", "go() { printf '%s\\n' \"$GOPROXY\" \"$@\"; }\n"+script)
			cmd.Dir = t.TempDir()
			cmd.Env = append(os.Environ(), "VERSION="+tc.version, "ACTION_REF="+tc.ref,
				"GOPROXY=https://proxy.golang.org")
			out, err := cmd.CombinedOutput()
			if tc.invalid {
				if err == nil || !strings.Contains(string(out), "Unsupported AgentBrief version") {
					t.Fatalf("%s %v", out, err)
				}
				return
			}
			want := fmt.Sprintf("%s\ninstall\ngithub.com/seanmcmahon101/AgentBrief/cmd/agentbrief@%s\n", tc.proxy, tc.want)
			if err != nil || string(out) != want {
				t.Fatalf("got %q, want %q: %v", out, want, err)
			}
		})
	}
}
