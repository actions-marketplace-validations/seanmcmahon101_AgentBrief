package instructions

import (
	"strings"
	"testing"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
)

// findings returns "rule/severity: message" lines for compact assertions.
func lines(fs []model.Finding) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(f.Rule + "/" + f.Severity + ": " + f.Message + " " + f.Suggestion + "\n")
	}
	return b.String()
}

func hasSev(fs []model.Finding, rule, sev, sub string) bool {
	for _, f := range fs {
		if f.Rule == rule && f.Severity == sev && strings.Contains(f.Message+" "+f.Suggestion, sub) {
			return true
		}
	}
	return false
}

func TestPathArgsTruePositives(t *testing.T) {
	doc := "# Dev\n\n" +
		"Run `pytest tests/gone_test.py::test_x` and `python scripts/gone.py`.\n" +
		"```bash\n" +
		"bash ./scripts/gone.sh\n" +
		"go test ./nonexistent/...\n" +
		"go build ./cmd/gone\n" +
		"node tools/gone.js\n" +
		"uv run pytest tests/gone2/\n" +
		"uv run scripts/gone3.py\n" +
		"npx tsx scripts/gone4.ts\n" +
		"./scripts/gone5.sh --flag\n" +
		"```\n"
	f := checkDoc(t, model.Repo{}, map[string]string{
		"AGENTS.md":        doc,
		"tests/test_a.py":  "",
		"scripts/real.sh":  "",
		"cmd/real/main.go": "package main",
	})
	for _, p := range []string{"tests/gone_test.py::test_x", "scripts/gone.py", "./scripts/gone.sh", "./nonexistent/...", "./cmd/gone",
		"tools/gone.js", "tests/gone2/", "scripts/gone3.py", "scripts/gone4.ts", "./scripts/gone5.sh"} {
		found := false
		for _, x := range f {
			found = found || (x.Rule == RuleMissingPath && strings.Contains(x.Message, "`"+p+"`"))
		}
		if !found {
			t.Errorf("not caught: %s\n%s", p, lines(f))
		}
	}
	// errors where the parent dir or an explicit ./ makes it unambiguous
	for _, p := range []string{"scripts/gone.py", "./nonexistent/...", "./cmd/gone", "tests/gone_test.py"} {
		if !hasSev(f, RuleMissingPath, model.SeverityError, "`"+p) {
			t.Errorf("want error for %s\n%s", p, lines(f))
		}
	}
	// no parent dir, no ./ => only a warning
	f2 := checkDoc(t, model.Repo{}, map[string]string{"AGENTS.md": "Run `python elsewhere/gone.py`.\n", "x.txt": ""})
	if !hasSev(f2, RuleMissingPath, model.SeverityWarning, "elsewhere/gone.py") {
		t.Errorf("want warning: %s", lines(f2))
	}
}

func TestPathArgsFalsePositivesAvoided(t *testing.T) {
	doc := "Commands:\n" +
		"- `pytest`, `pytest -k foo tests/`, `pytest tests/test_a.py::TestX::test_y`, `pytest tests/test_*.py`, `pytest --cov src tests`\n" +
		"- `python -m pytest tests/test_a.py`, `python -m http.server`, `python -c 'print(1)'`\n" +
		"- `pytest <file>`, `python path/to/script.py`, `bash ./scripts/$NAME.sh`, `node examples/demo.js`, `python my_script.py`\n" +
		"- `go test ./...`, `go test ./pkg/... -run X`, `go build ./cmd/real`, `go run ./cmd/real arg ./nonexistent`, `go test -tags=integration ./...`\n" +
		"- `./gradlew build`, `./target/release/app`, `./main`, `./bin/tool`, `bash -c 'x ./nope.sh'`, `sh -s`\n" +
		"- `uv run python -m mymod`, `node -e 'x'`, `bash scripts/real.sh`\n" +
		"```bash\n" +
		"cd sub && pytest tests/in_sub.py\n" +
		"cd other\n" +
		"pytest tests/in_other.py\n" +
		"```\n" +
		"Don't run `bash ./scripts/gone.sh`; for example `go test ./nonexistent/...`.\n" +
		"Use this, e.g. `pytest tests/gone.py`.\n" +
		"Never do the following:\n```\npytest tests/gone.py\n```\n"
	f := checkDoc(t, model.Repo{}, map[string]string{
		"AGENTS.md":               doc,
		"tests/test_a.py":         "",
		"scripts/real.sh":         "",
		"cmd/real/main.go":        "package main",
		"pkg/a.go":                "package pkg",
		"sub/tests/in_sub.py":     "",
		"other/tests/in_other.py": "",
		"gradlew":                 "",
	})
	if n := count(f, RuleMissingPath); n != 0 {
		t.Errorf("false positives:\n%s", lines(f))
	}
}

func TestCdMissingDirIsNotJudged(t *testing.T) {
	f := checkDoc(t, model.Repo{}, map[string]string{"AGENTS.md": "```bash\ncd nowhere && pytest tests/x.py\ncd $DIR\npytest tests/y.py\n```\n", "tests/t.py": ""})
	if count(f, RuleMissingPath) != 0 {
		t.Errorf("%s", lines(f))
	}
}

