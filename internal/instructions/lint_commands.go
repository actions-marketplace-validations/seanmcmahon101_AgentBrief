package instructions

import (
	"fmt"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
)

// ---- task index ------------------------------------------------------------

type taskIndex struct {
	by   map[string]map[string][]string // runner -> name -> dirs
	dirs map[string]map[string]bool     // runner -> dirs that define tasks
}

func newTaskIndex(repo *model.Repo) *taskIndex {
	t := &taskIndex{by: map[string]map[string][]string{}, dirs: map[string]map[string]bool{}}
	for _, k := range repo.Tasks {
		if t.by[k.Runner] == nil {
			t.by[k.Runner] = map[string][]string{}
			t.dirs[k.Runner] = map[string]bool{}
		}
		t.by[k.Runner][k.Name] = append(t.by[k.Runner][k.Name], k.Dir)
		t.dirs[k.Runner][k.Dir] = true
	}
	return t
}

func (t *taskIndex) any(runner string) bool { return len(t.by[runner]) > 0 }

// has reports whether name exists for runner; dir "" means anywhere.
func (t *taskIndex) has(runner, name, dir string) bool {
	for _, d := range t.by[runner][name] {
		if dir == "" || d == dir {
			return true
		}
	}
	return false
}

func (t *taskIndex) names(runner, dir string) []string {
	var out []string
	for n, ds := range t.by[runner] {
		for _, d := range ds {
			if dir == "" || d == dir {
				out = append(out, n)
				break
			}
		}
	}
	sort.Strings(out)
	return out
}

// ---- helpers ---------------------------------------------------------------

func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

func lastSeg(n string) string {
	if i := strings.LastIndexByte(n, ':'); i >= 0 {
		return n[i+1:]
	}
	return n
}

// closest returns the single best-matching candidate, or "" when there is none
// or the choice is ambiguous. It tries edit distance <= 2 first, then
// last-":"-segment equality and prefix/suffix containment.
func closest(name string, cands []string) string {
	best, bd, tie := "", 3, false
	for _, c := range cands {
		if c == name {
			continue
		}
		switch d := editDistance(name, c); {
		case d < bd:
			best, bd, tie = c, d, false
		case d == bd:
			tie = true
		}
	}
	if best != "" && !tie {
		return best
	}
	if best != "" {
		return ""
	}
	var seg, sub []string
	for _, c := range cands {
		if c == name || len(c) < 3 || len(name) < 3 {
			continue
		}
		if lastSeg(c) == lastSeg(name) || lastSeg(c) == name || lastSeg(name) == c {
			seg = append(seg, c)
		} else if strings.HasPrefix(c, name) || strings.HasSuffix(c, name) || strings.HasPrefix(name, c) || strings.HasSuffix(name, c) {
			sub = append(sub, c)
		}
	}
	for _, tier := range [][]string{seg, sub} {
		if len(tier) == 1 {
			return tier[0]
		}
		if len(tier) > 1 {
			return ""
		}
	}
	return ""
}

var segSplit = regexp.MustCompile(`&&|\|\||;|\|`)
var envAssign = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// segments splits a shell-ish line into command token lists.
func segments(s string) [][]string {
	if i := strings.Index(s, " #"); i >= 0 {
		s = s[:i]
	}
	var out [][]string
	for _, seg := range segSplit.Split(s, -1) {
		toks := strings.Fields(seg)
		for len(toks) > 0 {
			t := toks[0]
			switch {
			case t == "$" || t == ">" || t == "sudo" || t == "time" || t == "env" || t == "corepack" || t == "command" || envAssign.MatchString(t):
				toks = toks[1:]
				continue
			case t == "bundle" && len(toks) > 1 && toks[1] == "exec":
				toks = toks[2:]
				continue
			}
			break
		}
		if len(toks) > 0 {
			out = append(out, toks)
		}
	}
	return out
}

var placeholderWords = map[string]bool{
	"script": true, "name": true, "command": true, "cmd": true, "task": true, "target": true,
	"foo": true, "bar": true, "baz": true, "xxx": true, "xyz": true, "scriptname": true,
	"script-name": true, "your-script": true, "my-script": true, "target-name": true, "recipe": true,
}

