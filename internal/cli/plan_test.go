package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/seanmcmahon101/AgentBrief/internal/plan"
)

func planRepo() map[string]string {
	return map[string]string{
		"package.json":     `{"scripts":{"build":"tsc","test":"vitest run","docs":"typedoc","lint":"eslint ."}}`,
		"src/index.ts":     "export {}",
		"docs/guide.md":    "# Guide",
		"generated/api.ts": "// generated",
		".agentbrief.yaml": `version: 1
checks:
  - id: build
    kind: build
    run: npm run build
  - id: unit
    kind: test
    run: npm test
    paths: [src/**, package.json]
    exclude: [src/vendor/**]
    requires: [build]
  - id: docs
    kind: check
    run: npm run docs
    paths: [docs/**]
    requires: [build]
  - id: lint
    kind: lint
    run: npm run lint
    paths: ["**/*.ts"]
generated: [generated/**]
`,
	}
}

func readPlan(t *testing.T, root string, files ...string) (plan.Plan, string) {
	t.Helper()
	args := append([]string{"plan", "-C", root, "--json"}, files...)
	code, out, errOut := run(t, args...)
	if code != 0 || errOut != "" {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
	var p plan.Plan
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		t.Fatal(err)
	}
	return p, out
}

func stepIDs(p plan.Plan) []string {
	ids := []string{}
	for _, s := range p.Steps {
		ids = append(ids, s.ID)
	}
	return ids
}

func TestPlanPrerequisitesSelectionAndUnknownCoverage(t *testing.T) {
	root := mkrepo(t, planRepo())
	p, out := readPlan(t, root, "src/new.ts", "docs/deleted.md", "src/new.ts")
	if p.Schema != plan.Schema || !p.MappingComplete || p.Executed || p.Coverage != "unknown" ||
		!reflect.DeepEqual(stepIDs(p), []string{"build", "docs", "lint", "unit"}) ||
		!reflect.DeepEqual(p.Paths, []string{"docs/deleted.md", "src/new.ts"}) {
		t.Fatalf("%+v", p)
	}
	if len(p.Steps[0].SelectedBy) != 0 || !reflect.DeepEqual(p.Steps[0].RequiredBy, []string{"docs", "unit"}) {
		t.Fatal(p.Steps[0])
	}
	for _, step := range p.Steps {
		if step.Command.Source.Path != ".agentbrief.yaml" || step.Command.Source.Line == 0 ||
			step.Command.Dir != "." || !reflect.DeepEqual(step.Command.Evidence, []string{"maintainer"}) {
			t.Fatal(step)
		}
	}
	match := p.Steps[1].SelectedBy[0]
	if match.Path != "docs/deleted.md" || match.Pattern != "docs/**" || match.Source.Line == 0 {
		t.Fatal(match)
	}
	if _, again := readPlan(t, root, "docs/deleted.md", "src/new.ts"); out != again {
		t.Fatal("input order or duplicates changed output")
	}
	if code, md, _ := run(t, "plan", "src/new.ts", "-C", root); code != 0 ||
		!strings.Contains(md, "Required by: unit") || !strings.Contains(md, "coverage unverified") {
		t.Fatalf("%d %s", code, md)
	}
}

func TestPlanExclusionsAreLocalAndDoNotHideUnmappedFiles(t *testing.T) {
	root := mkrepo(t, planRepo())
	p, _ := readPlan(t, root, "src/vendor/foreign.ts", "unknown.txt", "generated/api.ts")
	if !reflect.DeepEqual(stepIDs(p), []string{"lint"}) ||
		!reflect.DeepEqual(p.UnmatchedPaths, []string{"unknown.txt"}) ||
		p.MappingComplete || len(p.ProtectedPaths) != 1 || p.ProtectedPaths[0].Rule.Source.Line == 0 {
		t.Fatalf("%+v", p)
	}
	code, out, _ := run(t, "plan", "unknown.txt", "--strict", "--json", "-C", root)
	if code != 1 || !strings.Contains(out, "unknown.txt") {
		t.Fatalf("%d %s", code, out)
	}
	p, _ = readPlan(t, root, ".agentbrief.yaml")
	if !p.PolicyChanged {
		t.Fatal(p)
	}
	// No configuration never becomes a guessed validation plan.
	empty := mkrepo(t, map[string]string{"go.mod": "module example.com/repo\ngo 1.23\n"})
	p, _ = readPlan(t, empty, "deleted.go")
	if len(p.Steps) != 0 || p.MappingComplete || p.Executed {
		t.Fatalf("%+v", p)
	}
}

