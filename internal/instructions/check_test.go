package instructions

import (
	"strings"
	"testing"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
)

func pnpmMonorepo() model.Repo {
	return model.Repo{
		PackageManagers: []model.PackageManager{{Name: "pnpm", Ecosystem: "node", Dir: ".", Source: model.Source{Path: "pnpm-lock.yaml"}}},
		Projects: []model.Project{
			{Dir: ".", Name: "root", Ecosystem: "node", IsWorkspaceRoot: true},
			{Dir: "packages/core", Name: "@acme/core", Ecosystem: "node", Workspace: "."},
			{Dir: "packages/ui", Name: "@acme/ui", Ecosystem: "node", Workspace: "."},
		},
		Tasks: []model.Task{
			npmTask("build", "."), npmTask("test", "."), npmTask("test:unit", "."), npmTask("lint", "."),
			npmTask("storybook", "packages/ui"), npmTask("gen", "packages/core"),
		},
	}
}

func checkDoc(t *testing.T, repo model.Repo, files map[string]string) []model.Finding {
	t.Helper()
	root, r := fixture(t, files, repo)
	return Check(root, r, CheckOptions{})
}

func TestUnknownScriptTruePositives(t *testing.T) {
	doc := "# Dev\n\nRun `pnpm test:unti` for unit tests.\n\n```bash\npnpm install\npnpm run deploy-all\npnpm --filter @acme/core run storybook\npnpm -F @acme/ui run gen\nnpm run nope\nnpm lint\n```\n\n$ pnpm run buidl\n"
	f := checkDoc(t, pnpmMonorepo(), map[string]string{"AGENTS.md": doc, "package.json": "{}"})
	for _, want := range []struct{ rule, sub string }{
		{RuleUnknownScript, "`test:unti`"},
		{RuleUnknownScript, "did you mean `pnpm test:unit`?"},
		{RuleUnknownScript, "`deploy-all`"},
		{RuleUnknownScript, "`storybook`, which is not defined in `packages/core/package.json`"},
		{RuleUnknownScript, "`gen`, which is not defined in `packages/ui/package.json`"},
		{RuleUnknownScript, "`nope`"},
		{RuleUnknownScript, "`npm lint` is not an npm command"},
		{RuleUnknownScript, "did you mean `pnpm run build`?"},
	} {
		if !has(f, want.rule, want.sub) {
			t.Errorf("missing %q in %+v", want.sub, f)
		}
	}
	for _, x := range f {
		if x.Rule == RuleUnknownScript && x.Severity != model.SeverityError {
			t.Errorf("severity %+v", x)
		}
		if x.Path != "AGENTS.md" || x.Line == 0 {
			t.Errorf("bad location %+v", x)
		}
	}
}

func TestUnknownScriptFalsePositivesAvoided(t *testing.T) {
	doc := "Commands:\n\n" +
		"- `pnpm install --frozen-lockfile`, `pnpm add -D x`, `pnpm dlx create-foo`, `pnpm exec tsc`\n" +
		"- `npm ci`, `npm install`, `npm publish`, `npm run <script>`, `pnpm run $SCRIPT`, `pnpm {name}`, `yarn ...`\n" +
		"- `pnpm vitest run src/foo.test.ts`, `pnpm tsc --noEmit`, `pnpm playwright test`\n" +
		"- `pnpm --filter @acme/core... build`, `pnpm -r build`, `pnpm --filter \"./packages/*\" test`\n" +
		"- `pnpm test`, `pnpm lint && pnpm build`, `pnpm run test:unit -- --watch`, `pnpm run --filter @acme/ui storybook`\n" +
		"- `yarn workspace @acme/core gen`\n" + // gen exists in core
		"- `make`, `make -j4`, `just`, `task`, `rake`\n" +
		"```\n" +
		"packages/\n  core/\n  ui/\n" +
		"```\n" +
		"```text\npnpm nonsense-in-text-fence\n```\n"
	f := checkDoc(t, pnpmMonorepo(), map[string]string{"AGENTS.md": doc, "package.json": "{}"})
	if n := count(f, RuleUnknownScript); n != 0 {
		t.Errorf("false positives: %+v", f)
	}
}