func isPlaceholder(tok string) bool {
	if strings.ContainsAny(tok, "<>${}[]()*|\"'`\\!") || strings.Contains(tok, "...") || strings.Contains(tok, "…") {
		return true
	}
	return placeholderWords[strings.ToLower(tok)]
}

var (
	scriptNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_:.\-@/]*$`)
	targetNameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_\-:+]*$`)
)

// ---- builtins --------------------------------------------------------------

func set(words string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(words) {
		m[w] = true
	}
	return m
}

var npmBuiltins = set(`create access adduser audit bugs build cache ci completion config dedupe deprecate diff dist-tag dist-tags docs doctor edit exec explain explore find-dupes fund get help help-search hook init install install-ci-test install-test link ll login logout ls org outdated owner pack ping pkg prefix profile prune publish query rebuild repo restart root run run-script sbom search set shrinkwrap star stars start stop team test token undeprecate uninstall unpublish unstar update version view whoami
i add in ins inst insta instal isnt isnta isntal isntall install-clean ic cit clean-install clean-install-test sit la list ln rb rm remove r un unlink up upgrade udpate v t tst c find s se info show home issues innit x author ddp why rum urn apihelp verison birthday xmas`)

var pnpmBuiltins = set(`add i install install-test it update up upgrade remove rm uninstall un link ln unlink import rebuild rb prune fetch patch patch-commit patch-remove outdated list ls ll la why audit licenses run run-script exec dlx create start test t tst restart stop publish pack deploy init root bin store config c get set env setup doctor self-update server recursive multi m dedupe approve-builds ignored-builds cache completion help sbom catalog`)

var yarnBuiltins = set(`add bin cache check config create dedupe dlx exec explain global help import info init install licenses link list login logout node npm outdated owner pack patch patch-commit plugin rebuild remove run search set tag team unlink unplug up upgrade upgrade-interactive version versions why workspace workspaces constraints audit autoclean generate-lock-entry policies publish prune start-dev set-version rm`)

var bunBuiltins = set(`add a build create c dev exec init install i link outdated patch pm publish remove rm repl run test unlink update upgrade x audit why info completions feedback`)

var composerBuiltins = set(`about archive audit browse bump check-platform-reqs clear-cache clearcache config create-project depends diagnose dump-autoload dumpautoload exec fund global help home init install licenses list outdated prohibits reinstall remove require run-script search self-update selfupdate show status suggests update validate why why-not`)

// Well-known binaries that `pnpm X` / `yarn X` / `bun X` fall back to.
var knownBins = set(`vitest jest tsc eslint prettier playwright tsx ts-node turbo nx next vite webpack rollup esbuild biome oxlint prisma drizzle-kit tailwindcss storybook cypress mocha ava nodemon rimraf concurrently husky lint-staged changeset changesets wrangler expo astro nuxt nuxi tsup unbuild vue-tsc stylelint knip depcheck madge typedoc jiti swc babel parcel gulp grunt eslint_d tsdx ng nest remix svelte-kit sv wxt zx cross-env dotenv sst cdk serverless netlify vercel firebase supabase tauri electron electron-builder electron-forge lerna rush bit pm2 ts-prune ts-unused-exports commitlint standard-version semantic-release release-it np`)

var placeholderOK = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// ---- main entry ------------------------------------------------------------

type cmdCtx struct {
	c    *checker
	file model.InstructionFile
	line int
	text string // full command segment text for messages
	cwd  string // directory set by a preceding `cd` ("" none, "?" unknown)
}

func (c *checker) lintCommandText(f model.InstructionFile, line int, s string) {
	for _, toks := range segments(s) {
		if toks[0] == "cd" || toks[0] == "pushd" {
			c.cwd = nextCwd(c.cwd, toks)
			continue
		}
		x := cmdCtx{c: c, file: f, line: line, text: strings.Join(toks, " "), cwd: c.cwd}
		x.pathCommand(toks)
		if !strings.Contains(s, "COMPOSE_") {
			x.inventoryCommand(toks)
		}
		switch toks[0] {
		case "npm", "pnpm", "yarn", "bun":
			x.jsCommand(toks[0], toks[1:])
		case "make", "gmake":
			x.makeCommand(toks[1:])
		case "just":
			x.justCommand(toks[1:])
		case "task":
			x.taskCommand(toks[1:])
		case "mise":
			x.miseCommand(toks[1:])
		case "composer":
			x.composerCommand(toks[1:])
		case "rake":
			x.rakeCommand(toks[1:])
		}
	}
}

