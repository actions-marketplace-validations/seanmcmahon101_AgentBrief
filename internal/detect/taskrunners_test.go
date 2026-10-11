package detect

import (
	"strings"
	"testing"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

// ---- helpers shared by the tests of this file's sibling detectors ----

// detectWith runs just the given detectors over an in-memory repo.
func detectWith(files map[string]string, fns ...Detector) *Facts {
	r := scan.FromMap(files)
	f := &Facts{Repo: &model.Repo{}}
	for _, fn := range fns {
		fn(r, f)
	}
	return f
}

func cmdKeys(f *Facts) map[string]model.Command {
	m := map[string]model.Command{}
	for _, c := range f.Commands {
		m[c.Dir+"|"+c.Kind+"|"+c.Run] = c
	}
	return m
}

func keyList(m map[string]model.Command) string {
	var ks []string
	for k := range m {
		ks = append(ks, "  "+k)
	}
	return strings.Join(ks, "\n")
}

// needCmds asserts each "dir|kind|run" exists.
func needCmds(t *testing.T, f *Facts, want ...string) {
	t.Helper()
	got := cmdKeys(f)
	for _, w := range want {
		if _, ok := got[w]; !ok {
			t.Errorf("missing command %q; have:\n%s", w, keyList(got))
		}
	}
}

func forbidCmds(t *testing.T, f *Facts, unwanted ...string) {
	t.Helper()
	got := cmdKeys(f)
	for _, w := range unwanted {
		if _, ok := got[w]; ok {
			t.Errorf("unexpected command %q", w)
		}
	}
}

func findTask(f *Facts, runner, name string) *model.Task {
	for i, tk := range f.Tasks {
		if tk.Runner == runner && tk.Name == name {
			return &f.Tasks[i]
		}
	}
	return nil
}

func taskNames(f *Facts, runner string) string {
	var ns []string
	for _, tk := range f.Tasks {
		if tk.Runner == runner {
			ns = append(ns, tk.Name)
		}
	}
	return strings.Join(ns, ",")
}

func hasToolchain(f *Facts, name, version string) bool {
	for _, tc := range f.Toolchains {
		if tc.Name == name && tc.Version == version {
			return true
		}
	}
	return false
}

// ---- Makefile ----

const trickyMakefile = `# top comment
CC := gcc
VERSION ?= 1.0
FLAGS += -O2
EMPTY =
X ::= y
export PATH := /bin:$(PATH)

.PHONY: build test lint clean help
.DEFAULT_GOAL := help

ifeq ($(OS),Windows_NT)
EXT := .exe
else
EXT :=
endif

define HELPTEXT
this: looks like a rule
	and a recipe
endef

build: ## Build the thing
	@echo building
	go build -o bin/app ./cmd/app
	echo second

test: build
	go test ./...

lint fmt: ## lint and format
	golangci-lint run

%.o: %.c
	$(CC) -c $<

$(BIN): main.go
	touch $@

bin/app: main.go
	go build -o $@

target-var: CFLAGS = -g
dist.tar.gz:
	tar czf $@ .

clean::
	rm -rf bin
help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST)
check: ; go vet ./...
`

func TestMakefileTargets(t *testing.T) {
	f := detectWith(map[string]string{"Makefile": trickyMakefile}, detectTaskRunners)
	for _, name := range []string{"build", "test", "lint", "fmt", "clean", "help", "check", "bin/app", "dist.tar.gz"} {
		if findTask(f, "make", name) == nil {
			t.Errorf("missing make task %q; have %s", name, taskNames(f, "make"))
		}
	}
	for _, name := range []string{"CC", "VERSION", "FLAGS", "EMPTY", "X", "PATH", "this", "%.o", "$(BIN)", "target-var", ".PHONY", ".DEFAULT_GOAL", "EXT", "and"} {
		if findTask(f, "make", name) != nil {
			t.Errorf("unexpected make task %q", name)
		}
	}
	if tk := findTask(f, "make", "build"); tk == nil || tk.Desc != "Build the thing" || tk.Body != "go build -o bin/app ./cmd/app" || tk.Source.Line != 23 {
		t.Errorf("build task = %+v", tk)
	}
	if tk := findTask(f, "make", "check"); tk == nil || tk.Body != "go vet ./..." {
		t.Errorf("inline recipe: %+v", tk)
	}
	if tk := findTask(f, "make", "fmt"); tk == nil || tk.Desc != "lint and format" {
		t.Errorf("multi-target: %+v", tk)
	}
	needCmds(t, f, ".|build|make build", ".|test|make test", ".|lint|make lint", ".|format|make fmt", ".|check|make check")
	forbidCmds(t, f, ".|build|make bin/app", ".|build|make dist.tar.gz")
	if c := cmdKeys(f)[".|test|make test"]; len(c.Evidence) != 1 || c.Evidence[0] != model.EvidenceDeclared {
		t.Errorf("evidence = %v", c.Evidence)
	}
}

func TestMakefileNonPhonyShadowedByDirectory(t *testing.T) {
	files := map[string]string{
		"Makefile":       "test:\n\tgo test ./...\nbuild:\n\tgo build\n",
		"test/a_test.go": "package x",
	}
	f := detectWith(files, detectTaskRunners)
	forbidCmds(t, f, ".|test|make test")
	needCmds(t, f, ".|build|make build")
	if findTask(f, "make", "test") == nil {
		t.Error("task test should still be listed")
	}
	files["Makefile"] = ".PHONY: test\n" + files["Makefile"]
	f = detectWith(files, detectTaskRunners)
	needCmds(t, f, ".|test|make test")
}

func TestMakefileVariantsAndIncludes(t *testing.T) {
	files := map[string]string{
		"GNUmakefile":  "include mk/common.mk\n-include missing.mk\ninclude $(X)/y.mk\nall: build\n",
		"Makefile":     "ignored:\n\ttrue\n",
		"mk/common.mk": "build:\n\tcc main.c\nlint:\n\tcppcheck .\n",
	}
	f := detectWith(files, detectTaskRunners)
	if findTask(f, "make", "ignored") != nil {
		t.Error("GNUmakefile must take precedence over Makefile")
	}
	tk := findTask(f, "make", "build")
	if tk == nil || tk.Source.Path != "mk/common.mk" || tk.Source.Line != 1 {
		t.Fatalf("included target: %+v", tk)
	}
	needCmds(t, f, ".|build|make build", ".|lint|make lint")
}

func TestMakefileInProjectDir(t *testing.T) {
	files := map[string]string{
		"go.mod":         "module a\n",
		"svc/go.mod":     "module a/svc\n",
		"svc/Makefile":   "test:\n\tgo test ./...\n",
		"other/Makefile": "test:\n\ttrue\n",
	}
	f := detectWith(files, detectGo, detectTaskRunners)
	needCmds(t, f, "svc|test|make test")
	forbidCmds(t, f, "other|test|make test") // not a project dir
}

func TestMakefileContinuationsAndNoise(t *testing.T) {
	mk := "build: a \\\n  b\n\t$(info hello)\n\t@echo hi\n\tgcc \\\n\t  -o x\nlist = a \\\n b\n"
	f := detectWith(map[string]string{"Makefile": mk}, detectTaskRunners)
	tk := findTask(f, "make", "build")
	if tk == nil || tk.Body != "gcc -o x" {
		t.Fatalf("task = %+v", tk)
	}
	if findTask(f, "make", "list") != nil {
		t.Error("continued assignment parsed as a target")
	}
}

// ---- justfile ----

const trickyJustfile = `set shell := ["bash", "-c"]
import 'other.just'
mod foo
alias t := test
export RUST_LOG := "info"
version := "1.0"

default:
    @just --list

# Build everything
build:
    cargo build

[private]
helper:
    echo hi

_hidden:
    echo hidden

@test *args='':
    cargo test {{args}}

lint file:
    ruff check {{file}}

fmt target='.':
    cargo fmt

[linux]
dev:
    ./run.sh

[macos]
dev:
    ./run-mac.sh

script:
    #!/usr/bin/env bash
    echo multi
`

func TestJustfile(t *testing.T) {
	f := detectWith(map[string]string{"justfile": trickyJustfile}, detectTaskRunners)
	for _, n := range []string{"default", "build", "helper", "_hidden", "test", "lint", "fmt", "dev", "script"} {
		if findTask(f, "just", n) == nil {
			t.Errorf("missing just task %q; have %s", n, taskNames(f, "just"))
		}
	}
	for _, n := range []string{"version", "alias", "t", "set", "import", "mod", "RUST_LOG", "export"} {
		if findTask(f, "just", n) != nil {
			t.Errorf("unexpected just task %q", n)
		}
	}
	if tk := findTask(f, "just", "build"); tk == nil || tk.Desc != "Build everything" || tk.Body != "cargo build" {
		t.Errorf("build = %+v", tk)
	}
	if tk := findTask(f, "just", "script"); tk == nil || tk.Body != "" {
		t.Errorf("shebang recipe body should be empty: %+v", tk)
	}
	n := 0
	for _, tk := range f.Tasks {
		if tk.Name == "dev" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("OS-specific duplicates should collapse, got %d", n)
	}
	needCmds(t, f, ".|build|just build", ".|test|just test", ".|format|just fmt", ".|dev|just dev")
	forbidCmds(t, f, ".|lint|just lint") // requires a positional argument
	forbidCmds(t, f, ".|build|just helper", ".|build|just _hidden")
}

func TestJustfileNames(t *testing.T) {
	for _, name := range []string{"justfile", "Justfile", ".justfile"} {
		f := detectWith(map[string]string{name: "test:\n    true\n"}, detectTaskRunners)
		needCmds(t, f, ".|test|just test")
	}
	f := detectWith(map[string]string{"justfile": "private-ish:\n    true\n[private]\ntest:\n    true\n"}, detectTaskRunners)
	forbidCmds(t, f, ".|test|just test")
}

// ---- Taskfile ----

func TestTaskfile(t *testing.T) {
	tf := `version: '3'
includes:
  docs: ./docs
  web:
    taskfile: ./web
tasks:
  default:
    cmds:
      - task: build
  build:
    desc: Build it
    cmds:
      - go build ./...
      - echo done
  test: go test ./...
  lint:
    cmd: golangci-lint run
  gen:
    internal: true
    cmds: [echo gen]
`
	f := detectWith(map[string]string{"Taskfile.yml": tf}, detectTaskRunners)
	if tk := findTask(f, "task", "build"); tk == nil || tk.Desc != "Build it" || tk.Body != "go build ./..." || tk.Source.Line != 10 {
		t.Errorf("build = %+v", tk)
	}
	if tk := findTask(f, "task", "default"); tk == nil || tk.Body != "" {
		t.Errorf("default = %+v", tk)
	}
	needCmds(t, f, ".|build|task build", ".|test|task test", ".|lint|task lint")
	forbidCmds(t, f, ".|generate|task gen")
	if findTask(f, "task", "gen") == nil {
		t.Error("internal task still belongs in the inventory")
	}
	found := false
	for _, n := range f.Notes {
		found = found || strings.Contains(n.Text, "docs, web")
	}
	if !found {
		t.Errorf("includes note missing: %+v", f.Notes)
	}
}

func TestTaskfileInvalid(t *testing.T) {
	f := detectWith(map[string]string{"Taskfile.yml": "tasks: [unclosed"}, detectTaskRunners)
	if len(f.Tasks) != 0 {
		t.Errorf("tasks = %v", f.Tasks)
	}
}

// ---- mise ----

func TestMiseTasks(t *testing.T) {
	toml := `[tools]
node = "20"

[tasks.build]
description = "Build"
run = "npm run build"

[tasks."test"]
run = ["npm test", "echo done"]

[tasks.secret]
hide = true
run = "x"

[tasks]
lint = "eslint ."
`
	files := map[string]string{
		"mise.toml":            toml,
		"mise-tasks/deploy":    "#!/usr/bin/env bash\n#MISE description=\"Deploy it\"\necho hi\n",
		"mise-tasks/test/unit": "#!/bin/sh\ntrue\n",
		".mise/tasks/.hidden":  "x",
	}
	f := detectWith(files, detectTaskRunners)
	if tk := findTask(f, "mise", "build"); tk == nil || tk.Desc != "Build" || tk.Body != "npm run build" || tk.Source.Line != 4 {
		t.Errorf("build = %+v", tk)
	}
	if tk := findTask(f, "mise", "test"); tk == nil || tk.Body != "npm test" {
		t.Errorf("test = %+v", tk)
	}
	if tk := findTask(f, "mise", "lint"); tk == nil || tk.Body != "eslint ." {
		t.Errorf("lint = %+v", tk)
	}
	if tk := findTask(f, "mise", "deploy"); tk == nil || tk.Desc != "Deploy it" {
		t.Errorf("deploy = %+v", tk)
	}
	if findTask(f, "mise", "test:unit") == nil {
		t.Errorf("nested file task: %s", taskNames(f, "mise"))
	}
	if findTask(f, "mise", ".hidden") != nil {
		t.Error("hidden file task")
	}
	needCmds(t, f, ".|build|mise run build", ".|test|mise run test", ".|lint|mise run lint")
	forbidCmds(t, f, ".|check|mise run secret")
}

// ---- toolfiles ----

func TestToolVersions(t *testing.T) {
	tv := "# comment\nnodejs 20.11.1\npython 3.12.1 3.11.7\ngolang 1.24.0\nrust stable # trailing\nruby system\njava temurin-21.0.1+12\n"
	f := detectWith(map[string]string{".tool-versions": tv}, detectToolFiles)
	for _, w := range [][2]string{{"node", "20.11.1"}, {"python", "3.12.1"}, {"go", "1.24.0"}, {"rust", "stable"}, {"java", "temurin-21.0.1+12"}} {
		if !hasToolchain(f, w[0], w[1]) {
			t.Errorf("missing toolchain %v in %+v", w, f.Toolchains)
		}
	}
	if hasToolchain(f, "ruby", "system") || len(f.Toolchains) != 5 {
		t.Errorf("toolchains = %+v", f.Toolchains)
	}
	if len(f.PackageManagers) != 0 {
		t.Error("tool files must not imply a package manager")
	}
}

func TestMiseTools(t *testing.T) {
	toml := "[tools]\nnode = \"22\"\ngo = { version = \"1.24\" }\npython = [\"3.12\", \"3.11\"]\n\"npm:prettier\" = \"latest\"\n\"cargo:ripgrep\" = \"14\"\nterraform = \"latest\"\n"
	f := detectWith(map[string]string{".config/mise/config.toml": toml}, detectToolFiles)
	for _, w := range [][2]string{{"node", "22"}, {"go", "1.24"}, {"python", "3.12"}, {"terraform", "latest"}} {
		if !hasToolchain(f, w[0], w[1]) {
			t.Errorf("missing %v in %+v", w, f.Toolchains)
		}
	}
	if len(f.Toolchains) != 4 {
		t.Errorf("backend-prefixed tools must be skipped: %+v", f.Toolchains)
	}
}
