package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKnownBrokenWorkflowCannotGenerateInstructions(t *testing.T) {
	files := workflowRepo()
	files[".agentbrief.yaml"] = "version: 1\nworkflows:\n  - commands:\n      - kind: build\n        run: npm run missing:wrapper\n"
	root := mkrepo(t, files)
	for _, command := range []string{"brief", "sync"} {
		code, out, errOut := run(t, command, "-C", root)
		if code != 2 || out != "" || !strings.Contains(errOut, "agentbrief check") {
			t.Fatalf("%d %s %s", code, out, errOut)
		}
	}
	contents, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil || string(contents) != files["AGENTS.md"] {
		t.Fatal("broken workflow changed instructions")
	}
}

func TestWorkflowConfigurationSizeAndDuplicateScopes(t *testing.T) {
	for _, config := range []string{
		"version: 1\n# " + strings.Repeat("x", 1<<20),
		"version: 1\nworkflows:\n  - scope: .\n    commands:\n      - kind: test\n        run: npm test\n  - scope: .\n    commands:\n      - kind: check\n        run: npm run check\n",
		"version: 1\nworkflows:\n  - commands:\n      - kind: test\n        run: " + strings.Repeat("x", 161) + "\n",
	} {
		files := workflowRepo()
		files[".agentbrief.yaml"] = config
		root := mkrepo(t, files)
		code, out, _ := run(t, "check", "--json", "-C", root)
		if code != 1 || !strings.Contains(out, "invalid-config") {
			t.Fatalf("%d %.300s", code, out)
		}
	}
}
