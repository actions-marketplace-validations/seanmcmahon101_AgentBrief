package detect

import (
	"testing"
)

func TestPythonToolDetection(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		pm    string
		want  []string
	}{
		{"uv lock", map[string]string{"pyproject.toml": "[project]\nname='a'\n", "uv.lock": ""}, "uv", []string{".|install|uv sync"}},
		{"tool.uv", map[string]string{"pyproject.toml": "[tool.uv]\n"}, "uv", nil},
		{"poetry", map[string]string{"pyproject.toml": "[tool.poetry]\nname='a'\n[tool.poetry.dependencies]\npython='^3.10'\n"}, "poetry", []string{".|install|poetry install"}},
		{"poetry lock beats pdm", map[string]string{"pyproject.toml": "[project]\n", "poetry.lock": "", "pdm.lock": ""}, "poetry", nil},
		{"pdm", map[string]string{"pyproject.toml": "[tool.pdm]\n"}, "pdm", []string{".|install|pdm install"}},
		{"hatch envs", map[string]string{"pyproject.toml": "[tool.hatch.envs.default]\n"}, "hatch", nil},
		{"hatchling backend only is pip", map[string]string{"pyproject.toml": "[build-system]\nrequires=['hatchling']\n[tool.hatch.version]\npath='x'\n"}, "pip", []string{".|install|pip install -e ."}},
		{"pipenv", map[string]string{"Pipfile": "[packages]\n"}, "pipenv", []string{".|install|pipenv install --dev"}},
		{"requirements", map[string]string{"requirements.txt": "flask\n", "app.py": ""}, "pip", []string{".|install|pip install -r requirements.txt"}},
		{"setup.py", map[string]string{"setup.py": "from setuptools import setup"}, "pip", []string{".|install|pip install -e ."}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := runOnly(tt.files, detectPython)
			if len(f.PackageManagers) != 1 || f.PackageManagers[0].Name != tt.pm || f.PackageManagers[0].Ecosystem != "python" {
				t.Fatalf("PMs = %+v", f.PackageManagers)
			}
			wantCmds(t, f, tt.want...)
		})
	}
}

func TestPythonNegative(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"go repo":                 {"go.mod": "module x", "main.go": ""},
		"requirements without py": {"requirements.txt": "sphinx\n", "main.go": ""},
		"node":                    {"package.json": "{}"},
		"invalid pyproject":       {"pyproject.toml": "[[[bad"},
	} {
		t.Run(name, func(t *testing.T) {
			f := runOnly(files, detectPython)
			if len(f.Projects)+len(f.Commands)+len(f.PackageManagers) != 0 {
				t.Errorf("false detection: %+v", f.Repo)
			}
		})
	}
}

func TestPythonToolchains(t *testing.T) {
	f := runOnly(map[string]string{"pyproject.toml": "[project]\nname='a'\nrequires-python='>=3.9'\n", ".python-version": "3.12\n"}, detectPython)
	got := map[string]bool{}
	for _, tc := range f.Toolchains {
		got[tc.Source.Path+":"+tc.Version] = true
	}
	if !got[".python-version:3.12"] || !got["pyproject.toml:>=3.9"] {
		t.Errorf("toolchains = %+v", f.Toolchains)
	}
}

func TestPythonCommands(t *testing.T) {
	base := "[project]\nname='a'\n[tool.pytest.ini_options]\nx=1\n[tool.ruff]\nline-length=88\n[tool.mypy]\nstrict=true\n"
	tests := []struct {
		name  string
		files map[string]string
		want  []string
	}{
		{"uv", map[string]string{"pyproject.toml": base, "uv.lock": ""}, []string{
			".|test|uv run pytest", ".|test-single|uv run pytest <file>::<test_name>",
			".|lint|uv run ruff check .", ".|format|uv run ruff format .", ".|typecheck|uv run mypy ."}},
		{"poetry", map[string]string{"pyproject.toml": base + "[tool.poetry]\n"}, []string{".|test|poetry run pytest", ".|lint|poetry run ruff check ."}},
		{"pdm", map[string]string{"pyproject.toml": base + "[tool.pdm]\n"}, []string{".|test|pdm run pytest"}},
		{"hatch", map[string]string{"pyproject.toml": base + "[tool.hatch.envs.default]\n"}, []string{".|test|hatch test", ".|test-single|hatch test <file>::<test_name>"}},
		{"pip", map[string]string{"pyproject.toml": base}, []string{".|test|python -m pytest", ".|lint|ruff check ."}},
		{"ruff.toml", map[string]string{"pyproject.toml": "[project]\nname='a'\n", "ruff.toml": ""}, []string{".|lint|ruff check ."}},
		{"pyright", map[string]string{"pyproject.toml": "[tool.pyright]\n"}, []string{".|typecheck|pyright"}},
		{"pytest by dependency", map[string]string{"pyproject.toml": "[project]\nname='a'\n[dependency-groups]\ndev=['pytest>=8']\n"}, []string{".|test|python -m pytest"}},
		{"pytest via requirements", map[string]string{"requirements-dev.txt": "pytest==8\n", "a.py": ""}, []string{".|test|python -m pytest"}},
		{"django", map[string]string{"pyproject.toml": "[project]\nname='a'\n", "manage.py": ""}, []string{".|test-single|python manage.py test <app.tests.Class.test>"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantCmds(t, runOnly(tt.files, detectPython), tt.want...)
		})
	}
	// Not configured: no invented pytest/ruff/mypy.
	f := runOnly(map[string]string{"pyproject.toml": "[project]\nname='a'\n", "tests/test_a.py": ""}, detectPython)
	for _, c := range f.Commands {
		if c.Kind != "install" {
			t.Errorf("invented command %+v", c)
		}
	}
}

