package detect

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { registerLate("ci-corroboration", finalizeCI) }

// ciCandidate is one command a CI step runs, with the directory it runs in.
type ciCandidate struct {
	dir     string   // "." for the repo root
	display []string // tokens as written (env assignments stripped)
	canon   []string // normalised tokens used for comparison
	src     model.Source
}

// finalizeCI marks commands that CI also runs, then adds well-known CI
// invocations that no detector produced.
func finalizeCI(r *scan.Repo, f *Facts) {
	var cands []ciCandidate
	for _, step := range f.CI {
		cands = append(cands, ciCandidates(step)...)
	}
	existing := len(f.Commands) // detector-made commands; CI-only additions come after
	var additions []pendingCI
	for _, c := range cands {
		canon, subset := ciStripSubset(c.canon)
		kind := ciKind(canon)
		restricted := kind == model.KindInstall && ciRestrictsDeps(canon)
		matched := false
		for i := 0; i < existing; i++ {
			cmd := &f.Commands[i]
			if cmd.Dir != c.dir || strings.ContainsAny(cmd.Run, "<>") {
				continue
			}
			ct := finCanon(shellFields(cmd.Run))
			if !hasTokenPrefix(canon, ct) {
				continue
			}
			if restricted && len(ct) != len(canon) {
				// `uv sync --no-dev ...` is a narrower install than the
				// project's own; do not let it vouch for the base command.
				matched = true
				continue
			}
			if kind == model.KindInstall && ciStrictInstall(canon) && len(ct) != len(canon) {
				continue // kept as its own, more specific, fact below
			}
			// CI runs the command, possibly with extra flags: corroborate
			// the base command rather than adding a near-duplicate.
			cmd.Evidence = mergeEvidence(cmd.Evidence, []string{model.EvidenceCI})
			matched = true
		}
		if matched {
			continue
		}
		if kind != "" && !ciUnsafe(c.display) {
			// A cd into a directory that is not in the repo (a second
			// checkout, say) says nothing about this repository.
			if subset && hasKindIn(f, kind, c.dir, len(f.Commands)) {
				continue // a subset run must not become the project's command
			}
			if c.dir != "." && !r.IsDir(c.dir) {
				continue
			}
			if kind == model.KindInstall && hasOtherInstaller(f, c.dir, existing, canon[0]) {
				continue
			}
			if restricted && hasKindIn(f, model.KindInstall, c.dir, existing) {
				continue
			}
			additions = append(additions, pendingCI{kind, c})
		}
	}
	// Workflows differ in relevance (a docs-deploy job installs different
	// things from the test job). Prefer test/build-ish ones and cap each
	// kind per directory so the inventory stays small.
	sort.SliceStable(additions, func(i, j int) bool { return ciRelevance(additions[i].c) < ciRelevance(additions[j].c) })
	perKind := map[string]int{}
	for _, a := range additions {
		key := a.kind + "|" + a.c.dir
		run := strings.Join(a.c.display, " ")
		if perKind[key] >= maxCIOnlyPerKind && !hasCommand(f, a.kind, a.c.dir, run) {
			continue
		}
		if !hasCommand(f, a.kind, a.c.dir, run) {
			perKind[key]++
		}
		f.AddCommand(model.Command{Kind: a.kind, Run: run, Dir: a.c.dir,
			Evidence: []string{model.EvidenceCI}, Source: a.c.src})
	}
}

// maxCIOnlyPerKind bounds CI-derived additions per (kind, directory).
const maxCIOnlyPerKind = 3

type pendingCI struct {
	kind string
	c    ciCandidate
}

// hasKindIn reports whether one of the first n commands has this kind and dir.
func hasKindIn(f *Facts, kind, dir string, n int) bool {
	for _, c := range f.Commands[:n] {
		if c.Kind == kind && c.Dir == dir {
			return true
		}
	}
	return false
}

