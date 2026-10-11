package detect

import "testing"

func TestGoSingleModule(t *testing.T) {
	files := map[string]string{
		"go.mod":              "module example.com/app // comment\n\ngo 1.24.0\n\ntoolchain go1.24.3\n",
		".golangci.yml":       "linters: {}\n",
		"main.go":             "package main\n",
		"internal/gen/gen.go": "package gen\n\n//go:generate stringer -type=X\n",
		"cmd/app/main.go":     "package main\n",
		"cmd/tool/main.go":    "package main\n",
		"testdata/go.mod":     "module fixture\n",
		"vendor/modules.txt":  "# x\n",
	}
	f := detectWith(files, detectGo)
	needCmds(t, f,
		".|build|go build ./...", ".|test|go test ./...", ".|test-single|go test ./<pkg> -run '^TestName$'",
		".|lint|golangci-lint run", ".|format|gofmt -l .", ".|generate|go generate ./...",
		".|run|go run ./cmd/app", ".|run|go run ./cmd/tool")
	forbidCmds(t, f, ".|lint|go vet ./...")
	if len(f.Projects) != 1 || f.Projects[0].Name != "example.com/app" {
		t.Errorf("projects = %+v", f.Projects)
	}
	if !hasToolchain(f, "go", "1.24.0") || !hasToolchain(f, "go", "1.24.3") {
		t.Errorf("toolchains = %+v", f.Toolchains)
	}
	if c := cmdKeys(f)[".|generate|go generate ./..."]; c.Source.Path != "internal/gen/gen.go" || c.Source.Line != 3 {
		t.Errorf("generate source = %+v", c.Source)
	}
	if c := cmdKeys(f)[".|lint|golangci-lint run"]; c.Evidence[0] != "declared" || c.Source.Path != ".golangci.yml" {
		t.Errorf("lint = %+v", c)
	}
	if len(f.Generated) != 1 || f.Generated[0].Pattern != "vendor/**" || f.Generated[0].Reason != "vendored" || len(f.Notes) != 1 {
		t.Errorf("vendor: %+v %+v", f.Generated, f.Notes)
	}
}

func TestGoVetWithoutGolangciAndManyCmds(t *testing.T) {
	files := map[string]string{"go.mod": "module a\ngo 1.22\n"}
	for _, n := range []string{"a", "b", "c", "d"} {
		files["cmd/"+n+"/main.go"] = "package main\n"
	}
	f := detectWith(files, detectGo)
	needCmds(t, f, ".|lint|go vet ./...")
	for _, c := range f.Commands {
		if c.Kind == "run" {
			t.Errorf("more than 3 binaries should not emit run commands: %+v", c)
		}
		if c.Kind == "generate" {
			t.Errorf("no //go:generate present: %+v", c)
		}
	}
}

func TestGoWorkspaceAndNestedModules(t *testing.T) {
	files := map[string]string{
		"go.work":              "go 1.23\n\nuse (\n\t./api\n\t./lib // shared\n)\nuse ./tools\n",
		"api/go.mod":           "module ex/api\ngo 1.23\n",
		"lib/go.mod":           "module ex/lib\ngo 1.23\n",
		"tools/go.mod":         "module ex/tools\ngo 1.23\n",
		"standalone/go.mod":    "module ex/standalone\ngo 1.21\n",
		"examples/demo/go.mod": "module ex/demo\ngo 1.18\n",
		"lib/.golangci.toml":   "",
		"lib/x.go":             "package lib\n",
	}
	f := detectWith(files, detectGo)
	ws := map[string]string{}
	var root bool
	for _, p := range f.Projects {
		ws[p.Dir] = p.Workspace
		root = root || (p.Dir == "." && p.IsWorkspaceRoot)
	}
	for _, d := range []string{"api", "lib", "tools"} {
		if ws[d] != "." {
			t.Errorf("%s workspace = %q", d, ws[d])
		}
	}
	if w, ok := ws["standalone"]; !ok || w != "" {
		t.Errorf("standalone: %v %v", ok, w)
	}
	if _, ok := ws["examples/demo"]; !ok {
		t.Error("example module should still be a project")
	}
	if !root {
		t.Error("go.work dir should be recorded as a workspace root project")
	}
	needCmds(t, f, "api|test|go test ./...", "lib|lint|golangci-lint run", "standalone|build|go build ./...")
	forbidCmds(t, f, "examples/demo|test|go test ./...")
	if hasToolchain(f, "go", "1.18") {
		t.Error("example module toolchain should not be reported")
	}
}

func TestGoNoModule(t *testing.T) {
	f := detectWith(map[string]string{"main.go": "package main\n"}, detectGo)
	if len(f.Commands)+len(f.Projects) != 0 {
		t.Errorf("unexpected facts: %+v", f.Repo)
	}
}

func TestGoGolangciConfigInAncestor(t *testing.T) {
	f := detectWith(map[string]string{"go.mod": "module a\n", "svc/go.mod": "module a/svc\n", ".golangci.yaml": ""}, detectGo)
	needCmds(t, f, "svc|lint|golangci-lint run")
}
