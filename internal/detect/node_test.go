package detect

import (
	"strings"
	"testing"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
)

// cmdSet renders commands as "dir|kind|run" for easy comparison.
func cmdSet(f *Facts) map[string]model.Command {
	m := map[string]model.Command{}
	for _, c := range f.Commands {
		m[c.Dir+"|"+c.Kind+"|"+c.Run] = c
	}
	return m
}

func wantCmds(t *testing.T, f *Facts, want ...string) {
	t.Helper()
	got := cmdSet(f)
	for _, w := range want {
		if _, ok := got[w]; !ok {
			t.Errorf("missing command %q; have:\n%s", w, joinKeys(got))
		}
	}
}

func noCmds(t *testing.T, f *Facts, unwanted ...string) {
	t.Helper()
	got := cmdSet(f)
	for _, w := range unwanted {
		if _, ok := got[w]; ok {
			t.Errorf("unexpected command %q", w)
		}
	}
}

func joinKeys(m map[string]model.Command) string {
	var ks []string
	for k := range m {
		ks = append(ks, "  "+k)
	}
	return strings.Join(ks, "\n")
}

func TestNodePackageManagerDetection(t *testing.T) {
	pkg := `{"name":"x","scripts":{"test":"vitest"}}`
	tests := []struct {
		name    string
		files   map[string]string
		wantPM  string
		wantVer string
	}{
		{"packageManager wins over lockfile", map[string]string{"package.json": `{"packageManager":"pnpm@9.12.0+sha512.abc"}`, "yarn.lock": ""}, "pnpm", "9.12.0"},
		{"pnpm lock", map[string]string{"package.json": pkg, "pnpm-lock.yaml": ""}, "pnpm", ""},
		{"yarn classic", map[string]string{"package.json": pkg, "yarn.lock": "# yarn lockfile v1"}, "yarn", ""},
		{"yarn berry", map[string]string{"package.json": pkg, "yarn.lock": "__metadata:\n  version: 6"}, "yarn", ""},
		{"npm lock", map[string]string{"package.json": pkg, "package-lock.json": "{}"}, "npm", ""},
		{"shrinkwrap", map[string]string{"package.json": pkg, "npm-shrinkwrap.json": "{}"}, "npm", ""},
		{"bun lockb", map[string]string{"package.json": pkg, "bun.lockb": ""}, "bun", ""},
		{"bun lock", map[string]string{"package.json": pkg, "bun.lock": ""}, "bun", ""},
		{"npm fallback", map[string]string{"package.json": pkg}, "npm", ""},
		{"pnpm workspace file hint", map[string]string{"package.json": pkg, "pnpm-workspace.yaml": "packages: []"}, "pnpm", ""},
		{"conflicting lockfiles", map[string]string{"package.json": pkg, "yarn.lock": "", "package-lock.json": "{}"}, "yarn", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := runOnly(tt.files, detectNode)
			if len(f.PackageManagers) != 1 {
				t.Fatalf("PMs = %+v", f.PackageManagers)
			}
			pm := f.PackageManagers[0]
			if pm.Name != tt.wantPM || pm.Version != tt.wantVer || pm.Ecosystem != "node" || pm.Dir != "." {
				t.Errorf("pm = %+v", pm)
			}
		})
	}
	f := runOnly(map[string]string{"package.json": pkg, "yarn.lock": "", "package-lock.json": "{}"}, detectNode)
	if len(f.Notes) != 1 || !strings.Contains(f.Notes[0].Text, "Conflicting lockfiles") {
		t.Errorf("notes = %+v", f.Notes)
	}
}

func TestNodeCommands(t *testing.T) {
	scripts := `{"name":"app","scripts":{
  "build":"tsc",
  "test":"vitest",
  "start":"node .",
  "install":"x",
  "lint":"eslint .",
  "custom":"echo hi"}}`
	tests := []struct {
		name  string
		pm    string // lockfile
		want  []string
		unwnt []string
	}{
		{"npm", "package-lock.json", []string{".|install|npm ci", ".|build|npm run build", ".|test|npm test", ".|dev|npm start", ".|install|npm run install", ".|lint|npm run lint"}, []string{".|build|npm build"}},
		{"pnpm", "pnpm-lock.yaml", []string{".|install|pnpm install", ".|build|pnpm build", ".|test|pnpm test", ".|install|pnpm run install", ".|dev|pnpm start"}, nil},
		{"yarn", "yarn.lock", []string{".|install|yarn install", ".|build|yarn build", ".|lint|yarn lint"}, nil},
		{"bun", "bun.lock", []string{".|install|bun install", ".|build|bun run build", ".|test|bun run test"}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := runOnly(map[string]string{"package.json": scripts, tt.pm: ""}, detectNode)
			wantCmds(t, f, tt.want...)
			noCmds(t, f, tt.unwnt...)
			noCmds(t, f, ".|run|"+"x")
			if len(f.Tasks) != 6 {
				t.Errorf("tasks = %d, want 6", len(f.Tasks))
			}
			c := cmdSet(f)[".|build|"+map[string]string{"npm": "npm run build", "pnpm": "pnpm build", "yarn": "yarn build", "bun": "bun run build"}[tt.name]]
			if c.Source.Path != "package.json" || c.Source.Line != 2 || c.Evidence[0] != model.EvidenceDeclared {
				t.Errorf("build source = %+v ev=%v", c.Source, c.Evidence)
			}
		})
	}
}

