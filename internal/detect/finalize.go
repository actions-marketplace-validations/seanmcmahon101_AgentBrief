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
func finalizeCI(_ *scan.Repo, f *Facts) {
	var cands []ciCandidate
	for _, step := range f.CI {
		cands = append(cands, ciCandidates(step)...)
	}
	existing := len(f.Commands) // detector-made commands; CI-only additions come after
	var additions []pendingCI
	for _, c := range cands {
		matchedExact := false
		for i := 0; i < existing; i++ {
			cmd := &f.Commands[i]
			if cmd.Dir != c.dir || strings.ContainsAny(cmd.Run, "<>") {
				continue
			}
			ct := finCanon(shellFields(cmd.Run))
			if !hasTokenPrefix(c.canon, ct) {
				continue
			}
			if len(ct) == len(c.canon) || hasEvidence(cmd, model.EvidenceDeclared) {
				cmd.Evidence = mergeEvidence(cmd.Evidence, []string{model.EvidenceCI})
				matchedExact = matchedExact || len(ct) == len(c.canon)
			}
			// Otherwise: a convention command that CI runs with extra flags.
			// CI's exact string is the more useful fact, so it is added below.
		}
		if matchedExact {
			continue
		}
		if kind := ciKind(c.canon); kind != "" && !ciUnsafe(c.display) {
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

func hasEvidence(c *model.Command, ev string) bool {
	for _, e := range c.Evidence {
		if e == ev {
			return true
		}
	}
	return false
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

var uvSwitches = setOf("--locked", "--frozen", "--no-sync", "--isolated", "--no-dev", "--all-extras", "--all-groups")

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
		for len(rest) > 0 && uvSwitches[rest[0]] {
			rest = rest[1:]
		}
		if len(rest) > 0 && !strings.HasPrefix(rest[0], "-") {
			toks = rest
		}
	}
	if len(toks) >= 3 && (toks[0] == "python" || toks[0] == "python3") && toks[1] == "-m" {
		toks = toks[2:]
	}
	if len(toks) == 0 {
		return toks
	}
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

// ---- recognising well-known CI commands ----

var ciRules = []struct {
	kind string
	re   *regexp.Regexp
}{
	{model.KindInstall, regexp.MustCompile(`^(?:npm ci|pnpm install|yarn install|bun install|uv sync|poetry install|bundle install|go mod download)(?: |$)`)},
	{model.KindTest, regexp.MustCompile(`^(?:go test|cargo test|cargo nextest run|pytest|npm test|pnpm test|yarn test|bun test|make test|just test|dotnet test|bundle exec rspec|mix test)(?: |$)`)},
	{model.KindTest, regexp.MustCompile(`^\./gradlew (?:\S+ )*?(?:test|check)(?: |$)`)},
	{model.KindTest, regexp.MustCompile(`^mvnw? (?:\S+ )*?(?:test|verify)(?: |$)`)},
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