func TestPlanFileListsPreserveNamesAndRenameSides(t *testing.T) {
	root := mkrepo(t, planRepo())
	var out, errOut bytes.Buffer
	code := mainWithInput([]string{"agentbrief", "plan", "--files-from", "-", "--null", "--json", "-C", root},
		strings.NewReader("src/old.ts\x00src/new name.ts\x00src/λ.ts\x00"), &out, &errOut)
	var p plan.Plan
	if code != 0 || json.Unmarshal(out.Bytes(), &p) != nil || len(p.Paths) != 3 || !p.MappingComplete {
		t.Fatalf("%d %s %s", code, out.String(), errOut.String())
	}
	list := filepath.Join(t.TempDir(), "files.txt")
	if err := os.WriteFile(list, []byte("docs/old.md\r\ndocs/new.md\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, errOut := run(t, "plan", "--files-from", list, "--json", "-C", root); code != 0 ||
		errOut != "" || !strings.Contains(out, "docs/old.md") || !strings.Contains(out, "docs/new.md") {
		t.Fatalf("%d %s %s", code, out, errOut)
	}
}

func TestPlanRefusesMalformedOrDriftedPolicyAtomically(t *testing.T) {
	base := "version: 1\nchecks:\n"
	check := "  - id: unit\n    kind: test\n    run: npm test\n"
	for _, config := range []string{
		base + check + "    requires: [missing]\n",
		base + check + "    requires: [unit]\n",
		base + check + "    requires: [other]\n  - id: other\n    kind: build\n    run: npm run build\n    requires: [unit]\n",
		base + check + check,
		base + check + "    paths: [../outside]\n",
		base + check + "    paths: [src/**tail]\n",
		base + check + "    paths: [/src/**]\n",
		base + check + "    paths: []\n",
		base + check + "    paths: [true]\n",
		base + check + "    exclude: [src/**]\n",
		base + check + "    requires: [build, build]\n",
		base + check + "    unknown: value\n",
		base + strings.ReplaceAll(check, "npm test", "npm run gone"),
		base + strings.ReplaceAll(check, "npm test", "python3 scripts/gone.py"),
	} {
		t.Run(config, func(t *testing.T) {
			files := planRepo()
			files[".agentbrief.yaml"] = config
			root := mkrepo(t, files)
			for _, command := range []string{"plan", "brief", "sync"} {
				args := []string{command, "-C", root}
				if command == "plan" {
					args = append(args, "src/index.ts")
				}
				code, out, errOut := run(t, args...)
				if code != 2 || out != "" || !strings.Contains(errOut, "agentbrief check") {
					t.Fatalf("%s: %d %s %s", command, code, out, errOut)
				}
			}
			if code, out, _ := run(t, "check", "--json", "-C", root); code != 1 ||
				!strings.Contains(out, ".agentbrief.yaml") {
				t.Fatalf("%d %s", code, out)
			}
		})
	}
}

func TestPlanCommandsHaveExactWorkingDirectoryAndAreNotExecuted(t *testing.T) {
	files := planRepo()
	files["nested/package.json"] = `{"scripts":{"only-here":"node sentinel.js"}}`
	files["nested/sentinel.js"] = "require('fs').writeFileSync('EXECUTED', 'bad')"
	files[".agentbrief.yaml"] = `version: 1
checks:
  - id: nested
    kind: test
    run: npm run only-here
    dir: nested
    paths: [nested/**]
`
	root := mkrepo(t, files)
	p, _ := readPlan(t, root, "nested/sentinel.js")
	if len(p.Steps) != 1 || p.Steps[0].Command.Dir != "nested" {
		t.Fatal(p)
	}
	if _, err := os.Stat(filepath.Join(root, "nested", "EXECUTED")); !os.IsNotExist(err) {
		t.Fatal("planner executed repository command")
	}
	if code, out, _ := run(t, "check", "--strict", "--json", "-C", root); code != 0 ||
		!strings.Contains(out, `"checks_checked": 1`) {
		t.Fatalf("%d %s", code, out)
	}
	// A script existing only at root must not validate the nested command.
	if err := os.WriteFile(filepath.Join(root, "nested", "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := run(t, "check", "--json", "-C", root); code != 1 || !strings.Contains(out, "unknown-script") {
		t.Fatalf("%d %s", code, out)
	}
}

func TestPlanInputValidationHelpAndReadFailures(t *testing.T) {
	root := mkrepo(t, planRepo())
	for _, file := range []string{"../x", "a/../../x", "/tmp/x", "C:/x", ".", "a\nb", "a\x00b", "\xff"} {
		if code, out, errOut := run(t, "plan", file, "-C", root); code != 2 || out != "" || errOut == "" {
			t.Fatalf("%q: %d %s %s", file, code, out, errOut)
		}
	}
	for _, args := range [][]string{{"plan"}, {"plan", "--null"}, {"plan", "--files-from", "does-not-exist"}} {
		args = append(args, "-C", root)
		if code, out, _ := run(t, args...); code != 2 || out != "" {
			t.Fatalf("%v: %d %s", args, code, out)
		}
	}
	for _, args := range [][]string{{"plan", "-h"}, {"help", "plan"}, {"-h", "plan"}} {
		if code, out, _ := run(t, args...); code != 0 || !strings.Contains(out, "agentbrief plan") {
			t.Fatalf("%v: %d %s", args, code, out)
		}
	}
	for _, reader := range []io.Reader{strings.NewReader(strings.Repeat("x", maxPlanInput+1)), brokenList{}} {
		var out, errOut bytes.Buffer
		code := mainWithInput([]string{"agentbrief", "plan", "--files-from", "-", "-C", root}, reader, &out, &errOut)
		if code != 2 || out.Len() != 0 || errOut.Len() == 0 {
			t.Fatalf("%d %s %s", code, out.String(), errOut.String())
		}
	}
}

type brokenList struct{}

func (brokenList) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// Unknown tools and legitimate generated paths remain unresolved rather than
// being falsely classified as broken commands.
func TestPlanDoesNotInventFailuresForUnresolvedCommands(t *testing.T) {
	files := planRepo()
	files[".agentbrief.yaml"] = `version: 1
checks:
  - id: dynamic
    kind: test
    run: uv run project-specific-tool
    paths: [src/**]
  - id: generated
    kind: check
    run: python3 build/generated_check.py
    paths: [src/**]
`
	root := mkrepo(t, files)
	p, _ := readPlan(t, root, "src/new.ts")
	if len(p.Steps) != 2 || p.Coverage != "unknown" || p.Executed {
		t.Fatal(p)
	}
}

func TestPlanScriptPathsResolveOnlyAtDeclaredDirectory(t *testing.T) {
	files := planRepo()
	files["nested/README.md"] = "# Nested"
	files["scripts/check.py"] = "# root script"
	files[".gitignore"] = "scripts/absent.py\n"
	files[".agentbrief.yaml"] = `version: 1
checks:
  - id: nested
    kind: check
    run: python3 scripts/check.py
    dir: nested
    paths: [nested/**]
`
	root := mkrepo(t, files)
	if code, out, _ := run(t, "check", "--json", "-C", root); code != 1 ||
		!strings.Contains(out, "missing-path") {
		t.Fatalf("root script validated a nested command: %d %s", code, out)
	}
	config := strings.Replace(files[".agentbrief.yaml"], "scripts/check.py", "scripts/absent.py", 1)
	if err := os.WriteFile(filepath.Join(root, ".agentbrief.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out, _ := run(t, "check", "--json", "-C", root); code != 1 ||
		!strings.Contains(out, "missing-path") {
		t.Fatalf("root ignore rule excused a nested missing script: %d %s", code, out)
	}
}