func (x cmdCtx) report(msg, suggestion string) {
	x.c.add(model.Finding{Rule: RuleUnknownScript, Severity: model.SeverityError, Path: x.file.Path,
		Line: x.line, Message: msg, Suggestion: suggestion})
}

func (x cmdCtx) scopeDir() string {
	if x.file.Scope == "" {
		return "."
	}
	return x.file.Scope
}

// manifestKnown reports whether one of the manifest names exists in the root,
// the file's scope dir, or any of the extra dirs.
func (x cmdCtx) manifestText(names []string, extraDirs ...string) (found bool, text string) {
	dirs := []string{".", x.scopeDir()}
	dirs = append(dirs, extraDirs...)
	seen := map[string]bool{}
	var sb strings.Builder
	for _, d := range dirs {
		if seen[d] {
			continue
		}
		seen[d] = true
		for _, n := range names {
			p := path.Join(d, n)
			if x.c.exists(p) {
				found = true
				sb.WriteString(x.c.readCached(p))
				sb.WriteByte('\n')
			}
		}
	}
	return found, sb.String()
}

func wordIn(text, name string) bool {
	re := regexp.MustCompile(`(^|[^A-Za-z0-9_-])` + regexp.QuoteMeta(name) + `([^A-Za-z0-9_-]|$)`)
	return re.MatchString(text)
}

// ---- JS package managers ---------------------------------------------------

func (x cmdCtx) jsCommand(tool string, args []string) {
	var builtins map[string]bool
	switch tool {
	case "npm":
		builtins = npmBuiltins
	case "pnpm":
		builtins = pnpmBuiltins
	case "yarn":
		builtins = yarnBuiltins
	case "bun":
		builtins = bunBuiltins
	}
	filter := ""
	valueFlags := map[string]bool{"--prefix": true, "--cwd": true, "-C": true, "--dir": true}
	filterFlags := map[string]bool{"--filter": true, "-F": true, "--workspace": true}
	if tool == "npm" {
		filterFlags["-w"] = true
	}
	// leading flags
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		a := args[0]
		switch {
		case strings.Contains(a, "="):
			if k, v, _ := strings.Cut(a, "="); k == "--filter" || k == "--workspace" {
				filter = v
			}
			args = args[1:]
		case filterFlags[a] && len(args) > 1:
			filter = args[1]
			args = args[2:]
		case valueFlags[a] && len(args) > 1:
			args = args[2:]
		default:
			args = args[1:]
		}
	}
	if len(args) == 0 {
		return
	}
	sub := args[0]
	rest := args[1:]
	if tool == "yarn" && sub == "workspace" {
		if len(rest) < 2 {
			return
		}
		filter = rest[0]
		sub, rest = rest[1], rest[2:]
	}
	if isPlaceholder(sub) {
		return
	}
	prefix := tool
	strict := false
	switch {
	case sub == "run" || sub == "run-script" || sub == "rum" || sub == "urn":
		for len(rest) > 0 && strings.HasPrefix(rest[0], "-") && rest[0] != "--" {
			a := rest[0]
			if (filterFlags[a] || valueFlags[a]) && !strings.Contains(a, "=") && len(rest) > 1 {
				if filterFlags[a] {
					filter = rest[1]
				}
				rest = rest[2:]
				continue
			}
			if k, v, ok := strings.Cut(a, "="); ok && (k == "--filter" || k == "--workspace") {
				filter = v
			}
			rest = rest[1:]
		}
		if len(rest) == 0 || rest[0] == "--" {
			return
		}
		prefix = tool + " " + sub
		if tool == "yarn" && filter != "" && args[0] == "workspace" {
			prefix = "yarn workspace " + args[1] + " run"
		}
		strict = tool == "npm" || tool == "pnpm"
		x.checkScript(tool, prefix, rest[0], filter, strict)
	case sub == "test" && (tool == "npm" || tool == "pnpm" || tool == "yarn"):
		x.checkScript(tool, tool, "test", filter, true)
	case builtins[sub]:
		return
	case tool == "npm":
		if !x.c.jsInventoryKnown(x.scopeDir()) {
			return
		}
		msg := fmt.Sprintf("`%s` is not an npm command", x.text)
		sug := ""
		if x.c.tix.has("npm-script", sub, "") {
			sug = fmt.Sprintf("did you mean `npm run %s`?", sub)
		} else if !scriptNameRe.MatchString(sub) || strings.ContainsAny(sub, "/@") {
			return
		}
		x.report(msg, sug)
	default:
		if strings.HasPrefix(sub, "-") || strings.ContainsAny(sub, "/@") {
			return
		}
		prefix = tool
		if tool == "yarn" && filter != "" && args[0] == "workspace" {
			prefix = "yarn workspace " + args[1]
		}
		x.checkScript(tool, prefix, sub, filter, false)
	}
}