var restrictingFlags = []string{"--no-dev", "--only", "--production", "--prod", "--omit=dev", "--no-default-groups"}

// ciRestrictsDeps reports whether an install skips part of the dependencies.
func ciRestrictsDeps(canon []string) bool {
	for _, t := range canon {
		for _, r := range restrictingFlags {
			if t == r || strings.HasPrefix(t, r+"=") || (r == "--only" && strings.HasPrefix(t, "--only-")) {
				return true
			}
		}
	}
	return false
}

// ciStrictInstall marks installs whose CI form is materially different from
// the plain command (lockfile-enforcing), worth keeping as their own fact.
func ciStrictInstall(canon []string) bool {
	for _, t := range canon {
		if t == "--frozen-lockfile" || t == "--immutable" {
			return true
		}
	}
	return false
}

// hasOtherInstaller reports whether the project already has an install
// command from a different tool (pip in CI next to uv in the repo).
func hasOtherInstaller(f *Facts, dir string, n int, tool string) bool {
	other := false
	for _, c := range f.Commands[:n] {
		if c.Kind != model.KindInstall || c.Dir != dir {
			continue
		}
		t := finCanon(shellFields(c.Run))
		if len(t) > 0 && t[0] == tool {
			return false
		}
		other = true
	}
	return other
}

func hasCommand(f *Facts, kind, dir, run string) bool {
	for _, c := range f.Commands {
		if c.Kind == kind && c.Dir == dir && c.Run == run {
			return true
		}
	}
	return false
}

var ciRelevantWords = []string{"test", "ci", "check", "lint", "build", "main", "verify"}

// ciRelevance ranks a candidate by its workflow file and job names: 0 for
// test/build-like, 1 for everything else (docs, release, translate, ...).
func ciRelevance(c ciCandidate) int {
	name := strings.ToLower(path.Base(c.src.Path))
	for _, w := range []string{"doc", "translate", "release", "deploy", "publish", "label", "comment"} {
		if strings.Contains(name, w) {
			return 2
		}
	}
	for _, w := range ciRelevantWords {
		if strings.Contains(name, w) {
			return 0
		}
	}
	return 1
}

// hasTokenPrefix reports whether prefix is a token-wise prefix of tokens, so
// "cargo test" matches "cargo test --workspace" but "make test" does not
// match "make test-integration".
func hasTokenPrefix(tokens, prefix []string) bool {
	if len(prefix) == 0 || len(prefix) > len(tokens) {
		return false
	}
	for i := range prefix {
		if tokens[i] != prefix[i] {
			return false
		}
	}
	return true
}

// ciUnsafe rejects commands that depend on shell state we cannot see.
func ciUnsafe(tokens []string) bool {
	s := strings.Join(tokens, " ")
	return strings.Contains(s, "${{") || strings.ContainsAny(s, "$`") || strings.Contains(s, "<<")
}

// ---- turning a CI step into candidates ----

// ciCandidates splits a step's run line on &&, ||, ; and | and tracks `cd`,
// so "cd web && npm test" yields npm test in web.
func ciCandidates(step model.CIStep) []ciCandidate {
	dir := step.Dir
	if dir == "" {
		dir = "."
	}
	var out []ciCandidate
	known := true
	for _, seg := range splitShellSegments(step.Run) {
		toks := shellFields(seg)
		toks = stripEnvPrefix(toks)
		if len(toks) == 0 {
			continue
		}
		if toks[0] == "cd" {
			if len(toks) == 2 && !strings.ContainsAny(toks[1], "$`") && !strings.HasPrefix(toks[1], "/") && !strings.HasPrefix(toks[1], "..") && !strings.HasPrefix(toks[1], "~") {
				dir = path.Join(dir, strings.Trim(toks[1], `"'`))
			} else {
				known = false
			}
			continue
		}
		if !known {
			continue
		}
		out = append(out, ciCandidate{dir: dir, display: toks, canon: finCanon(toks), src: step.Source})
	}
	return out
}