func TestMakeDirFix(t *testing.T) {
	repo := model.Repo{Tasks: []model.Task{{Runner: "make", Name: "build", Dir: "."}, {Runner: "make", Name: "gen", Dir: "sub"}}}
	// missing dir: report the directory, not the root Makefile target
	f := checkDoc(t, repo, map[string]string{"Makefile": "build:\n\techo\n", "AGENTS.md": "`make -C gone build` `make -C gone ghost`\n"})
	if !hasSev(f, RuleMissingPath, model.SeverityError, "directory `gone` does not exist") || count(f, RuleUnknownScript) != 0 {
		t.Errorf("%s", lines(f))
	}
	// existing dir is judged against its own Makefile
	f = checkDoc(t, repo, map[string]string{"Makefile": "build:\n\techo\n", "sub/Makefile": "gen:\n\techo\n",
		"AGENTS.md": "`make -C sub gen` `make -C sub build` `cd sub && make nope` `cd sub && make gen` `make -f other.mk zzz`\n"})
	if !has(f, RuleUnknownScript, "target `build`, which is not defined in the Makefile of `sub`") || !has(f, RuleUnknownScript, "target `nope`") || count(f, RuleUnknownScript) != 2 {
		t.Errorf("%s", lines(f))
	}
	if count(f, RuleMissingPath) != 0 {
		t.Errorf("%s", lines(f))
	}
}

func TestNegationAndExampleSkipping(t *testing.T) {
	repo := pnpmMonorepo()
	doc := "Don't run `pnpm run lintt`.\n" +
		"Use this for example `npm run foo`.\n" +
		"Such as `pnpm run nope1` or e.g. `pnpm run nope2`.\n" +
		"Rather than `pnpm run nope3`, use the real one.\n" +
		"Never edit generated files. Run `pnpm run nope4` afterwards.\n" +
		"Run `pnpm run nope5`, never `pnpm run nope6`.\n" +
		"For example:\n```bash\npnpm run nope7\n```\n" +
		"Run it:\n```bash\npnpm run nope8\n```\n"
	f := checkDoc(t, repo, map[string]string{"AGENTS.md": doc, "package.json": "{}"})
	var got []string
	for _, x := range f {
		if x.Rule == RuleUnknownScript {
			got = append(got, x.Message)
		}
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"nope4", "nope5", "nope8"} {
		if !strings.Contains(joined, want) {
			t.Errorf("genuine instruction %s missed: %s", want, joined)
		}
	}
	for _, bad := range []string{"lintt", "foo", "nope1", "nope2", "nope3", "nope6", "nope7"} {
		if strings.Contains(joined, bad) {
			t.Errorf("negated/example %s flagged: %s", bad, joined)
		}
	}
}

func rustRepo() model.Repo {
	return model.Repo{Projects: []model.Project{
		{Dir: ".", Name: "ws", Ecosystem: "rust", IsWorkspaceRoot: true},
		{Dir: "crates/core", Name: "app-core", Ecosystem: "rust", Workspace: "."},
		{Dir: "crates/cli", Name: "app-cli", Ecosystem: "rust", Workspace: "."},
	}}
}

func TestCargoPackages(t *testing.T) {
	doc := "`cargo test -p ghost` `cargo build --package=app-cor` `cargo clippy -p app-core -p nope2 -- -D warnings` `cargo run -p app-cli`\n" +
		"`cargo test -p app-core --test integ` `cargo test --test ghosttest` `cargo test -p serde` `cargo test -p <crate>` `cargo build -p $X`\n"
	f := checkDoc(t, rustRepo(), map[string]string{"AGENTS.md": doc, "crates/core/tests/integ.rs": "", "Cargo.lock": "[[package]]\nname = \"serde\"\n"})
	for _, w := range []string{"crate `ghost`", "crate `app-cor`", "crate `nope2`", "integration test `ghosttest`"} {
		if !hasSev(f, RuleUnknownScript, model.SeverityError, w) {
			t.Errorf("missing %s\n%s", w, lines(f))
		}
	}
	if !has(f, RuleUnknownScript, "did you mean `-p app-core`?") {
		t.Errorf("%s", lines(f))
	}
	if n := count(f, RuleUnknownScript); n != 4 {
		t.Errorf("want 4 got %d\n%s", n, lines(f))
	}
	// no rust project: silent
	f = checkDoc(t, model.Repo{}, map[string]string{"AGENTS.md": doc})
	if count(f, RuleUnknownScript) != 0 {
		t.Errorf("%s", lines(f))
	}
}