func (c *checker) jsInventoryKnown(scope string) bool {
	return c.tix.any("npm-script") || c.exists("package.json") || c.exists(path.Join(scope, "package.json"))
}

func (x cmdCtx) checkScript(tool, prefix, name, filter string, strict bool) {
	if isPlaceholder(name) || !scriptNameRe.MatchString(name) {
		return
	}
	if strings.HasPrefix(name, "http") || (tool == "bun" && (strings.Contains(name, "/") || strings.Contains(path.Ext(name), "s"))) {
		return
	}
	c := x.c
	if !c.jsInventoryKnown(x.scopeDir()) {
		return
	}
	// resolve a package filter
	dir := ""
	if f := strings.TrimPrefix(filter, "./"); f != "" && !strings.ContainsAny(f, "*{}![]") && !strings.Contains(f, "...") {
		for _, p := range c.repo.Projects {
			if p.Name == f || p.Dir == f {
				dir = p.Dir
				break
			}
		}
	}
	if c.tix.has("npm-script", name, dir) {
		return
	}
	if dir == "" && c.tix.has("npm-script", name, "") {
		return
	}
	if !strict {
		// pnpm/yarn/bun fall back to local binaries.
		if knownBins[name] {
			return
		}
		for _, d := range []string{".", x.scopeDir(), dir} {
			if d != "" && c.exists(path.Join(d, "node_modules", ".bin", name)) {
				return
			}
		}
		// bare tool-like words (vitest-ish) with no near script: stay quiet.
		if closest(name, c.tix.names("npm-script", dir)) == "" && !strings.Contains(name, ":") && !commonScriptName(name) {
			return
		}
	}
	where := "any package.json"
	if dir != "" {
		where = "`" + path.Join(dir, "package.json") + "`"
	}
	sug := ""
	if s := closest(name, c.tix.names("npm-script", dir)); s != "" {
		sug = fmt.Sprintf("did you mean `%s %s`?", prefix, s)
	}
	x.report(fmt.Sprintf("`%s` runs script `%s`, which is not defined in %s", x.text, name, where), sug)
}

func commonScriptName(n string) bool {
	switch n {
	case "build", "dev", "start", "lint", "test", "format", "typecheck", "type-check", "check", "ci", "deploy", "serve", "watch", "clean", "docs", "preview", "generate", "codegen", "fmt", "bootstrap", "setup", "verify", "prepare", "release", "e2e", "storybook":
		return true
	}
	return false
}

// ---- make ------------------------------------------------------------------

func (x cmdCtx) makeCommand(args []string) {
	dir, explicit := "", false
	var targets []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			i = len(args)
		case a == "-C" || a == "--directory":
			if i+1 < len(args) {
				d := args[i+1]
				if isPlaceholder(d) || strings.ContainsAny(d, "$~") {
					return
				}
				dir = path.Join(dir, strings.TrimPrefix(d, "./"))
				explicit = true
				i++
			}
		case strings.HasPrefix(a, "--directory="):
			d := a[len("--directory="):]
			if isPlaceholder(d) || strings.ContainsAny(d, "$~") {
				return
			}
			dir = path.Join(dir, strings.TrimPrefix(d, "./"))
			explicit = true
		case a == "-f" || a == "--file" || a == "--makefile" || strings.HasPrefix(a, "--file=") || strings.HasPrefix(a, "--makefile="):
			return // a custom makefile: the root Makefile says nothing about it
		case strings.HasPrefix(a, "-f") && len(a) > 2 && !strings.HasPrefix(a, "--"):
			return
		case a == "-I" || a == "-o" || a == "-W":
			i++
		case a == "-j" || a == "-l":
			if i+1 < len(args) && regexp.MustCompile(`^\d+$`).MatchString(args[i+1]) {
				i++
			}
		case strings.HasPrefix(a, "-"):
		case strings.Contains(a, "="):
		default:
			if isPlaceholder(a) || !targetNameRe.MatchString(a) {
				i = len(args)
				break
			}
			targets = append(targets, a)
		}
	}
	// After `cd sub && make x` the Makefile that counts is sub/Makefile.
	switch x.cwd {
	case "?":
		return
	case "":
	default:
		dir = path.Join(x.cwd, dir)
	}
	dir = path.Clean(dir)
	if dir == "." {
		dir = ""
	}
	if dir != "" {
		resolved, ok := x.findDir(dir)
		if !ok {
			if explicit && !isGeneratedPath(dir+"/") && !strings.HasPrefix(dir, "..") {
				x.reportPath(model.SeverityError, fmt.Sprintf("`%s` uses `-C %s`, but directory `%s` does not exist", x.text, dir, dir))
			}
			return
		}
		dir = resolved
		x.makeIn(dir, targets)
		return
	}
	for _, t := range targets {
		x.checkTarget("make", "make", "target", []string{"Makefile", "makefile", "GNUmakefile"}, []string{"include ", "-include", "sinclude"}, t, dir)
	}
}

