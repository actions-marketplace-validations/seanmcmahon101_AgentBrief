package detect

import (
	"testing"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

// finalizeWith seeds commands and CI steps, runs finalizeCI and returns the facts.
func finalizeWith(cmds []model.Command, steps ...model.CIStep) *Facts {
	f := &Facts{Repo: &model.Repo{}}
	for _, c := range cmds {
		f.AddCommand(c)
	}
	f.CI = steps
	finalizeCI(scan.FromMap(nil), f)
	return f
}

func step(run string) model.CIStep { return stepIn(run, "") }

func stepIn(run, dir string) model.CIStep {
	return model.CIStep{System: "github-actions", Job: "j", Run: run, Dir: dir, Source: model.Source{Path: ".github/workflows/ci.yml", Line: 7}}
}

func decl(kind, run string) model.Command {
	return model.Command{Kind: kind, Run: run, Dir: ".", Evidence: []string{model.EvidenceDeclared}, Source: model.Source{Path: "package.json"}}
}

func conv(kind, run string) model.Command {
	return model.Command{Kind: kind, Run: run, Dir: ".", Evidence: []string{model.EvidenceConvention}, Source: model.Source{Path: "Cargo.toml"}}
}

func hasCI(c model.Command) bool {
	for _, e := range c.Evidence {
		if e == model.EvidenceCI {
			return true
		}
	}
	return false
}

func TestFinalizeCorroboration(t *testing.T) {
	tests := []struct {
		name   string
		cmd    model.Command
		ci     string
		wantCI bool
	}{
		{"exact", decl("test", "npm test"), "npm test", true},
		{"npm run test equals npm test", decl("test", "npm test"), "npm run test", true},
		{"pnpm run x equals pnpm x", decl("build", "pnpm build"), "pnpm run build", true},
		{"yarn run", decl("lint", "yarn lint"), "yarn run lint", true},
		{"env prefix", decl("test", "pnpm test"), "CI=true pnpm test", true},
		{"env command prefix", decl("test", "pnpm test"), "env FOO=bar pnpm test", true},
		{"trailing args", decl("test", "pnpm test"), "pnpm test -- --coverage", true},
		{"cd chain", decl("build", "npm run build"), "cd . && npm run build", true},
		{"after &&", decl("test", "make test"), "make build && make test", true},
		{"similar target not matched", decl("test", "make test"), "make test-integration", false},
		{"different script", decl("build", "npm run build"), "npm run build:docs", false},
		{"uv run pytest vs pytest", conv("test", "uv run pytest"), "pytest", true},
		{"python -m", conv("test", "python -m pytest"), "pytest", true},
		{"placeholder never matches", conv("test-single", "pytest <file>::<name>"), "pytest tests", false},
		{"unrelated", decl("test", "npm test"), "echo npm test", false},
		{"quoted &&", decl("test", "npm test"), `echo "a && npm test"`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := finalizeWith([]model.Command{tt.cmd}, step(tt.ci))
			if got := hasCI(f.Commands[0]); got != tt.wantCI {
				t.Errorf("ci evidence = %v, want %v; cmds=%+v", got, tt.wantCI, f.Commands)
			}
		})
	}
}

func TestFinalizeDirs(t *testing.T) {
	cmd := decl("test", "npm test")
	cmd.Dir = "web"
	if f := finalizeWith([]model.Command{cmd}, stepIn("npm test", "web")); !hasCI(f.Commands[0]) {
		t.Error("working-directory should match")
	}
	if f := finalizeWith([]model.Command{cmd}, step("cd web && npm test")); !hasCI(f.Commands[0]) {
		t.Error("cd should match")
	}
	if f := finalizeWith([]model.Command{cmd}, step("npm test")); hasCI(f.Commands[0]) {
		t.Error("root CI must not corroborate web command")
	}
	if f := finalizeWith([]model.Command{cmd}, step("cd .. && npm test")); hasCI(f.Commands[0]) {
		t.Error("escaping cd must not match")
	}
}

func TestFinalizeConventionWithFlagsAddsExactCI(t *testing.T) {
	f := finalizeWith([]model.Command{conv("test", "cargo test"), conv("install", "pnpm install")},
		step("cargo test --workspace --all-features"), step("pnpm install --frozen-lockfile"))
	got := cmdSet(f)
	if c, ok := got[".|test|cargo test --workspace --all-features"]; !ok || c.Evidence[0] != "ci" || c.Source.Line != 7 {
		t.Errorf("missing exact CI test command: %v", joinKeys(got))
	}
	if _, ok := got[".|install|pnpm install --frozen-lockfile"]; !ok {
		t.Errorf("missing exact CI install: %v", joinKeys(got))
	}
	if hasCI(got[".|test|cargo test"]) {
		t.Error("convention command must stay convention-only")
	}
}