func TestPythonTaskRunners(t *testing.T) {
	files := map[string]string{
		"pyproject.toml": `[project]
name = "a"
[tool.uv]
[tool.poe.tasks]
test = "pytest"
docs = { cmd = "mkdocs build" }
[tool.pdm.scripts]
_ = { env = {A = "1"} }
lint = "ruff check ."
[tool.hatch.envs.default.scripts]
test = "pytest"
[tool.hatch.envs.lint.scripts]
style = "ruff ."
`,
		"tox.ini": "[tox]\nenvlist = py{39,310}, lint, typecheck\n[testenv]\ncommands = pytest\n[testenv:lint]\ncommands = ruff check\n",
		"noxfile.py": `import nox

@nox.session
def tests(session):
    pass

@nox.session(
    python=["3.11"],
    name="type-check",
)
def mypy_session(session):
    pass

@nox.parametrize("x", [1])
@nox.session(name="lint")
def lint_session(session):
    pass

def helper():
    pass
`,
	}
	f := runOnly(files, detectPython)
	got := map[string]bool{}
	for _, tk := range f.Tasks {
		got[tk.Runner+":"+tk.Name] = true
	}
	for _, w := range []string{"poe:test", "poe:docs", "pdm:lint", "hatch:test", "hatch:lint:style", "tox:lint", "tox:typecheck",
		"nox:tests", "nox:type-check", "nox:lint"} {
		if !got[w] {
			t.Errorf("missing task %s; have %v", w, got)
		}
	}
	for _, bad := range []string{"pdm:_", "nox:helper", "tox:py{39", "nox:mypy_session"} {
		if got[bad] {
			t.Errorf("unexpected task %s", bad)
		}
	}
	wantCmds(t, f, ".|test|uv run poe test", ".|lint|pdm run lint", ".|test|hatch run test", ".|lint|tox -e lint",
		".|typecheck|tox -e typecheck", ".|test|nox -s tests", ".|lint|nox -s lint")
}

func TestPythonUvWorkspace(t *testing.T) {
	files := map[string]string{
		"pyproject.toml":              "[project]\nname='root'\n[tool.uv.workspace]\nmembers=['packages/*']\nexclude=['packages/old']\n",
		"uv.lock":                     "",
		"packages/a/pyproject.toml":   "[project]\nname='a'\n[tool.pytest.ini_options]\n",
		"packages/old/pyproject.toml": "[project]\nname='old'\n",
		"packages/b/pyproject.toml":   "[project]\nname='b'\n",
	}
	f := runOnly(files, detectPython)
	if len(f.Projects) != 3 {
		t.Fatalf("projects = %+v", f.Projects)
	}
	if !f.Projects[0].IsWorkspaceRoot || f.Projects[1].Workspace != "." {
		t.Errorf("projects = %+v", f.Projects)
	}
	wantCmds(t, f, "packages/a|test|uv run pytest")
	if f.Name != "root" {
		t.Errorf("name = %q", f.Name)
	}
}

func TestPythonStandalone(t *testing.T) {
	f := runOnly(map[string]string{"go.mod": "x", "scripts/pyproject.toml": "[project]\nname='s'\n", "examples/x/pyproject.toml": "[project]\n"}, detectPython)
	if len(f.Projects) != 1 || f.Projects[0].Dir != "scripts" {
		t.Errorf("projects = %+v", f.Projects)
	}
}

func TestPythonSourceLines(t *testing.T) {
	f := runOnly(map[string]string{"pyproject.toml": "[project]\nname='a'\n\n[tool.ruff]\nx=1\n"}, detectPython)
	for _, c := range f.Commands {
		if c.Kind == "lint" && c.Source.Line != 4 {
			t.Errorf("lint source = %+v", c.Source)
		}
	}
}

func TestPythonToxCommand(t *testing.T) {
	f := runOnly(map[string]string{"pyproject.toml": "[project]\nname='a'\n[tool.tox]\nenv_list=['lint']\n", "uv.lock": ""}, detectPython)
	wantCmds(t, f, ".|test|uv run tox")
	f = runOnly(map[string]string{"tox.ini": "[tox]\nenvlist=py\n", "setup.py": "x"}, detectPython)
	wantCmds(t, f, ".|test|tox")
	f = runOnly(map[string]string{"pyproject.toml": "[project]\nname='a'\n"}, detectPython)
	noCmds(t, f, ".|test|tox")
}