// makeIn checks targets against the Makefile of one specific directory only.
func (x cmdCtx) makeIn(dir string, targets []string) {
	var text string
	found := false
	for _, n := range []string{"Makefile", "makefile", "GNUmakefile"} {
		if x.c.exists(path.Join(dir, n)) {
			found = true
			text += x.c.readCached(path.Join(dir, n)) + "\n"
		}
	}
	if !found {
		return
	}
	for _, t := range targets {
		if isPlaceholder(t) || strings.Contains(t, "%") || x.c.tix.has("make", t, dir) || wordIn(text, t) {
			continue
		}
		skip := false
		for _, m := range []string{"include ", "-include", "sinclude"} {
			skip = skip || strings.Contains(text, m)
		}
		if skip {
			continue
		}
		sug := ""
		if s := closest(t, x.c.tix.names("make", dir)); s != "" {
			sug = fmt.Sprintf("did you mean `make -C %s %s`?", dir, s)
		}
		x.report(fmt.Sprintf("`%s` refers to target `%s`, which is not defined in the Makefile of `%s`", x.text, t, dir), sug)
	}
}

// checkTarget validates make/just/task/mise/composer names with manifest text fallback.
func (x cmdCtx) checkTarget(runner, prefix, noun string, manifests, giveUpMarkers []string, name, dir string) {
	if isPlaceholder(name) || strings.Contains(name, "%") {
		return
	}
	c := x.c
	var extra []string
	if dir != "" {
		extra = append(extra, dir)
	}
	for d := range c.tix.dirs[runner] {
		extra = append(extra, d)
	}
	found, text := x.manifestText(manifests, extra...)
	if !found && !c.tix.any(runner) {
		return // no manifest at all: cannot judge
	}
	if !c.tix.any(runner) && found && text == "" {
		return
	}
	if c.tix.has(runner, name, dir) {
		return
	}
	if dir != "" && !c.tix.dirs[runner][dir] {
		if ok, _ := (cmdCtx{c: c, file: model.InstructionFile{Scope: dir}}).manifestText(manifests); !ok {
			return
		}
	}
	if wordIn(text, name) {
		return
	}
	for _, m := range giveUpMarkers {
		if strings.Contains(text, m) {
			return
		}
	}
	names := c.tix.names(runner, dir)
	sug := ""
	if s := closest(name, names); s != "" {
		sug = fmt.Sprintf("did you mean `%s %s`?", prefix, s)
	}
	x.report(fmt.Sprintf("`%s` refers to %s `%s`, which is not defined in the %s", x.text, noun, name, manifestNoun(runner)), sug)
}

func manifestNoun(runner string) string {
	switch runner {
	case "make":
		return "Makefile"
	case "just":
		return "justfile"
	case "task":
		return "Taskfile"
	case "mise":
		return "mise config"
	case "composer":
		return "composer.json scripts"
	}
	return "repository"
}

// ---- just ------------------------------------------------------------------