func TestFinalizeCIOnly(t *testing.T) {
	tests := []struct{ ci, kind string }{
		{"npm ci", "install"}, {"yarn install --immutable", "install"}, {"bun install", "install"}, {"uv sync --all-extras", "install"},
		{"poetry install --no-interaction", "install"}, {"pip install -e .[dev]", "install"}, {"pip install -r requirements.txt", "install"},
		{"bundle install", "install"}, {"go mod download", "install"},
		{"go test ./...", "test"}, {"cargo test --workspace", "test"}, {"cargo nextest run", "test"}, {"uv run pytest -x", "test"},
		{"make test", "test"}, {"just test", "test"}, {"./gradlew check", "test"}, {"mvn -B verify", "test"}, {"dotnet test", "test"},
		{"bundle exec rspec", "test"}, {"mix test", "test"},
		{"golangci-lint run", "lint"}, {"cargo clippy --all-targets", "lint"}, {"ruff check .", "lint"}, {"npx eslint .", "lint"},
		{"cargo fmt --check", "format"}, {"gofmt -l .", "format"}, {"ruff format --check .", "format"}, {"npx prettier --check .", "format"},
		{"mypy src", "typecheck"}, {"pyright", "typecheck"}, {"npx tsc --noEmit", "typecheck"},
	}
	for _, tt := range tests {
		t.Run(tt.ci, func(t *testing.T) {
			f := finalizeWith(nil, step(tt.ci))
			if len(f.Commands) != 1 || f.Commands[0].Kind != tt.kind || f.Commands[0].Run != tt.ci || f.Commands[0].Evidence[0] != "ci" {
				t.Errorf("got %+v", f.Commands)
			}
		})
	}
}

func TestFinalizeConservative(t *testing.T) {
	for _, ci := range []string{
		"pip install tox", "pip install --upgrade pip", "tsc", "npx tsc -p build", "make test-all", "echo cargo test",
		"go test $PKGS", "cargo test --target ${{ matrix.target }}", "pnpm install ${{ inputs.flags }}", "curl x | bash",
		"cd /tmp && go test ./...", "cd ../other && go test", "cat <<EOF", "gh release create",
	} {
		t.Run(ci, func(t *testing.T) {
			if f := finalizeWith(nil, step(ci)); len(f.Commands) != 0 {
				t.Errorf("unexpected %+v", f.Commands)
			}
		})
	}
}

func TestFinalizeCDChainAndEnv(t *testing.T) {
	f := finalizeWith(nil, step("cd web && CI=1 npm ci && npm test"))
	got := cmdSet(f)
	if _, ok := got["web|install|npm ci"]; !ok {
		t.Errorf("have %s", joinKeys(got))
	}
	if _, ok := got["web|test|npm test"]; !ok {
		t.Errorf("have %s", joinKeys(got))
	}
}

func TestFinalizeNoDuplicates(t *testing.T) {
	f := finalizeWith(nil, step("go test ./..."), step("go test ./..."), stepIn("go test ./...", "."))
	if len(f.Commands) != 1 {
		t.Errorf("commands = %+v", f.Commands)
	}
	f = finalizeWith([]model.Command{decl("test", "npm test")}, step("npm test"), step("npm run test"))
	if len(f.Commands) != 1 || !hasCI(f.Commands[0]) {
		t.Errorf("commands = %+v", f.Commands)
	}
}

func TestFinalizeDeterministic(t *testing.T) {
	a := finalizeWith(nil, step("go test ./..."), step("cargo test"), step("npm ci"))
	b := finalizeWith(nil, step("go test ./..."), step("cargo test"), step("npm ci"))
	for i := range a.Commands {
		if a.Commands[i].Run != b.Commands[i].Run {
			t.Error("nondeterministic")
		}
	}
}

func TestFinalizeCapsCIOnlyPerKind(t *testing.T) {
	var steps []model.CIStep
	for _, flag := range []string{"a", "b", "c", "d", "e"} {
		steps = append(steps, step("uv sync --group "+flag))
	}
	docs := step("uv sync --group docs")
	docs.Source.Path = ".github/workflows/build-docs.yml"
	steps = append([]model.CIStep{docs}, steps...)
	f := finalizeWith(nil, steps...)
	if len(f.Commands) != maxCIOnlyPerKind {
		t.Fatalf("got %d commands", len(f.Commands))
	}
	for _, c := range f.Commands {
		if c.Run == "uv sync --group docs" {
			t.Error("docs workflow should rank last")
		}
	}
}

func TestFinalizeUvFlags(t *testing.T) {
	c := conv("test", "uv run pytest")
	if f := finalizeWith([]model.Command{c}, step("uv run --locked pytest")); !hasCI(f.Commands[0]) {
		t.Error("--locked is a plain switch")
	}
	if f := finalizeWith([]model.Command{c}, step("uv run --directory=sub pytest")); hasCI(f.Commands[0]) {
		t.Error("--directory changes where the command runs")
	}
}
