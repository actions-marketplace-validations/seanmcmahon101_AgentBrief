package detect

import (
	"encoding/json"
	"path"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/redact"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { register("node", detectNode) }

const (
	maxNodeMembers     = 300
	maxNodeStandalones = 8
	maxBodyLen         = 160
)

// npmPlaceholderTest is what `npm init` writes; it is not a real test command.
var npmPlaceholderTest = regexp.MustCompile(`^echo\s+["']?Error: no test specified["']?\s*&&\s*exit\s+1$`)

// nodePkg is a parsed package.json.
type nodePkg struct {
	dir  string // "." for the root
	file string // path of package.json
	raw  string
	Name string `json:"name"`

	PackageManager string          `json:"packageManager"`
	Scripts        map[string]any  `json:"scripts"`
	Engines        map[string]any  `json:"engines"`
	Volta          map[string]any  `json:"volta"`
	Workspaces     json.RawMessage `json:"workspaces"`
	Deps           map[string]any  `json:"dependencies"`
	DevDeps        map[string]any  `json:"devDependencies"`
	Jest           json.RawMessage `json:"jest"`
}

func (p *nodePkg) script(name string) string {
	s, _ := p.Scripts[name].(string)
	return s
}

func (p *nodePkg) scriptNames() []string {
	names := make([]string, 0, len(p.Scripts))
	for n := range p.Scripts {
		if _, ok := p.Scripts[n].(string); ok {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	return names
}

func (p *nodePkg) hasDep(name string) bool {
	_, a := p.Deps[name]
	_, b := p.DevDeps[name]
	return a || b
}

func loadNodePkg(r *scan.Repo, dir string) *nodePkg {
	file := coreJoin(dir, "package.json")
	if !r.Has(file) {
		return nil
	}
	raw := r.ReadString(file)
	p := &nodePkg{dir: dir, file: file, raw: raw}
	if err := json.Unmarshal([]byte(raw), p); err != nil {
		return nil
	}
	return p
}

// coreJoin joins a repo-relative dir ("." for root) and a name.
func coreJoin(dir, name string) string {
	if dir == "." || dir == "" {
		return name
	}
	return dir + "/" + name
}

// nodePM is the resolved package manager for a directory tree.
type nodePM struct {
	name    string
	version string
	source  model.Source
	pkgLock bool // package-lock.json / npm-shrinkwrap.json present (npm ci is valid)
}

func (pm nodePM) exec() string {
	switch pm.name {
	case "pnpm":
		return "pnpm exec"
	case "yarn":
		return "yarn"
	case "bun":
		return "bunx"
	}
	return "npx"
}

var pnpmBuiltins = setOf("install", "i", "add", "update", "up", "upgrade", "remove", "rm", "uninstall", "link", "ln",
	"unlink", "outdated", "publish", "pack", "audit", "list", "ls", "why", "rebuild", "rb", "prune", "fetch",
	"import", "init", "exec", "dlx", "create", "deploy", "env", "store", "config", "patch", "run", "root", "bin",
	"licenses", "setup", "doctor", "server")

var yarnBuiltins = setOf("install", "add", "remove", "upgrade", "check", "link", "unlink", "init", "publish", "pack",
	"version", "info", "list", "why", "run", "config", "cache", "global", "audit", "exec", "workspaces",
	"workspace", "dlx", "up", "set", "plugin", "constraints", "dedupe", "rebuild", "patch", "node", "bin", "login", "logout")

func setOf(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// runScript formats the command that runs package.json script name.
func (pm nodePM) runScript(name string) string {
	switch pm.name {
	case "pnpm":
		if pnpmBuiltins[name] {
			return "pnpm run " + name
		}
		return "pnpm " + name
	case "yarn":
		if yarnBuiltins[name] {
			return "yarn run " + name
		}
		return "yarn " + name
	case "bun":
		return "bun run " + name
	}
	switch name {
	case "test", "start", "stop", "restart":
		return "npm " + name
	}
	return "npm run " + name
}

func (pm nodePM) install() string {
	switch pm.name {
	case "pnpm", "yarn", "bun":
		return pm.name + " install"
	}
	if pm.pkgLock {
		return "npm ci"
	}
	return "npm install"
}

// lockfileOrder is the preference when several lockfiles coexist.
var lockfileOrder = []struct{ file, pm string }{
	{"pnpm-lock.yaml", "pnpm"}, {"yarn.lock", "yarn"}, {"bun.lock", "bun"}, {"bun.lockb", "bun"},
	{"package-lock.json", "npm"}, {"npm-shrinkwrap.json", "npm"},
}

// resolveNodePM applies the priority: packageManager field > lockfile >
// workspace/config hints > npm fallback. ok is false only when nothing at all
// identifies the directory as using Node.
func resolveNodePM(r *scan.Repo, f *Facts, p *nodePkg) nodePM {
	pm := nodePM{}
	pm.pkgLock = r.Has(coreJoin(p.dir, "package-lock.json")) || r.Has(coreJoin(p.dir, "npm-shrinkwrap.json"))

	var locks []string
	seen := map[string]bool{}
	var lockPMs []string
	for _, l := range lockfileOrder {
		if r.Has(coreJoin(p.dir, l.file)) {
			locks = append(locks, coreJoin(p.dir, l.file))
			if !seen[l.pm] {
				seen[l.pm] = true
				lockPMs = append(lockPMs, l.pm)
			}
		}
	}
	if len(lockPMs) > 1 {
		f.AddNote(model.Note{
			Text:   "Conflicting lockfiles (" + strings.Join(baseNames(locks), ", ") + "); treating " + lockPMs[0] + " as the package manager",
			Source: model.Source{Path: locks[0]},
		})
	}

	if name, ver, ok := strings.Cut(p.PackageManager, "@"); ok || p.PackageManager != "" {
		name = strings.TrimSpace(name)
		if i := strings.IndexByte(ver, '+'); i >= 0 {
			ver = ver[:i]
		}
		if setOf("npm", "pnpm", "yarn", "bun")[name] {
			pm.name, pm.version = name, ver
			pm.source = model.Source{Path: p.file, Line: jsonKeyLine(p.raw, "", "packageManager")}
			return pm
		}
	}
	if len(lockPMs) > 0 {
		pm.name = lockPMs[0]
		pm.source = model.Source{Path: locks[0]}
		if pm.name == "yarn" && isYarnBerry(r, p.dir) {
			f.AddNote(model.Note{Text: "Yarn Berry (v2+): lockfile has __metadata or .yarnrc.yml exists", Source: model.Source{Path: coreJoin(p.dir, "yarn.lock")}})
		}
		return pm
	}
	switch {
	case r.Has(coreJoin(p.dir, "pnpm-workspace.yaml")):
		pm.name, pm.source = "pnpm", model.Source{Path: coreJoin(p.dir, "pnpm-workspace.yaml")}
	case r.Has(coreJoin(p.dir, ".yarnrc.yml")):
		pm.name, pm.source = "yarn", model.Source{Path: coreJoin(p.dir, ".yarnrc.yml")}
	default:
		pm.name, pm.source = "npm", model.Source{Path: p.file}
	}
	return pm
}

func isYarnBerry(r *scan.Repo, dir string) bool {
	if r.Has(coreJoin(dir, ".yarnrc.yml")) {
		return true
	}
	return strings.Contains(r.ReadString(coreJoin(dir, "yarn.lock")), "__metadata:")
}

func baseNames(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = path.Base(p)
	}
	return out
}

func detectNode(r *scan.Repo, f *Facts) {
	detectDeno(r, f)

	root := loadNodePkg(r, ".")
	var standalone []*nodePkg
	var members []*nodePkg
	if root != nil {
		members = nodeMembers(r, root)
	}
	memberSet := map[string]bool{}
	for _, m := range members {
		memberSet[m.dir] = true
	}
	standalone = nodeStandalones(r, root, memberSet)
	if root == nil && len(standalone) == 0 {
		return
	}

	var rootPM nodePM
	if root != nil {
		if root.Name != "" && (f.Name == "" || f.Name == path.Base(slash(r.Root()))) {
			f.Name = root.Name
		}
		rootPM = resolveNodePM(r, f, root)
		processNodeRoot(r, f, root, rootPM, len(members) > 0)
		for _, m := range members {
			processNodeMember(r, f, m, root, rootPM)
		}
	}
	for _, s := range standalone {
		pm := resolveNodePM(r, f, s)
		processNodeRoot(r, f, s, pm, false)
	}
}

// processNodeRoot records a project that owns its package manager: the repo
// root or a stand-alone package.json in a subdirectory.
func processNodeRoot(r *scan.Repo, f *Facts, p *nodePkg, pm nodePM, isWorkspace bool) {
	f.AddPackageManager(model.PackageManager{Name: pm.name, Ecosystem: "node", Version: pm.version, Dir: p.dir, Source: pm.source})
	f.AddProject(model.Project{Dir: p.dir, Name: nodeProjectName(p), Ecosystem: "node", Manifest: p.file, IsWorkspaceRoot: isWorkspace})
	addNodeToolchains(r, f, p)

	f.AddCommand(model.Command{Kind: model.KindInstall, Run: pm.install(), Dir: p.dir,
		Evidence: []string{model.EvidenceConvention}, Source: pm.source})
	addNodeScripts(r, f, p, pm)
	addNodeSingleTest(r, f, p, pm, nil)
	addNodeTypecheck(r, f, p, pm, nil)
}

func processNodeMember(r *scan.Repo, f *Facts, m, root *nodePkg, pm nodePM) {
	f.AddProject(model.Project{Dir: m.dir, Name: nodeProjectName(m), Ecosystem: "node", Manifest: m.file, Workspace: root.dir})
	addNodeScripts(r, f, m, pm)
	addNodeSingleTest(r, f, m, pm, root)
	addNodeTypecheck(r, f, m, pm, root)
}

func nodeProjectName(p *nodePkg) string {
	if p.Name != "" {
		return p.Name
	}
	if p.dir == "." {
		return ""
	}
	return path.Base(p.dir)
}

func addNodeToolchains(r *scan.Repo, f *Facts, p *nodePkg) {
	for _, name := range []string{".nvmrc", ".node-version"} {
		file := coreJoin(p.dir, name)
		if !r.Has(file) {
			continue
		}
		v := firstLine(r.ReadString(file))
		v = strings.TrimPrefix(v, "v")
		if v != "" && len(v) <= 40 {
			f.AddToolchain(model.Toolchain{Name: "node", Version: v, Source: model.Source{Path: file, Line: 1}})
		}
	}
	if v, ok := p.Engines["node"].(string); ok && v != "" && len(v) <= 40 {
		f.AddToolchain(model.Toolchain{Name: "node", Version: v, Source: model.Source{Path: p.file, Line: jsonKeyLine(p.raw, "engines", "node")}})
	}
	if v, ok := p.Volta["node"].(string); ok && v != "" {
		f.AddToolchain(model.Toolchain{Name: "node", Version: v, Source: model.Source{Path: p.file, Line: jsonKeyLine(p.raw, "volta", "node")}})
	}
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(s, "\n")
	return strings.TrimSpace(strings.TrimSuffix(s, "\r"))
}

// addNodeScripts records every script as a Task and the conventionally named
// ones as Commands.
func addNodeScripts(r *scan.Repo, f *Facts, p *nodePkg, pm nodePM) {
	for _, name := range p.scriptNames() {
		body := p.script(name)
		src := model.Source{Path: p.file, Line: jsonKeyLine(p.raw, "scripts", name)}
		t := model.Task{Runner: "npm-script", Name: name, Dir: p.dir, Source: src}
		if len(body) <= maxBodyLen {
			t.Body = redact.String(body)
		}
		f.AddTask(t)

		kind := KindForTaskName(name)
		if kind == "" || (name == "test" && npmPlaceholderTest.MatchString(strings.TrimSpace(body))) {
			continue
		}
		f.AddCommand(model.Command{Kind: kind, Run: pm.runScript(name), Dir: p.dir,
			Evidence: []string{model.EvidenceDeclared}, Source: src})
	}
}

// testRunner describes a JS test runner and how to run one test with it.
type testRunner struct {
	name   string
	source string // file that revealed it
	// format builds the single-test command; exec is the PM's binary runner.
	format func(pm nodePM) string
}

func addNodeSingleTest(r *scan.Repo, f *Facts, p *nodePkg, pm nodePM, root *nodePkg) {
	tr, ok := detectTestRunner(r, p, pm)
	if !ok && root != nil && p.script("test") != "" {
		tr, ok = detectTestRunner(r, root, pm)
	}
	if !ok {
		return
	}
	f.AddCommand(model.Command{
		Kind: model.KindTestOne, Run: tr.format(pm), Dir: p.dir,
		Evidence: []string{model.EvidenceConvention},
		Source:   model.Source{Path: tr.source},
		Note:     tr.name + "; replace <file> and <name>",
	})
}

var jsConfigExts = []string{".ts", ".js", ".mts", ".mjs", ".cts", ".cjs", ".json"}

// firstConfig returns the first existing dir/base+ext for the given extensions.
func firstConfig(r *scan.Repo, dir, base string, exts []string) string {
	for _, e := range exts {
		if f := coreJoin(dir, base+e); r.Has(f) {
			return f
		}
	}
	return ""
}

// detectTestRunner picks vitest > jest > mocha > node:test > bun test > playwright.
func detectTestRunner(r *scan.Repo, p *nodePkg, pm nodePM) (testRunner, bool) {
	testBodies := ""
	for _, n := range p.scriptNames() {
		if n == "test" || strings.HasPrefix(n, "test:") {
			testBodies += " " + p.script(n)
		}
	}
	has := func(re string) bool { return regexp.MustCompile(re).MatchString(testBodies) }

	if src := firstNonEmpty(firstConfig(r, p.dir, "vitest.config", jsConfigExts), firstConfig(r, p.dir, "vitest.workspace", jsConfigExts)); src != "" || p.hasDep("vitest") || has(`\bvitest\b`) {
		return testRunner{"vitest", firstNonEmpty(src, p.file), func(pm nodePM) string {
			return pm.exec() + ` vitest run <file> -t "<name>"`
		}}, true
	}
	if src := firstConfig(r, p.dir, "jest.config", jsConfigExts); src != "" || len(p.Jest) > 0 || p.hasDep("jest") || has(`\bjest\b`) {
		return testRunner{"jest", firstNonEmpty(src, p.file), func(pm nodePM) string {
			return pm.exec() + ` jest <file> -t "<name>"`
		}}, true
	}
	mocharc := ""
	for _, e := range []string{".js", ".cjs", ".json", ".jsonc", ".yml", ".yaml"} {
		if r.Has(coreJoin(p.dir, ".mocharc"+e)) {
			mocharc = coreJoin(p.dir, ".mocharc"+e)
			break
		}
	}
	if mocharc != "" || p.hasDep("mocha") || has(`\bmocha\b`) {
		return testRunner{"mocha", firstNonEmpty(mocharc, p.file), func(pm nodePM) string {
			return pm.exec() + ` mocha <file> -g "<name>"`
		}}, true
	}
	if has(`\bnode\s+(?:\S+\s+)*--test\b`) {
		return testRunner{"node:test", p.file, func(nodePM) string {
			return `node --test --test-name-pattern="<name>" <file>`
		}}, true
	}
	if has(`\bbun\s+test\b`) {
		return testRunner{"bun test", p.file, func(nodePM) string { return `bun test <file> -t "<name>"` }}, true
	}
	if src := firstConfig(r, p.dir, "playwright.config", jsConfigExts); src != "" || p.hasDep("@playwright/test") {
		return testRunner{"playwright", firstNonEmpty(src, p.file), func(pm nodePM) string {
			return pm.exec() + ` playwright test <file> -g "<name>"`
		}}, true
	}
	return testRunner{}, false
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// addNodeTypecheck adds `tsc --noEmit` only when the package has a tsconfig,
// TypeScript is a declared dependency (here or in the workspace root), and no
// script already covers type checking.
func addNodeTypecheck(r *scan.Repo, f *Facts, p *nodePkg, pm nodePM, root *nodePkg) {
	tsconfig := coreJoin(p.dir, "tsconfig.json")
	if !r.Has(tsconfig) {
		return
	}
	// A member must declare TypeScript itself: hoisting makes root's tsc
	// runnable, but a package without it is unlikely to be type-checked alone.
	if !p.hasDep("typescript") && (root == nil || p.dir != "." || !root.hasDep("typescript")) {
		return
	}
	for _, n := range p.scriptNames() {
		if KindForTaskName(n) == model.KindTypecheck {
			return
		}
	}
	f.AddCommand(model.Command{Kind: model.KindTypecheck, Run: pm.exec() + " tsc --noEmit", Dir: p.dir,
		Evidence: []string{model.EvidenceConvention}, Source: model.Source{Path: tsconfig}})
}

// nodeMembers expands the workspace globs of root into member packages.
func nodeMembers(r *scan.Repo, root *nodePkg) []*nodePkg {
	patterns := workspacePatterns(r, root)
	var include, exclude []string
	for _, pat := range patterns {
		pat = strings.TrimSpace(pat)
		if strings.HasPrefix(pat, "!") {
			exclude = append(exclude, strings.TrimPrefix(pat, "!"))
		} else if pat != "" {
			include = append(include, pat)
		}
	}
	seen := map[string]bool{}
	var dirs []string
	for _, pat := range include {
		for _, d := range r.GlobDirs(pat) {
			if d == "." || seen[d] || !r.Has(coreJoin(d, "package.json")) || nodeExcluded(exclude, d) || hasFixtureDir(d+"/x") {
				continue
			}
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	sort.Strings(dirs)
	if len(dirs) > maxNodeMembers {
		dirs = dirs[:maxNodeMembers]
	}
	var out []*nodePkg
	for _, d := range dirs {
		if m := loadNodePkg(r, d); m != nil {
			out = append(out, m)
		}
	}
	return out
}

func nodeExcluded(exclude []string, dir string) bool {
	for _, e := range exclude {
		if scan.MatchPath(e, dir) || scan.MatchPath(strings.TrimSuffix(e, "/")+"/**", dir) {
			return true
		}
	}
	return false
}

// workspacePatterns reads package.json "workspaces" (array or {packages}) and
// pnpm-workspace.yaml "packages".
func workspacePatterns(r *scan.Repo, root *nodePkg) []string {
	var pats []string
	if len(root.Workspaces) > 0 {
		var arr []string
		var obj struct {
			Packages []string `json:"packages"`
		}
		if json.Unmarshal(root.Workspaces, &arr) == nil {
			pats = append(pats, arr...)
		} else if json.Unmarshal(root.Workspaces, &obj) == nil {
			pats = append(pats, obj.Packages...)
		}
	}
	if r.Has("pnpm-workspace.yaml") {
		var ws struct {
			Packages []string `yaml:"packages"`
		}
		if yaml.Unmarshal([]byte(r.ReadString("pnpm-workspace.yaml")), &ws) == nil {
			pats = append(pats, ws.Packages...)
		}
	}
	return pats
}

// notStandalone are directory names that hold fixtures or samples rather than
// the project's own packages.
var notStandalone = setOf("test", "tests", "testdata", "fixtures", "fixture", "__tests__", "__fixtures__", "example",
	"examples", "sample", "samples", "template", "templates", "demo", "demos", "playground", "e2e", "spec", "specs", "snapshots", "benchmarks", "bench")

// nodeStandalones finds package.json files one or two levels down in repos
// that do not declare them as workspace members (e.g. a Go service with a
// web/ frontend). Fixture-like directories are skipped.
func nodeStandalones(r *scan.Repo, root *nodePkg, members map[string]bool) []*nodePkg {
	var out []*nodePkg
	for _, file := range r.Files() {
		if path.Base(file) != "package.json" {
			continue
		}
		dir := path.Dir(file)
		depth := strings.Count(dir, "/") + 1
		if dir == "." || depth > 2 || members[dir] || isVendoredPath(file) {
			continue
		}
		skip := false
		for _, seg := range strings.Split(dir, "/") {
			if notStandalone[strings.ToLower(seg)] || strings.HasPrefix(seg, ".") {
				skip = true
			}
		}
		if skip {
			continue
		}
		// Inside a workspace root only declared members count.
		if root != nil && len(workspacePatterns(r, root)) > 0 {
			continue
		}
		if p := loadNodePkg(r, dir); p != nil {
			out = append(out, p)
		}
		if len(out) >= maxNodeStandalones {
			break
		}
	}
	return out
}

// jsonKeyLine returns the 1-based line of key (optionally inside the object
// named section) in raw JSON text, or 0 when not found. It is a text search,
// good enough for pointing humans at a line.
func jsonKeyLine(raw, section, key string) int {
	start := 0
	if section != "" {
		loc := regexp.MustCompile(`"` + regexp.QuoteMeta(section) + `"\s*:`).FindStringIndex(raw)
		if loc == nil {
			return 0
		}
		start = loc[1]
	}
	loc := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `"\s*:`).FindStringIndex(raw[start:])
	if loc == nil {
		return 0
	}
	return strings.Count(raw[:start+loc[0]], "\n") + 1
}

// detectDeno records the basics for a root deno.json(c): project, tasks and
// the built-in `deno test|lint|fmt` commands.
func detectDeno(r *scan.Repo, f *Facts) {
	file := firstNonEmpty(map[bool]string{true: "deno.json"}[r.Has("deno.json")], map[bool]string{true: "deno.jsonc"}[r.Has("deno.jsonc")])
	if file == "" {
		return
	}
	raw := r.ReadString(file)
	var cfg struct {
		Name  string            `json:"name"`
		Tasks map[string]string `json:"tasks"`
	}
	if json.Unmarshal([]byte(stripJSONC(raw)), &cfg) != nil {
		return
	}
	name := cfg.Name
	if name == "" {
		name = "."
	}
	f.AddProject(model.Project{Dir: ".", Name: name, Ecosystem: "deno", Manifest: file})
	f.AddPackageManager(model.PackageManager{Name: "deno", Ecosystem: "deno", Dir: ".", Source: model.Source{Path: file}})
	names := make([]string, 0, len(cfg.Tasks))
	for n := range cfg.Tasks {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		src := model.Source{Path: file, Line: jsonKeyLine(raw, "tasks", n)}
		t := model.Task{Runner: "deno-task", Name: n, Dir: ".", Source: src}
		if len(cfg.Tasks[n]) <= maxBodyLen {
			t.Body = redact.String(cfg.Tasks[n])
		}
		f.AddTask(t)
		if kind := KindForTaskName(n); kind != "" {
			f.AddCommand(model.Command{Kind: kind, Run: "deno task " + n, Dir: ".", Evidence: []string{model.EvidenceDeclared}, Source: src})
		}
	}
	conv := model.Source{Path: file}
	for _, c := range []struct{ kind, run string }{
		{model.KindTest, "deno test"}, {model.KindLint, "deno lint"}, {model.KindFormat, "deno fmt"},
		{model.KindTestOne, "deno test <file>"},
	} {
		f.AddCommand(model.Command{Kind: c.kind, Run: c.run, Dir: ".", Evidence: []string{model.EvidenceConvention}, Source: conv})
	}
}

// stripJSONC removes // and /* */ comments and trailing commas so the stdlib
// decoder can read deno.jsonc.
func stripJSONC(s string) string {
	var b strings.Builder
	inStr := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inStr:
			b.WriteByte(c)
			if c == '\\' && i+1 < len(s) {
				i++
				b.WriteByte(s[i])
			} else if c == '"' {
				inStr = false
			}
		case c == '"':
			inStr = true
			b.WriteByte(c)
		case c == '/' && i+1 < len(s) && s[i+1] == '/':
			for i < len(s) && s[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
		case c == '/' && i+1 < len(s) && s[i+1] == '*':
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			i += end + 3
		default:
			b.WriteByte(c)
		}
	}
	return regexp.MustCompile(`,(\s*[}\]])`).ReplaceAllString(b.String(), "$1")
}