func (x cmdCtx) justCommand(args []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-f" || a == "--justfile" || a == "-d" || a == "--working-directory" || a == "--shell" || a == "--chooser" || a == "--color" || a == "--dotenv-path":
			i++
		case a == "--set":
			i += 2
		case strings.HasPrefix(a, "-"):
		case strings.Contains(a, "="):
		default:
			if isPlaceholder(a) || !regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_-]*$`).MatchString(a) {
				return
			}
			x.checkTarget("just", "just", "recipe", []string{"justfile", "Justfile", ".justfile"}, []string{"import ", "mod "}, a, "")
			return
		}
	}
}

// ---- task (go-task) --------------------------------------------------------

func (x cmdCtx) taskCommand(args []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return
		case a == "-t" || a == "--taskfile" || a == "-d" || a == "--dir" || a == "-o" || a == "--output" || a == "-C" || a == "--concurrency":
			i++
		case strings.HasPrefix(a, "-"):
			if a == "-l" || a == "--list" || a == "-a" || a == "--list-all" || a == "--init" || a == "-i" {
				return
			}
		case strings.Contains(a, "="):
		default:
			if isPlaceholder(a) || !scriptNameRe.MatchString(a) || strings.ContainsAny(a, "/") {
				return
			}
			x.checkTarget("task", "task", "task", []string{"Taskfile.yml", "Taskfile.yaml", "taskfile.yml", "taskfile.yaml", "Taskfile.dist.yml", "Taskfile.dist.yaml"}, []string{"includes:"}, a, "")
		}
	}
}

// ---- mise ------------------------------------------------------------------

func (x cmdCtx) miseCommand(args []string) {
	if len(args) < 2 || (args[0] != "run" && args[0] != "r") {
		return
	}
	for _, d := range []string{".mise/tasks", "mise-tasks", ".config/mise/tasks", "mise/tasks"} {
		if x.c.exists(d) || x.c.exists(path.Join(x.scopeDir(), d)) {
			return
		}
	}
	for _, a := range args[1:] {
		if a == "--" || a == ":::" {
			return
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		if isPlaceholder(a) || !scriptNameRe.MatchString(a) || strings.Contains(a, "/") {
			return
		}
		x.checkTarget("mise", "mise run", "task", []string{"mise.toml", ".mise.toml", ".config/mise.toml", "mise.local.toml"}, []string{"includes", "task_config"}, a, "")
		return
	}
}

// ---- composer --------------------------------------------------------------

func (x cmdCtx) composerCommand(args []string) {
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if (args[0] == "-d" || args[0] == "--working-dir") && len(args) > 1 {
			args = args[2:]
		} else {
			args = args[1:]
		}
	}
	if len(args) == 0 {
		return
	}
	sub := args[0]
	prefix := "composer"
	name := sub
	switch sub {
	case "run-script", "run":
		rest := args[1:]
		for len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			return
		}
		name, prefix = rest[0], "composer "+sub
	default:
		if composerBuiltins[sub] || strings.HasPrefix(sub, "-") {
			return
		}
		if found, text := x.manifestText([]string{"composer.json"}); !found ||
			strings.Contains(text, "plugin") || strings.Contains(text, "symfony/flex") {
			return
		}
	}
	if isPlaceholder(name) || !scriptNameRe.MatchString(name) || strings.Contains(name, "/") {
		return
	}
	if !x.c.tix.any("composer") {
		return
	}
	x.checkTarget("composer", prefix, "script", []string{"composer.json"}, nil, name, "")
}

// ---- rake ------------------------------------------------------------------

func (x cmdCtx) rakeCommand(args []string) {
	for _, a := range args {
		if strings.HasPrefix(a, "-") || strings.Contains(a, "=") {
			continue
		}
		if isPlaceholder(a) || !targetNameRe.MatchString(a) || strings.Contains(a, ":") {
			return
		}
		switch a {
		case "test", "spec", "default", "routes", "stats", "notes", "about", "console":
			return
		}
		found, text := x.manifestText([]string{"Rakefile", "rakefile", "Rakefile.rb"})
		if !found || !x.c.tix.any("rake") || strings.Contains(text, "require") || strings.Contains(text, "load ") ||
			strings.Contains(text, "Rails") || strings.Contains(text, "import ") || wordIn(text, a) || x.c.tix.has("rake", a, "") {
			return
		}
		x.report(fmt.Sprintf("`%s` refers to rake task `%s`, which is not defined in the Rakefile", x.text, a), "")
		return
	}
}