func TestNodeNpmInstallWithoutLock(t *testing.T) {
	f := runOnly(map[string]string{"package.json": `{}`}, detectNode)
	wantCmds(t, f, ".|install|npm install")
}

func TestNodePlaceholderTestSkipped(t *testing.T) {
	f := runOnly(map[string]string{"package.json": `{"scripts":{"test":"echo \"Error: no test specified\" && exit 1"}}`}, detectNode)
	for _, c := range f.Commands {
		if c.Kind == model.KindTest {
			t.Errorf("placeholder test emitted: %+v", c)
		}
	}
	if len(f.Tasks) != 1 {
		t.Errorf("task should still be inventoried")
	}
}

func TestNodeSingleTestRunners(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"vitest pnpm", map[string]string{"package.json": `{"devDependencies":{"vitest":"1"}}`, "pnpm-lock.yaml": ""}, `.|test-single|pnpm exec vitest run <file> -t "<name>"`},
		{"vitest config wins over jest dep", map[string]string{"package.json": `{"devDependencies":{"jest":"1"}}`, "vitest.config.ts": "", "package-lock.json": ""}, `.|test-single|npx vitest run <file> -t "<name>"`},
		{"jest npm", map[string]string{"package.json": `{"devDependencies":{"jest":"1"}}`}, `.|test-single|npx jest <file> -t "<name>"`},
		{"jest yarn", map[string]string{"package.json": `{"jest":{}}`, "yarn.lock": ""}, `.|test-single|yarn jest <file> -t "<name>"`},
		{"mocha bun", map[string]string{"package.json": `{"scripts":{"test":"mocha"}}`, "bun.lock": ""}, `.|test-single|bunx mocha <file> -g "<name>"`},
		{"node test", map[string]string{"package.json": `{"scripts":{"test":"node --test test/"}}`}, `.|test-single|node --test --test-name-pattern="<name>" <file>`},
		{"bun test", map[string]string{"package.json": `{"scripts":{"test":"bun test"}}`, "bun.lock": ""}, `.|test-single|bun test <file> -t "<name>"`},
		{"playwright", map[string]string{"package.json": `{}`, "playwright.config.ts": ""}, `.|test-single|npx playwright test <file> -g "<name>"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := runOnly(tt.files, detectNode)
			wantCmds(t, f, tt.want)
			c := cmdSet(f)[tt.want]
			if c.Evidence[0] != model.EvidenceConvention || c.Note == "" || c.Source.Path == "" {
				t.Errorf("bad evidence/note/source: %+v", c)
			}
		})
	}
	f := runOnly(map[string]string{"package.json": `{"scripts":{"build":"tsc"}}`}, detectNode)
	for _, c := range f.Commands {
		if c.Kind == model.KindTestOne {
			t.Errorf("no runner but single-test emitted: %+v", c)
		}
	}
}

func TestNodeTypecheck(t *testing.T) {
	f := runOnly(map[string]string{"package.json": `{"devDependencies":{"typescript":"5"}}`, "tsconfig.json": "{}", "pnpm-lock.yaml": ""}, detectNode)
	wantCmds(t, f, ".|typecheck|pnpm exec tsc --noEmit")
	f = runOnly(map[string]string{"package.json": `{"devDependencies":{"typescript":"5"},"scripts":{"typecheck":"tsc -b"}}`, "tsconfig.json": "{}"}, detectNode)
	noCmds(t, f, ".|typecheck|npx tsc --noEmit")
	wantCmds(t, f, ".|typecheck|npm run typecheck")
	f = runOnly(map[string]string{"package.json": `{}`, "tsconfig.json": "{}"}, detectNode)
	noCmds(t, f, ".|typecheck|npx tsc --noEmit")
}

func TestNodeToolchains(t *testing.T) {
	f := runOnly(map[string]string{
		"package.json": "{\n\"engines\":{\"node\":\">=18\"},\"volta\":{\"node\":\"20.1.0\"}}",
		".nvmrc":       "v20.11.0\n", ".node-version": "22\n",
	}, detectNode)
	got := map[string]string{}
	for _, tc := range f.Toolchains {
		if tc.Name != "node" {
			t.Errorf("name = %q", tc.Name)
		}
		got[tc.Source.Path+":"+tc.Version] = "x"
	}
	for _, w := range []string{".nvmrc:20.11.0", ".node-version:22", "package.json:>=18", "package.json:20.1.0"} {
		if got[w] == "" {
			t.Errorf("missing toolchain %s in %v", w, got)
		}
	}
}

func TestNodeWorkspaces(t *testing.T) {
	files := map[string]string{
		"package.json":               `{"name":"mono","workspaces":["packages/*","!packages/skip"],"scripts":{"build":"turbo build"}}`,
		"pnpm-lock.yaml":             "",
		"packages/a/package.json":    `{"name":"@m/a","scripts":{"test":"vitest","dev":"vite"}}`,
		"packages/b/package.json":    `{"name":"@m/b"}`,
		"packages/skip/package.json": `{"name":"@m/skip"}`,
		"packages/nopkg/README.md":   "",
		"examples/x/package.json":    `{"name":"ex"}`,
		"vitest.config.ts":           "",
	}
	f := runOnly(files, detectNode)
	dirs := map[string]model.Project{}
	for _, p := range f.Projects {
		dirs[p.Dir] = p
	}
	if len(dirs) != 3 || !dirs["."].IsWorkspaceRoot || dirs["packages/a"].Workspace != "." || dirs["packages/a"].Name != "@m/a" {
		t.Errorf("projects = %+v", f.Projects)
	}
	if len(f.PackageManagers) != 1 {
		t.Errorf("members should inherit the root PM: %+v", f.PackageManagers)
	}
	wantCmds(t, f, "packages/a|test|pnpm test", "packages/a|dev|pnpm dev", `packages/a|test-single|pnpm exec vitest run <file> -t "<name>"`, ".|build|pnpm build")
	if f.Name != "mono" {
		t.Errorf("Name = %q", f.Name)
	}
}

func TestNodePnpmWorkspaceYAML(t *testing.T) {
	files := map[string]string{
		"package.json":          `{"name":"mono"}`,
		"pnpm-workspace.yaml":   "packages:\n  - 'apps/**'\n  - '!apps/old'\n",
		"apps/web/package.json": `{"name":"web"}`,
		"apps/old/package.json": `{"name":"old"}`,
	}
	f := runOnly(files, detectNode)
	if len(f.Projects) != 2 {
		t.Errorf("projects = %+v", f.Projects)
	}
	if !f.Projects[0].IsWorkspaceRoot && !f.Projects[1].IsWorkspaceRoot {
		t.Error("no workspace root")
	}
}

func TestNodeStandalone(t *testing.T) {
	files := map[string]string{
		"go.mod": "module x", "web/package.json": `{"name":"web","scripts":{"build":"vite build"}}`, "web/pnpm-lock.yaml": "",
		"test/fixtures/a/package.json": `{}`, "examples/e/package.json": `{}`,
	}
	f := runOnly(files, detectNode)
	wantCmds(t, f, "web|build|pnpm build", "web|install|pnpm install")
	if len(f.Projects) != 1 {
		t.Errorf("projects = %+v", f.Projects)
	}
}

func TestNodeDeno(t *testing.T) {
	f := runOnly(map[string]string{"deno.jsonc": "{\n// c\n\"tasks\":{\"dev\":\"deno run -A main.ts\",},\n}"}, detectNode)
	wantCmds(t, f, ".|dev|deno task dev", ".|test|deno test")
	if len(f.Projects) != 1 || f.Projects[0].Ecosystem != "deno" {
		t.Errorf("projects = %+v", f.Projects)
	}
}

func TestNodeNegativeAndDeterministic(t *testing.T) {
	f := runOnly(map[string]string{"main.go": "package main", "go.mod": "module x"}, detectNode)
	if len(f.Commands)+len(f.Projects)+len(f.PackageManagers)+len(f.Tasks) != 0 {
		t.Errorf("false detection: %+v", f.Repo)
	}
	f = runOnly(map[string]string{"package.json": `{bad json`}, detectNode)
	if len(f.Projects) != 0 {
		t.Error("invalid json produced a project")
	}
	files := map[string]string{"package.json": `{"scripts":{"a":"1","test":"jest","build":"x","lint":"y","dev":"z"}}`}
	a, b := runOnly(files, detectNode), runOnly(files, detectNode)
	if len(a.Commands) != len(b.Commands) {
		t.Fatal("nondeterministic count")
	}
	for i := range a.Commands {
		if a.Commands[i].Run != b.Commands[i].Run {
			t.Errorf("order differs at %d", i)
		}
	}
}

func TestNodeSecretsRedactedInTaskBody(t *testing.T) {
	f := runOnly(map[string]string{"package.json": `{"scripts":{"deploy":"curl -H token=abcdef123456 x"}}`}, detectNode)
	if strings.Contains(f.Tasks[0].Body, "abcdef123456") {
		t.Errorf("body not redacted: %q", f.Tasks[0].Body)
	}
}