func TestUnknownScriptNpmFamily(t *testing.T) {
	repo := model.Repo{
		PackageManagers: []model.PackageManager{{Name: "npm", Ecosystem: "node", Dir: ".", Source: model.Source{Path: "package-lock.json"}}},
		Tasks:           []model.Task{npmTask("build", "."), npmTask("test", ".")},
	}
	f := checkDoc(t, repo, map[string]string{"CLAUDE.md": "Use `npm test` then `npm run build`, `npm start`, `npm run dev`.\n", "package.json": "{}"})
	if count(f, RuleUnknownScript) != 1 || !has(f, RuleUnknownScript, "`dev`") {
		t.Errorf("%+v", f)
	}
	repo.Tasks = nil
	f = checkDoc(t, repo, map[string]string{"CLAUDE.md": "Run `npm test`.\n", "package.json": `{"name":"x"}`})
	if !has(f, RuleUnknownScript, "`test`") {
		t.Errorf("npm test without script: %+v", f)
	}
	// no package.json and no tasks at all: cannot judge
	f = checkDoc(t, repo, map[string]string{"CLAUDE.md": "Run `npm run build`.\n"})
	if len(f) != 0 {
		t.Errorf("%+v", f)
	}
}

func TestMakeJustTask(t *testing.T) {
	repo := model.Repo{Tasks: []model.Task{
		{Runner: "make", Name: "build", Dir: "."}, {Runner: "make", Name: "test", Dir: "."},
		{Runner: "make", Name: "gen", Dir: "sub"},
		{Runner: "just", Name: "fmt", Dir: "."},
		{Runner: "task", Name: "lint", Dir: "."},
		{Runner: "mise", Name: "ci", Dir: "."},
	}}
	files := map[string]string{
		"Makefile": "build:\n\techo\ntest:\n\techo\n",
		"AGENTS.md": "Use `make buid`, `make test`, `make -C sub gen`, `make -C sub nope`, `make -j 4 test`, `make build/out.o`, `make $(T)`.\n" +
			"Also `just fmt`, `just frmt`, `just --list`, `task lint`, `task lnt`, `mise run ci`, `mise run cii`, `mise run lint:*`.\n",
		"sub/Makefile": "gen:\n\techo\n",
	}
	f := checkDoc(t, repo, files)
	var got []string
	for _, x := range f {
		if x.Rule == RuleUnknownScript {
			got = append(got, x.Message)
		}
	}
	want := []string{"target `buid`", "target `nope`", "recipe `frmt`", "task `lnt`", "task `cii`"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i, w := range want {
		found := false
		for _, g := range got {
			found = found || strings.Contains(g, w)
		}
		if !found {
			t.Errorf("missing %s (%d) in %v", w, i, got)
		}
	}
	if !has(f, RuleUnknownScript, "did you mean `make build`?") {
		t.Errorf("%+v", f)
	}
}

func TestMakeWithIncludeIsNotJudged(t *testing.T) {
	repo := model.Repo{Tasks: []model.Task{{Runner: "make", Name: "build", Dir: "."}}}
	f := checkDoc(t, repo, map[string]string{"Makefile": "include mk/*.mk\nbuild:\n", "AGENTS.md": "`make docs`\n"})
	if len(f) != 0 {
		t.Errorf("%+v", f)
	}
	// A target only visible as text in the Makefile (detector missed it) is accepted.
	f = checkDoc(t, repo, map[string]string{"Makefile": "build:\nbogus-alias: build\n", "AGENTS.md": "`make bogus-alias`\n"})
	if len(f) != 0 {
		t.Errorf("%+v", f)
	}
}

func TestComposerAndRake(t *testing.T) {
	repo := model.Repo{Tasks: []model.Task{{Runner: "composer", Name: "test", Dir: "."}, {Runner: "rake", Name: "build", Dir: "."}}}
	f := checkDoc(t, repo, map[string]string{
		"composer.json": `{"scripts":{"test":"phpunit"}}`,
		"Rakefile":      "task :build do\nend\n",
		"AGENTS.md":     "`composer install`, `composer test`, `composer run-script lint`, `composer require x/y`, `bundle exec rake build`, `rake spec`, `rake deploy`\n",
	})
	if !has(f, RuleUnknownScript, "script `lint`") || !has(f, RuleUnknownScript, "rake task `deploy`") || count(f, RuleUnknownScript) != 2 {
		t.Errorf("%+v", f)
	}
}

func TestMissingPaths(t *testing.T) {
	files := map[string]string{
		"AGENTS.md": "# A\n" +
			"See [guide](docs/guide.md#setup), [gone](docs/gone.md), [ext](https://x.dev/a.md), [abs](/etc/passwd), [anchor](#top), [mail](mailto:a@b.c), [img](./img/a.png?raw=1).\n" +
			"Edit `src/app.ts` and `src/missing.ts:12` and `README.md`; also `docs/` and `nodir/sub/file.json`.\n" +
			"Ignore `dist/bundle.js`, `src/**/*.ts`, `@acme/core`, `github.com/foo/bar`, `react-dom/client`, `<dir>/x.md`, `$HOME/x.md`, `--flag/x`, `Next.js`, `.env`, `components/Button.tsx`.\n" +
			"```\nsrc/not-checked-in-fence.ts\n```\n" +
			"<!-- agentbrief:start -->\n`src/inside-block.ts`\n<!-- agentbrief:end -->\n",
		"README.md":                       "r",
		"docs/guide.md":                   "g",
		"src/app.ts":                      "a",
		"src/ui/components/Button.tsx":    "b",
		"img/a.png":                       "i",
		".github/copilot-instructions.md": "[rel](../docs/guide.md) [bad](../docs/nope.md) `docs/guide.md`\n",
	}
	f := checkDoc(t, model.Repo{}, files)
	var errs, warns []string
	for _, x := range f {
		if x.Rule != RuleMissingPath {
			continue
		}
		if x.Severity == model.SeverityError {
			errs = append(errs, x.Path+":"+x.Message)
		} else {
			warns = append(warns, x.Path+":"+x.Message)
		}
	}
	wantErr := []string{"AGENTS.md:link target `docs/gone.md` does not exist", ".github/copilot-instructions.md:link target `../docs/nope.md` does not exist"}
	if strings.Join(errs, "\n") != strings.Join([]string{wantErr[1], wantErr[0]}, "\n") {
		t.Errorf("errors: %q", errs)
	}
	wantWarn := []string{"AGENTS.md:`src/missing.ts:12` does not exist in the repository", "AGENTS.md:`nodir/sub/file.json` does not exist in the repository"}
	if len(warns) != 2 {
		t.Fatalf("warnings: %q", warns)
	}
	for _, w := range wantWarn {
		ok := false
		for _, g := range warns {
			ok = ok || g == w
		}
		if !ok {
			t.Errorf("missing warning %q in %q", w, warns)
		}
	}
}

func TestWrongPackageManager(t *testing.T) {
	repo := model.Repo{PackageManagers: []model.PackageManager{
		{Name: "pnpm", Ecosystem: "node", Dir: ".", Source: model.Source{Path: "pnpm-lock.yaml"}},
		{Name: "uv", Ecosystem: "python", Dir: ".", Source: model.Source{Path: "uv.lock"}},
	}}
	doc := "Run `npm install` then `npm run build`.\n\n```sh\nyarn add left-pad\npip install -r requirements.txt\npip install uv\nnpm install -g pnpm\n```\n" +
		"Never use `npm install` here.\nDon't run `yarn install`.\n"
	f := checkDoc(t, repo, map[string]string{"CLAUDE.md": "@AGENTS.md\n" + doc, "AGENTS.md": "x\n"})
	wp := 0
	for _, x := range f {
		if x.Rule == RuleWrongPM {
			wp++
			if x.Severity != model.SeverityWarning {
				t.Error(x)
			}
		}
	}
	if wp != 3 || !has(f, RuleWrongPM, "`npm install`, but this repository uses pnpm (pnpm-lock.yaml)") ||
		!has(f, RuleWrongPM, "`yarn add`") || !has(f, RuleWrongPM, "`pip install`, but this repository uses uv (uv.lock)") {
		t.Errorf("%+v", f)
	}
	// uncertain package manager (no lockfile/version) is never contradicted
	repo.PackageManagers = []model.PackageManager{{Name: "npm", Ecosystem: "node", Dir: ".", Source: model.Source{Path: "package.json"}}}
	f = checkDoc(t, repo, map[string]string{"AGENTS.md": "`pnpm install`\n"})
	if count(f, RuleWrongPM) != 0 {
		t.Errorf("%+v", f)
	}
}

func TestAgentsNotLoaded(t *testing.T) {
	f := checkDoc(t, model.Repo{}, map[string]string{"AGENTS.md": "a\n", "CLAUDE.md": "# c\n", "GEMINI.md": "@AGENTS.md\n"})
	if count(f, RuleAgentsNotLoaded) != 1 || f[0].Path != "CLAUDE.md" || f[0].Severity != model.SeverityWarning {
		t.Errorf("%+v", f)
	}
	f = checkDoc(t, model.Repo{}, map[string]string{"AGENTS.md": "a\n", "CLAUDE.md": "@docs/shared.md\n", "docs/shared.md": "@../AGENTS.md\n"})
	if count(f, RuleAgentsNotLoaded) != 0 {
		t.Errorf("transitive import: %+v", f)
	}
	f = checkDoc(t, model.Repo{}, map[string]string{"CLAUDE.md": "# c\n"})
	if len(f) != 0 {
		t.Errorf("no AGENTS.md: %+v", f)
	}
	f = checkDoc(t, model.Repo{}, map[string]string{"AGENTS.md": "a\n", "CLAUDE.md": "see `@AGENTS.md` in a span\n"})
	if count(f, RuleAgentsNotLoaded) != 1 {
		t.Errorf("import inside code span must not count: %+v", f)
	}
}

func TestOversized(t *testing.T) {
	big := strings.Repeat("x", 100) + "\n"
	f := checkDoc(t, model.Repo{}, map[string]string{
		"AGENTS.md": strings.Repeat(big, 400),
		"CLAUDE.md": strings.Repeat("l\n", 201) + "@AGENTS.md\n",
		"GEMINI.md": strings.Repeat("l\n", 250) + "@AGENTS.md\n",
	})
	if count(f, RuleOversized) != 2 || !has(f, RuleOversized, "KiB") || !has(f, RuleOversized, "202 lines") {
		t.Errorf("%+v", f)
	}
}

func TestStaleBlock(t *testing.T) {
	repo := syncRepo()
	root, r := fixture(t, nil, repo)
	if _, err := Sync(root, r, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	r = refresh(t, root, repo)
	if f := Check(root, r, CheckOptions{}); len(f) != 0 {
		t.Fatalf("fresh: %+v", f)
	}
	// repo changes -> stale
	repo2 := syncRepo()
	repo2.Commands[0].Run = "pnpm vitest run"
	r2 := refresh(t, root, repo2)
	f := Check(root, r2, CheckOptions{})
	if len(f) != 1 || f[0].Rule != RuleStaleBlock || f[0].Severity != model.SeverityError || f[0].Line == 0 || !strings.Contains(f[0].Suggestion, "agentbrief sync") {
		t.Errorf("%+v", f)
	}
	// CRLF checkout of a fresh block is not stale
	write(t, root, "AGENTS.md", strings.ReplaceAll(read(t, root, "AGENTS.md"), "\n", "\r\n"))
	if f := Check(root, refresh(t, root, repo), CheckOptions{}); len(f) != 0 {
		t.Errorf("crlf: %+v", f)
	}
	// malformed markers
	write(t, root, "AGENTS.md", StartMarker+"\n")
	f = Check(root, refresh(t, root, repo), CheckOptions{})
	if len(f) != 1 || f[0].Rule != RuleStaleBlock || !strings.Contains(f[0].Message, "malformed") {
		t.Errorf("%+v", f)
	}
	// no block at all: not an error
	write(t, root, "AGENTS.md", "# hand written\n")
	if f := Check(root, refresh(t, root, repo), CheckOptions{}); len(f) != 0 {
		t.Errorf("%+v", f)
	}
}

func TestCheckNeverNil(t *testing.T) {
	root, r := fixture(t, nil, model.Repo{})
	if f := Check(root, r, CheckOptions{}); f == nil {
		t.Error("nil findings")
	}
}

func TestScopedCursorRulesPaths(t *testing.T) {
	// .cursor/rules/*.mdc paths are repo-root relative (scope), front matter ignored.
	f := checkDoc(t, model.Repo{}, map[string]string{
		".cursor/rules/ts.mdc": "---\nglobs: src/**/*.ts\nalwaysApply: false\n---\nSee `src/app.ts` and [x](src/app.ts).\n",
		"src/app.ts":           "x",
	})
	if len(f) != 0 {
		t.Errorf("%+v", f)
	}
}

func TestBareFilenameWithModernExtension(t *testing.T) {
	f := checkDoc(t, model.Repo{}, map[string]string{
		"CLAUDE.md":        "@AGENTS.md\nsee `vitest.config.mts`, `package.json`, `README.md`, `foo.py`, `my_app/x.ts`\n",
		"AGENTS.md":        "a\n",
		"README.md":        "r",
		"vitest.config.ts": "x",
	})
	if count(f, RuleMissingPath) != 2 || !has(f, RuleMissingPath, "`vitest.config.mts`") || !has(f, RuleMissingPath, "`package.json`") {
		t.Errorf("%+v", f)
	}
}

func TestSuggestionBeyondEditDistance(t *testing.T) {
	repo := model.Repo{
		PackageManagers: []model.PackageManager{{Name: "yarn", Ecosystem: "node", Dir: ".", Source: model.Source{Path: "yarn.lock"}}},
		Tasks:           []model.Task{npmTask("typecheck", "."), npmTask("test", "."), npmTask("build:app", "."), npmTask("build:lib", ".")},
	}
	f := checkDoc(t, repo, map[string]string{"package.json": "{}", "AGENTS.md": "`yarn test:typecheck` and `yarn run build`\n"})
	if !has(f, RuleUnknownScript, "did you mean `yarn typecheck`?") {
		t.Errorf("%+v", f)
	}
	for _, x := range f {
		if strings.Contains(x.Message, "`build`") && x.Suggestion != "" {
			t.Errorf("ambiguous suggestion must be empty: %+v", x)
		}
	}
}
