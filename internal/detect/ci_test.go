package detect

import (
	"fmt"
	"reflect"
	"testing"
)

func TestCISplitCommands(t *testing.T) {
	tests := []struct {
		name  string
		block string
		want  []string
	}{
		{"simple", "npm ci\nnpm test\n", []string{"npm ci", "npm test"}},
		{"continuation", "docker run \\\n  --rm \\\n  img\nnpm test", []string{"docker run --rm img", "npm test"}},
		{"comments blanks set", "# install\nset -euo pipefail\n\nset +x\nmake test  # trailing", []string{"make test # trailing"}},
		{"cd and keep", "cd web && npm ci", []string{"cd web && npm ci"}},
		{"trailing &&", "npm ci &&\n  npm test", []string{"npm ci && npm test"}},
		{"multiline quotes", "python -c \"\nprint(1)\n\"\nnpm test", []string{`python -c " print(1) "`, "npm test"}},
		{"heredoc dropped", "cat <<EOF > f\nhello\nnpm publish\nEOF\nnpm test", []string{"cat <<EOF > f", "npm test"}},
		{"control keywords", "if [ -f x ]; then\n  npm test\nfi", []string{"npm test"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []string
			for _, c := range ciSplitCommands(tt.block, 10) {
				got = append(got, c.text)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %q want %q", got, tt.want)
			}
		})
	}
	cmds := ciSplitCommands("a\n\nb", 5)
	if cmds[0].line != 5 || cmds[1].line != 7 {
		t.Errorf("lines = %+v", cmds)
	}
}

const ghWorkflow = `name: CI
defaults:
  run:
    working-directory: app
jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: 20
      - uses: pnpm/action-setup@v4
        with:
          version: 9.1.0
      - uses: actions/setup-python@v5
        with:
          python-version: ${{ matrix.python }}
      - uses: dtolnay/rust-toolchain@stable
      - uses: actions/setup-go@v5
        with:
          go-version: "1.22"
      - run: pnpm install --frozen-lockfile
      - name: multi
        run: |
          pnpm build
          pnpm test -- --coverage
      - run: npm test
        working-directory: ./web/
      - run: curl -H token=abcdef1234567890 x
  release:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: pkg
    steps:
      - run: npm publish
      - run: npm run build
      - run: gh release create v1
      - run: echo hi
        shell: pwsh
`

func TestCIGitHubActions(t *testing.T) {
	f := runOnly(map[string]string{".github/workflows/ci.yml": ghWorkflow}, detectCI)
	type row struct {
		job, run, dir string
		line          int
	}
	var got []row
	for _, s := range f.CI {
		if s.System != "github-actions" {
			t.Errorf("system = %q", s.System)
		}
		got = append(got, row{s.Job, s.Run, s.Dir, s.Source.Line})
	}
	want := []row{
		{"test", "pnpm install --frozen-lockfile", "app", 23},
		{"test", "pnpm build", "app", 26},
		{"test", "pnpm test -- --coverage", "app", 27},
		{"test", "npm test", "web", 28},
		{"test", "curl -H token=[REDACTED] x", "app", 30},
		{"release", "npm run build", "pkg", 38},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got\n%+v\nwant\n%+v", got, want)
	}
	tc := map[string]string{}
	for _, x := range f.Toolchains {
		tc[x.Name] = x.Version
		if x.Source.Path != ".github/workflows/ci.yml" {
			t.Errorf("source %+v", x.Source)
		}
	}
	wantTC := map[string]string{"node": "20", "pnpm": "9.1.0", "rust": "stable", "go": "1.22"}
	if !reflect.DeepEqual(tc, wantTC) {
		t.Errorf("toolchains = %v, want %v", tc, wantTC)
	}
}

func TestCIGitLab(t *testing.T) {
	files := map[string]string{".gitlab-ci.yml": `stages: [test, deploy]
variables:
  A: b
.template:
  script: [echo template]
lint:
  stage: test
  before_script:
    - pip install -r requirements.txt
  script:
    - ruff check .
    - |
      pytest -q
      mypy .
ship:
  stage: deploy
  script:
    - ./deploy.sh
`}
	f := runOnly(files, detectCI)
	var runs []string
	for _, s := range f.CI {
		runs = append(runs, s.Job+": "+s.Run)
	}
	want := []string{"lint: pip install -r requirements.txt", "lint: ruff check .", "lint: pytest -q", "lint: mypy ."}
	if !reflect.DeepEqual(runs, want) {
		t.Errorf("got %q", runs)
	}
	if f.CI[0].System != "gitlab-ci" {
		t.Error("system")
	}
}

func TestCIEdgeCases(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"none":         {"main.go": ""},
		"invalid yaml": {".github/workflows/x.yml": "jobs: [[["},
		"no jobs":      {".github/workflows/x.yml": "name: x\n"},
		"not mapping":  {".github/workflows/x.yml": "- a\n- b\n"},
	} {
		t.Run(name, func(t *testing.T) {
			f := runOnly(files, detectCI)
			if len(f.CI) != 0 {
				t.Errorf("CI = %+v", f.CI)
			}
		})
	}
}

func TestCICap(t *testing.T) {
	steps := ""
	for i := 0; i < 400; i++ {
		steps += fmt.Sprintf("      - run: make t%d\n", i)
	}
	f := runOnly(map[string]string{".github/workflows/x.yml": "jobs:\n  a:\n    steps:\n" + steps}, detectCI)
	if len(f.CI) != maxCISteps {
		t.Errorf("len = %d", len(f.CI))
	}
}