func TestPoeToxNox(t *testing.T) {
	repo := model.Repo{Tasks: []model.Task{
		{Runner: "poe", Name: "lint", Dir: "."}, {Runner: "poe", Name: "test", Dir: "."},
		{Runner: "tox", Name: "lint", Dir: "."}, {Runner: "tox", Name: "docs", Dir: "."},
		{Runner: "nox", Name: "tests", Dir: "."},
	}}
	doc := "`poe nothing` `poe lint` `poe --help` `poe <task>` `uv run poe tset`\n" +
		"`tox -e py99` `tox -e lint,docs` `tox -e py311` `tox -e py311-django` `tox -e lint,ghost` `tox -e .pkg` `tox`\n" +
		"`nox -s ghost` `nox -s tests` `nox -s tests-3.11` `nox -s tests(python='3.11')` `nox -l`\n"
	f := checkDoc(t, repo, map[string]string{"AGENTS.md": doc, "pyproject.toml": "[tool.poe.tasks]\nlint='x'\n", "tox.ini": "[tox]\nenvlist = lint, docs\n"})
	for _, w := range []struct{ sev, sub string }{
		{"error", "poe task `nothing`"}, {"error", "poe task `tset`"}, {"error", "tox environment `py99`"},
		{"error", "tox environment `ghost`"}, {"warning", "nox session `ghost`"},
	} {
		if !hasSev(f, RuleUnknownScript, w.sev, w.sub) {
			t.Errorf("missing %v\n%s", w, lines(f))
		}
	}
	if n := count(f, RuleUnknownScript); n != 5 {
		t.Errorf("want 5 got %d\n%s", n, lines(f))
	}
	// tox generator syntax => never judged; poe include => warning only
	f = checkDoc(t, repo, map[string]string{"AGENTS.md": doc, "pyproject.toml": "[tool.poe]\ninclude = \"x.toml\"\n", "tox.ini": "[tox]\nenvlist =\n    py{39,310}\n    lint\n"})
	if hasSev(f, RuleUnknownScript, "error", "tox") || !hasSev(f, RuleUnknownScript, "warning", "poe task `nothing`") || hasSev(f, RuleUnknownScript, "error", "poe") {
		t.Errorf("%s", lines(f))
	}
	// empty inventory => silent
	f = checkDoc(t, model.Repo{}, map[string]string{"AGENTS.md": doc})
	if count(f, RuleUnknownScript) != 0 {
		t.Errorf("%s", lines(f))
	}
}

func TestComposeServices(t *testing.T) {
	doc := "`docker compose up web` `docker compose up -d --build web db` `docker compose up nope` `docker-compose run --rm api pytest tests/x.py`\n" +
		"`docker compose exec ghost sh` `docker compose -f other.yml up zzz` `docker compose up` `docker compose logs -f web` `docker compose run --rm -e A=b web ls ghost2`\n" +
		"`docker compose run --weird-flag zzz`\n"
	files := map[string]string{"AGENTS.md": doc, "compose.yaml": "services:\n  web:\n    image: x\n  db:\n    image: y\n  api: {}\n"}
	f := checkDoc(t, model.Repo{}, files)
	for _, w := range []string{"service `nope`", "service `ghost`"} {
		if !hasSev(f, RuleUnknownScript, "error", w) {
			t.Errorf("missing %s\n%s", w, lines(f))
		}
	}
	if n := count(f, RuleUnknownScript); n != 2 {
		t.Errorf("want 2 got %d\n%s", n, lines(f))
	}
	// include / extends / no file => not judged
	for name, c := range map[string]string{"include": "include:\n  - other.yml\nservices:\n  web: {}\n", "extends": "services:\n  web:\n    extends:\n      file: x.yml\n      service: web\n", "bad": "services: [\n"} {
		files["compose.yaml"] = c
		if f := checkDoc(t, model.Repo{}, files); count(f, RuleUnknownScript) != 0 {
			t.Errorf("%s: %s", name, lines(f))
		}
	}
	delete(files, "compose.yaml")
	if f := checkDoc(t, model.Repo{}, files); count(f, RuleUnknownScript) != 0 {
		t.Errorf("no file: %s", lines(f))
	}
}

func TestProsePaths(t *testing.T) {
	doc := "See internal/ghost/ for details, and docs/real/ too.\n" +
		"Use client/server architecture, and/or this; read src/missing.go.\n" +
		"Link: https://example.com/internal/ghost/ and [x](internal/gone2/) and `internal/gone3/`.\n" +
		"Don't touch internal/gone4/. Also unknowntop/foo/ is ignored, and ./internal/gone5/ too.\n"
	f := checkDoc(t, model.Repo{}, map[string]string{"AGENTS.md": doc, "internal/real.go": "", "docs/real/a.md": "", "src/x.go": "", "client/a.go": ""})
	var prose []string
	for _, x := range f {
		if x.Rule == RuleMissingPath && strings.Contains(x.Message, "is mentioned") {
			if x.Severity != model.SeverityWarning {
				t.Errorf("severity %v", x)
			}
			prose = append(prose, x.Message)
		}
	}
	j := strings.Join(prose, "|")
	for _, w := range []string{"internal/ghost/", "src/missing.go", "internal/gone5/"} {
		if !strings.Contains(j, w) {
			t.Errorf("missing %s in %s", w, j)
		}
	}
	for _, bad := range []string{"docs/real", "client/server", "and/or", "gone2", "gone3", "gone4", "unknowntop", "example.com"} {
		if strings.Contains(j, bad) {
			t.Errorf("flagged %s in %s", bad, j)
		}
	}
}
