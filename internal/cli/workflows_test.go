package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanmcmahon101/AgentBrief/internal/brief"
)

func workflowRepo() map[string]string {
	return map[string]string{
		"package.json":       `{"name":"custom","packageManager":"npm@11.16.0","scripts":{"build":"tsc","build:cached":"tsc","test":"vitest run","test:focused":"vitest run","check":"tsc --noEmit"},"devDependencies":{"vitest":"1"}}`,
		"AGENTS.md":          "# Existing guidance\nKeep this text.\n",
		"scripts/website.py": "# static input\n",
		"website/index.html": "<html></html>\n",
		".agentbrief.yaml": `version: 1
workflows:
  - scope: .
    commands:
      - kind: build
        run: npm run build:cached
      - kind: test-single
        run: npm run test:focused -- <file>
  - scope: website
    commands:
      - kind: check
        run: python3 scripts/website.py check
        dir: .
generated:
  - skills/**
  - docs/public/product-catalog.json
`,
	}
}

func workflowBrief(t *testing.T, root, scope string) brief.Brief {
	t.Helper()
	code, out, errOut := run(t, "brief", scope, "--json", "-C", root)
	if code != 0 || errOut != "" {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	var b brief.Brief
	if err := json.Unmarshal([]byte(out), &b); err != nil {
		t.Fatal(err)
	}
	return b
}

func workflowCommand(t *testing.T, b brief.Brief, kind string) brief.Command {
	t.Helper()
	for _, c := range b.Commands {
		if c.Kind == kind {
			return c
		}
	}
	t.Fatalf("missing %s: %+v", kind, b.Commands)
	return brief.Command{}
}

func TestWorkflowBriefSyncAndDrift(t *testing.T) {
	root := mkrepo(t, workflowRepo())
	b := workflowBrief(t, root, ".")
	for kind, want := range map[string]string{"build": "npm run build:cached", "test-single": "npm run test:focused -- <file>"} {
		c := workflowCommand(t, b, kind)
		if c.Run != want || !c.Preferred || c.Verified || c.Source == nil || c.Source.Path != ".agentbrief.yaml" || c.Source.Line == 0 || len(c.Evidence) != 1 || c.Evidence[0] != "maintainer" {
			t.Errorf("%s: %+v", kind, c)
		}
	}
	for _, pattern := range []string{"skills/**", "docs/public/product-catalog.json"} {
		found := false
		for _, rule := range b.DoNotEdit {
			found = found || rule.Pattern == pattern
		}
		if !found {
			t.Errorf("missing generated rule %s", pattern)
		}
	}
	web := workflowBrief(t, root, "website/index.html")
	c := workflowCommand(t, web, "check")
	if web.Scope != "website" || web.Project != nil || c.Run != "python3 scripts/website.py check" || c.Dir != "." {
		t.Fatalf("website: %+v %+v", web, c)
	}
	if workflowCommand(t, b, "check").Run == c.Run {
		t.Fatal("nested policy leaked into root")
	}
	_, md, _ := run(t, "brief", "website", "-C", root)
	if !strings.Contains(md, "- Scope: \x60website\x60") || !strings.Contains(md, "execution unverified") {
		t.Fatal(md)
	}

	agent := filepath.Join(root, "AGENTS.md")
	original, _ := os.ReadFile(agent)
	if code, _, errOut := run(t, "sync", "-C", root); code != 0 {
		t.Fatal(errOut)
	}
	synced, _ := os.ReadFile(agent)
	if !strings.HasPrefix(string(synced), string(original)) || !strings.Contains(string(synced), "npm run test:focused") {
		t.Fatal(string(synced))
	}
	first, _ := os.Stat(agent)
	if code, out, _ := run(t, "sync", "-C", root); code != 0 || !strings.Contains(out, "unchanged") {
		t.Fatal(out)
	}
	second, _ := os.Stat(agent)
	if !first.ModTime().Equal(second.ModTime()) {
		t.Fatal("idempotent sync rewrote file")
	}
	if code, out, errOut := run(t, "check", "--strict", "--json", "-C", root); code != 0 || !strings.Contains(out, `"workflows_checked": 2`) {
		t.Fatalf("%d %s %s", code, out, errOut)
	}

	// The chosen script remains checked even though managed Markdown is skipped.
	pkg := filepath.Join(root, "package.json")
	data, _ := os.ReadFile(pkg)
	if err := os.WriteFile(pkg, []byte(strings.ReplaceAll(string(data), `"build:cached"`, `"build:new"`)), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, _ := run(t, "check", "--json", "-C", root)
	if code != 1 || !strings.Contains(out, "unknown-script") || !strings.Contains(out, `"path": ".agentbrief.yaml"`) {
		t.Fatalf("%d %s", code, out)
	}
	if err := os.WriteFile(pkg, data, 0o644); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, ".agentbrief.yaml")
	policy, _ := os.ReadFile(config)
	if err := os.WriteFile(config, []byte(strings.ReplaceAll(string(policy), "npm run build:cached", "npm run build")), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := run(t, "check", "-C", root); code != 1 || !strings.Contains(out, "stale-block") {
		t.Fatalf("%d %s", code, out)
	}
}

func TestWorkflowScopePrecedence(t *testing.T) {
	files := workflowRepo()
	files["package.json"] = `{"name":"root","workspaces":["packages/*"],"scripts":{"test":"vitest","root:test":"vitest"}}`
	files["packages/api/package.json"] = `{"name":"api","scripts":{"test":"vitest"}}`
	files["packages/api/src/index.ts"] = ""
	files["packages/api/src/deep/index.ts"] = ""
	files["packages/apian/index.ts"] = ""
	files[".agentbrief.yaml"] = `version: 1
workflows:
  - commands:
      - kind: test
        run: npm run root:test
  - scope: packages/api/src
    commands:
      - kind: test
        run: npm run root:test -- src
        dir: .
  - scope: packages/api/src/deep
    commands:
      - kind: test
        run: npm run root:test -- deep
`
	root := mkrepo(t, files)
	cases := []struct {
		scope, want string
		preferred   bool
	}{
		{".", "npm run root:test", true},
		{"packages/api", "npm test", false},
		{"packages/api/src", "npm run root:test -- src", true},
		{"packages/api/src/deep/index.ts", "npm run root:test -- deep", true},
		{"packages/apian", "npm run root:test", true},
	}
	for _, tc := range cases {
		t.Run(tc.scope, func(t *testing.T) {
			c := workflowCommand(t, workflowBrief(t, root, tc.scope), "test")
			if c.Run != tc.want || c.Preferred != tc.preferred {
				t.Fatalf("%+v", c)
			}
		})
	}
}

func TestWorkflowCheckUsesCommandDirectory(t *testing.T) {
	files := map[string]string{
		"package.json":              `{"name":"root","workspaces":["packages/*"],"scripts":{"check:root":"echo root"}}`,
		"packages/api/package.json": `{"name":"api","scripts":{"check:api":"echo api"}}`,
		"packages/api/validate.py":  "",
		"Makefile":                  "root-check:\n\ttrue\n",
		"packages/api/Makefile":     "api-check:\n\ttrue\n",
	}
	cases := []struct {
		run, dir string
		broken   bool
	}{
		{"npm run check:api", "packages/api", false},
		{"npm run check:api", ".", true},
		{"npm run check:root", "packages/api", true},
		{"npm --prefix packages/api run check:api", ".", false},
		{"npm --prefix=packages/api run check:api", ".", false},
		{"python3 validate.py", "packages/api", false},
		{"make api-check", "packages/api", false},
		{"make api-check", ".", true},
	}
	for _, tc := range cases {
		t.Run(tc.run+"/"+tc.dir, func(t *testing.T) {
			files[".agentbrief.yaml"] = "version: 1\nworkflows:\n  - commands:\n      - kind: check\n        run: " + tc.run + "\n        dir: " + tc.dir + "\n"
			root := mkrepo(t, files)
			code, out, errOut := run(t, "check", "--json", "-C", root)
			if (code == 1) != tc.broken || code == 2 {
				t.Fatalf("%d %s %s", code, out, errOut)
			}
		})
	}
}

func TestInvalidWorkflowConfigCannotWriteOrLeak(t *testing.T) {
	cases := []string{
		"", "version: 2", "version: 1\nunknown: secret-value", "version: 1\nversion: 1",
		"version: 1\n---\nversion: 1", "version: [", "version: 1\nworkflows: {}",
		"version: 1\nworkflows:\n  - scope: missing\n    commands: []",
		"version: 1\nworkflows:\n  - scope: ../outside\n    commands: []",
		"version: 1\nworkflows:\n  - commands:\n      - kind: secret-value\n        run: npm test",
		"version: 1\nworkflows:\n  - commands:\n      - kind: test\n        run: npm test\n        dir: /absolute",
		"version: 1\nworkflows:\n  - commands:\n      - kind: test\n        run: npm test\n        unknown: secret-value",
		"version: 1\nworkflows:\n  - commands:\n      - kind: test\n        run: npm test\n      - kind: test\n        run: npm test",
		"version: 1\nworkflows:\n  - commands:\n      - kind: test\n        run: &cmd npm test\n      - kind: check\n        run: *cmd",
		"version: 1\nworkflows:\n  - commands:\n      - kind: test\n        run: |\n          npm test\n          secret-value",
		"version: 1\ngenerated: [../outside]",
		"version: 1\ngenerated: [/absolute]",
		"version: 1\ngenerated: [123]",
	}
	for _, config := range cases {
		t.Run(config, func(t *testing.T) {
			files := workflowRepo()
			files[".agentbrief.yaml"] = config
			root := mkrepo(t, files)
			code, out, errOut := run(t, "check", "--json", "-C", root)
			if code != 1 || !strings.Contains(out, "invalid-config") || strings.Contains(out+errOut, "secret-value") {
				t.Fatalf("%d %s %s", code, out, errOut)
			}
			agent := filepath.Join(root, "AGENTS.md")
			before, _ := os.ReadFile(agent)
			for _, command := range []string{"brief", "sync"} {
				code, out, errOut := run(t, command, "-C", root)
				if code != 2 || out != "" || !strings.Contains(errOut, "invalid .agentbrief.yaml") {
					t.Fatalf("%d %s %s", code, out, errOut)
				}
			}
			after, _ := os.ReadFile(agent)
			if string(before) != string(after) {
				t.Fatal("invalid config modified instructions")
			}
		})
	}
}

func TestWorkflowCommandsAreNeverExecuted(t *testing.T) {
	files := workflowRepo()
	files[".agentbrief.yaml"] = `version: 1
workflows:
  - commands:
      - kind: test
        run: node -e "require('fs').writeFileSync('executed', 'yes')"
`
	root := mkrepo(t, files)
	for _, command := range []string{"brief", "check", "sync"} {
		if code, out, errOut := run(t, command, "-C", root); code != 0 {
			t.Fatalf("%d %s %s", code, out, errOut)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "executed")); !os.IsNotExist(err) {
		t.Fatal("repository command was executed")
	}
}