var envAssign = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// stripEnvPrefix drops `env`, `VAR=x` assignments and `time` before a command.
func stripEnvPrefix(toks []string) []string {
	for len(toks) > 0 && (toks[0] == "env" || toks[0] == "time" || envAssign.MatchString(toks[0])) {
		toks = toks[1:]
	}
	return toks
}

// splitShellSegments splits on && || ; | outside quotes.
func splitShellSegments(s string) []string {
	var out []string
	var cur strings.Builder
	var quote byte
	flush := func() {
		if t := strings.TrimSpace(cur.String()); t != "" {
			out = append(out, t)
		}
		cur.Reset()
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			cur.WriteByte(c)
			if c == '\\' && quote == '"' && i+1 < len(s) {
				i++
				cur.WriteByte(s[i])
			} else if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
			cur.WriteByte(c)
		case c == '&' && i+1 < len(s) && s[i+1] == '&', c == '|' && i+1 < len(s) && s[i+1] == '|':
			flush()
			i++
		case c == ';' || c == '|':
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

// shellFields splits on whitespace, keeping quoted text inside one token.
func shellFields(s string) []string {
	var out []string
	var cur strings.Builder
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			cur.WriteByte(c)
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
			cur.WriteByte(c)
		case c == ' ' || c == '\t' || c == '\n':
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// ---- canonical form ----

var uvValueFlags = setOf("--group", "--extra", "--with")

var uvSwitches = setOf("--locked", "--frozen", "--no-sync", "--isolated", "--no-dev", "--all-extras", "--all-groups", "--no-default-groups")

var pyRunners = setOf("uv", "poetry", "pdm", "pipenv", "hatch")

// finCanon normalises a command so equivalent spellings compare equal:
// `npm run test` == `npm test`, `pnpm run x` == `pnpm x`, and Python tools
// compare equal with or without `uv run` / `python -m`.
func finCanon(toks []string) []string {
	toks = append([]string(nil), toks...)
	if len(toks) >= 2 && pyRunners[toks[0]] && toks[1] == "run" {
		// Only strip the wrapper when its flags are plain switches; flags
		// like --directory change where the command runs.
		rest := toks[2:]
		for len(rest) > 0 && (uvSwitches[rest[0]] || (uvValueFlags[rest[0]] && len(rest) > 1)) {
			if uvValueFlags[rest[0]] {
				rest = rest[1:]
			}
			rest = rest[1:]
		}
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			toks = rest
		}
	}
	if len(toks) >= 3 && (toks[0] == "python" || toks[0] == "python3") && toks[1] == "-m" {
		toks = toks[2:]
	}
	if len(toks) >= 2 && (toks[0] == "bash" || toks[0] == "sh" || toks[0] == "zsh") && !strings.HasPrefix(toks[1], "-") {
		toks = toks[1:]
	}
	if len(toks) == 0 {
		return toks
	}
	if strings.Contains(strings.TrimPrefix(toks[0], "./"), "/") {
		// scripts/test, ./scripts/test and scripts/test.sh are one script.
		toks[0] = strings.TrimSuffix(strings.TrimSuffix(strings.TrimPrefix(toks[0], "./"), ".sh"), ".bash")
	}
	if len(toks) >= 2 && toks[0] == "tox" && toks[1] == "run" {
		toks = append([]string{"tox"}, toks[2:]...)
	}
	switch toks[0] {
	case "./mvnw", "mvnw", "mvnw.cmd":
		toks[0] = "mvn"
	case "./gradlew", "gradlew", "gradlew.bat":
		toks[0] = "gradle"
	}
	toks = dropNoise(toks)
	switch toks[0] {
	case "npm":
		if len(toks) >= 3 && (toks[1] == "run" || toks[1] == "run-script") {
			switch toks[2] {
			case "test", "start", "stop", "restart":
				toks = append([]string{"npm"}, toks[2:]...)
			default:
				toks = append([]string{"npm", "run"}, toks[2:]...)
			}
		}
	case "pnpm", "yarn":
		if len(toks) >= 3 && (toks[1] == "run" || toks[1] == "run-script") {
			toks = append([]string{toks[0]}, toks[2:]...)
		}
		if toks[0] == "yarn" && (len(toks) == 1 || strings.HasPrefix(toks[1], "-")) {
			toks = append([]string{"yarn", "install"}, toks[1:]...)
		}
	}
	return toks
}

// noiseFlags change verbosity or interactivity, not what a command does.
var noiseFlags = setOf("--verbose", "-v", "-B", "--batch-mode", "-q", "--quiet", "--no-daemon", "--stacktrace",
	"-ntp", "--no-transfer-progress", "--locked", "--color=always", "--color=never", "--console=plain")

// dropNoise removes noise flags, and the leading `clean` goal of mvn/gradle.
func dropNoise(toks []string) []string {
	out := toks[:1:1]
	for i, t := range toks[1:] {
		if noiseFlags[t] || (i == 0 && t == "clean" && (toks[0] == "mvn" || toks[0] == "gradle")) {
			continue
		}
		out = append(out, t)
	}
	return out
}

// subsetFlags select part of a build or test run. They take a value unless
// written as flag=value or glued (-Dtest=Foo).
var subsetFlags = setOf("--projects", "-pl", "-P", "--activate-profiles", "--package", "-p", "--filter", "-F",
	"-k", "--tests", "--test", "-run", "--features", "--also-make", "-am", "--bin", "--manifest-path", "--lib", "--doc", "--example", "--bench")

var subsetBoolean = setOf("--also-make", "-am", "--lib", "--doc")

// ciStripSubset removes subset-selecting flags (and their values) and reports
// whether any were present. A subset run is not the project's general command.
func ciStripSubset(canon []string) (rest []string, subset bool) {
	for i := 0; i < len(canon); i++ {
		t := canon[i]
		name, _, hasEq := strings.Cut(t, "=")
		switch {
		case strings.HasPrefix(t, "-Dtest") || strings.HasPrefix(t, "-Dit.test"):
			subset = true
		case subsetFlags[name]:
			subset = true
			if !hasEq && !subsetBoolean[name] && i+1 < len(canon) {
				i++
			}
		default:
			rest = append(rest, t)
		}
	}
	return rest, subset
}

// ---- recognising well-known CI commands ----

var ciRules = []struct {
	kind string
	re   *regexp.Regexp
}{
	{model.KindInstall, regexp.MustCompile(`^(?:npm ci|pnpm install|yarn install|bun install|uv sync|poetry install|bundle install|go mod download)(?: |$)`)},
	{model.KindTest, regexp.MustCompile(`^(?:go test|cargo test|cargo nextest run|pytest|npm test|pnpm test|yarn test|bun test|make test|just test|dotnet test|bundle exec rspec|mix test)(?: |$)`)},
	{model.KindTest, regexp.MustCompile(`^gradle (?:\S+ )*?(?:test|check)(?: |$)`)},
	{model.KindTest, regexp.MustCompile(`^mvn (?:\S+ )*?(?:test|verify)(?: |$)`)},
	{model.KindLint, regexp.MustCompile(`^(?:golangci-lint run|cargo clippy|ruff check|(?:npx |pnpm exec |yarn |bunx )?eslint)(?: |$)`)},
	{model.KindFormat, regexp.MustCompile(`^(?:cargo fmt .*--check|gofmt -l|ruff format .*--check|(?:npx |pnpm exec |yarn |bunx )?prettier .*(?:--check|-c)\b)`)},
	{model.KindTypecheck, regexp.MustCompile(`^(?:mypy|pyright)(?: |$)`)},
	{model.KindTypecheck, regexp.MustCompile(`^(?:npx |pnpm exec |yarn |bunx )?tsc\b.*--noEmit(?: |$)`)},
}

var pipProjectArg = regexp.MustCompile(`^(?:-e|-r\S*|\.|\.\[\S+\]|\S*requirements\S*\.txt)$`)

// ciKind classifies canonical tokens, or returns "" when not well known.
func ciKind(canon []string) string {
	s := strings.Join(canon, " ")
	// pip install is only a project install when it targets the project or a
	// requirements file; `pip install tox` installs a tool.
	if len(canon) >= 3 && canon[0] == "pip" && canon[1] == "install" {
		for _, t := range canon[2:] {
			if pipProjectArg.MatchString(t) {
				return model.KindInstall
			}
		}
		return ""
	}
	for _, rule := range ciRules {
		if rule.re.MatchString(s) {
			return rule.kind
		}
	}
	return ""
}

// ---- oversized manifests ----

func init() { registerLate("oversized-manifests", noteOversized) }

var manifestNames = setOf("package.json", "pyproject.toml", "Cargo.toml", "pom.xml", "go.mod", "build.gradle",
	"build.gradle.kts", "settings.gradle", "settings.gradle.kts", "composer.json", "Gemfile", "mix.exs", "Package.swift",
	"Makefile", "justfile", "Taskfile.yml", "Taskfile.yaml", "CMakeLists.txt", "deno.json", "tox.ini", "noxfile.py",
	".gitlab-ci.yml", "docker-compose.yml", "compose.yaml", "Dockerfile")

const maxOversizedNotes = 5

// noteOversized tells the reader when a file we would have parsed was too big
// to read, instead of silently producing nothing for it.
func noteOversized(r *scan.Repo, f *Facts) {
	n := 0
	for _, p := range r.Files() {
		base := path.Base(p)
		isCI := strings.HasPrefix(p, ".github/workflows/") && (strings.HasSuffix(p, ".yml") || strings.HasSuffix(p, ".yaml"))
		if !(manifestNames[base] || isCI || strings.HasSuffix(base, ".csproj")) || isVendoredPath(p) || hasFixtureDir(p) {
			continue
		}
		if r.Size(p) > scan.MaxReadBytes {
			f.AddNote(model.Note{Text: "`" + p + "` is larger than 1 MiB and was not parsed", Source: model.Source{Path: p}})
			if n++; n >= maxOversizedNotes {
				return
			}
		}
	}
}

// ---- example/fixture projects ----

func init() { registerLate("a-prune-examples", pruneExampleProjects) }

var exampleDirs = setOf("example", "examples", "_examples", "testdata", "fixtures", "test-fixtures", "samples", "sample")

func inExampleDir(dir string) bool {
	for _, seg := range strings.Split(dir, "/") {
		if exampleDirs[strings.ToLower(seg)] {
			return true
		}
	}
	return false
}

// pruneExampleProjects drops stand-alone projects (and their package managers
// and commands) that live in example or fixture directories: they are
// samples, not parts of the project. Workspace members are kept.
func pruneExampleProjects(_ *scan.Repo, f *Facts) {
	drop := map[string]bool{}
	var keep []model.Project
	for _, p := range f.Projects {
		if p.Dir != "." && p.Workspace == "" && !p.IsWorkspaceRoot && inExampleDir(p.Dir) {
			drop[p.Dir] = true
			continue
		}
		keep = append(keep, p)
	}
	if len(drop) == 0 {
		return
	}
	f.Projects = keep
	// A directory still owned by a kept project (another ecosystem) stays.
	for _, p := range keep {
		delete(drop, p.Dir)
	}
	pms := f.PackageManagers[:0]
	for _, pm := range f.PackageManagers {
		if !drop[pm.Dir] {
			pms = append(pms, pm)
		}
	}
	f.PackageManagers = pms
	cmds := f.Commands[:0]
	for _, c := range f.Commands {
		if !drop[c.Dir] {
			cmds = append(cmds, c)
		}
	}
	f.Commands = cmds
}
