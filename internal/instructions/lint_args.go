package instructions

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
)

// ---- argument scanning -----------------------------------------------------

// argSpec describes how to walk the arguments of a command.
type argSpec struct {
	valued   map[string]bool // flags that consume the next token
	bools    map[string]bool // flags known to take no value
	boolChar string          // short flags made only of these letters are boolean (-vvx)
	stop     map[string]bool // tokens that end scanning (-c, --, -args)
	maxPos   int             // stop after this many positionals (0 = unlimited)
	// strict: an unknown flag ends scanning (compose, bash) instead of only
	// hiding the positional that follows it.
	strict bool
}

// scanArgs returns the positional arguments of args. A positional directly
// after an unknown flag (which may be its value) is dropped, never guessed.
func scanArgs(args []string, s argSpec) []string {
	var pos []string
	ambiguous := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if s.stop[a] {
			break
		}
		if strings.HasPrefix(a, "-") && a != "-" {
			switch {
			case strings.Contains(a, "="):
				ambiguous = false
			case s.valued[a]:
				i++
				ambiguous = false
			case s.bools[a]:
				ambiguous = false
			case s.boolChar != "" && regexp.MustCompile(`^-[`+s.boolChar+`]+$`).MatchString(a):
				ambiguous = false
			default:
				if s.strict {
					return pos
				}
				ambiguous = true
			}
			continue
		}
		if ambiguous {
			ambiguous = false
			continue
		}
		pos = append(pos, a)
		if s.maxPos > 0 && len(pos) >= s.maxPos {
			break
		}
	}
	return pos
}

// ---- cwd / base dirs --------------------------------------------------------

func (c *checker) isDir(rel string) bool {
	st, err := osStat(c.abs(rel))
	return err == nil && st.IsDir()
}

// findDir resolves rel against the file's base dirs and returns the first
// existing directory (repo-relative).
func (x cmdCtx) findDir(rel string) (string, bool) {
	for _, d := range x.c.baseDirs(x.file, false) {
		p := path.Clean(path.Join(d, rel))
		if strings.HasPrefix(p, "..") {
			continue
		}
		if x.c.isDir(p) {
			return p, true
		}
	}
	return "", false
}

// pathBases returns the directories relative paths in the command resolve
// against, honouring a preceding `cd`. nil means "unknown, do not judge".
func (x cmdCtx) pathBases() []string {
	switch x.cwd {
	case "":
		return x.c.baseDirs(x.file, false)
	case "?":
		return nil
	}
	var out []string
	for _, d := range x.c.baseDirs(x.file, false) {
		p := path.Clean(path.Join(d, x.cwd))
		if !strings.HasPrefix(p, "..") && x.c.isDir(p) {
			out = append(out, p)
		}
	}
	return out
}

// nextCwd applies a `cd` segment to the working directory state.
func nextCwd(cwd string, toks []string) string {
	if len(toks) != 2 {
		return "?"
	}
	a := toks[1]
	if a == "--" || a == "-" || strings.ContainsAny(a, "$`<>(){}*~") || strings.HasPrefix(a, "/") {
		return "?"
	}
	if cwd == "?" {
		return "?"
	}
	base := cwd
	if base == "" {
		base = "."
	}
	p := path.Clean(path.Join(base, a))
	if p == ".." || strings.HasPrefix(p, "../") {
		return "?"
	}
	if p == "." {
		return ""
	}
	return p
}

// ---- path arguments ---------------------------------------------------------

var placeholderTokens = set(`foo bar baz xxx yyy zzz xyz some your my name file filename path dir folder mymodule module pkg package test_name`)

// pathPlaceholder reports whether p looks like an illustrative path.
func pathPlaceholder(p string) bool {
	if isPlaceholder(p) || strings.Contains(p, "path/to") || strings.Contains(p, "...") || strings.Contains(p, "…") {
		return true
	}
	for _, seg := range strings.Split(strings.ToLower(p), "/") {
		if strings.HasPrefix(seg, "my_") || strings.HasPrefix(seg, "my-") || strings.HasPrefix(seg, "example") || strings.HasPrefix(seg, "your") || strings.HasPrefix(seg, "some") {
			return true
		}
		if placeholderSegs[seg] {
			return true
		}
		stem := strings.TrimSuffix(seg, path.Ext(seg))
		for _, tk := range regexp.MustCompile(`[_\-.]`).Split(stem, -1) {
			if placeholderTokens[tk] || len(tk) == 1 && stem == tk {
				return true
			}
		}
	}
	return false
}

func (x cmdCtx) reportPath(sev, msg string) {
	x.c.add(model.Finding{Rule: RuleMissingPath, Severity: sev, Path: x.file.Path, Line: x.line, Message: msg})
}

var buildOutDirs = set(`bin obj out release debug .build _build`)

type pathOpts struct {
	pytest bool // strip ::node-ids
	exec   bool // ./thing executable
	pkg    bool // go package pattern
}

// checkPathArg verifies that a path argument of a recognised command exists.
func (x cmdCtx) checkPathArg(raw string, o pathOpts) {
	p := raw
	if o.pytest {
		if i := strings.Index(p, "::"); i >= 0 {
			p = p[:i]
		}
	}
	if o.pkg {
		for strings.HasSuffix(p, "/...") {
			p = strings.TrimSuffix(p, "/...")
		}
		if p == "." || p == "./" || p == "" || p == "..." {
			return
		}
	}
	explicit := strings.HasPrefix(p, "./")
	p = strings.TrimSuffix(p, "/")
	if p == "" || p == "." || strings.HasPrefix(p, "/") || strings.HasPrefix(p, "~") || strings.HasPrefix(p, "-") ||
		strings.HasPrefix(p, "../") || p == ".." || strings.Contains(p, "..") && !strings.Contains(p, "/") {
		return
	}
	if !pathCharsRe.MatchString(p) || strings.Contains(p, ":") || pathPlaceholder(p) {
		return
	}
	clean := path.Clean(p)
	if strings.HasPrefix(clean, "..") || isGeneratedPath(clean) || isGeneratedPath(clean+"/") {
		return
	}
	bases := x.pathBases()
	if len(bases) == 0 {
		return
	}
	for _, b := range bases {
		if x.c.exists(path.Join(b, clean)) {
			return
		}
	}
	ext := strings.ToLower(path.Ext(clean))
	first := strings.SplitN(clean, "/", 2)[0]
	if o.exec {
		// `./main`, `./bin/app` are usually build outputs; only judge scripts
		// and files inside real directories.
		if buildOutDirs[first] {
			return
		}
		if !knownExts[ext] && !strings.Contains(clean, "/") {
			return
		}
	}
	sev := model.SeverityWarning
	parentExists := false
	if d := path.Dir(clean); d != "." {
		for _, b := range bases {
			if x.c.isDir(path.Join(b, d)) {
				parentExists = true
			}
		}
	}
	if (explicit || parentExists || (strings.Contains(clean, "/") && x.c.idx.topLevel(first))) && !(!explicit && x.c.idx.suffixMatch(clean)) {
		sev = model.SeverityError
	}
	if x.c.ignored(clean) {
		return
	}
	x.reportPath(sev, fmt.Sprintf("`%s` refers to `%s`, which does not exist in the repository", x.text, raw))
}

// ---- recognised commands ----------------------------------------------------

var (
	pythonCmdRe = regexp.MustCompile(`^(python|python3|py|python3\.\d+)$`)
	pytestSpec  = argSpec{
		valued:   set(`-k -m -c -p -o -W --rootdir --confcutdir --maxfail -n --cov --cov-report --cov-config --junitxml --junit-xml --tb --durations --log-level --log-cli-level --basetemp --import-mode --color --capture --deselect --ignore --ignore-glob --override-ini --numprocesses --dist --timeout --reruns --randomly-seed -r --html --config-file --cache-clear-dir --pyargs-dir`),
		bools:    set(`--lf --ff --nf --sw --co --collect-only --no-header --strict-markers --pdb --trace --disable-warnings --verbose --quiet --exitfirst --showlocals --no-cov --cov-append --failed-first --last-failed --new-first --stepwise --doctest-modules --setup-show -ra -rA -rs -rx -rf -rE`),
		boolChar: "vqxsl", stop: set(`--pyargs`),
	}
	bashSpec = argSpec{valued: set(`-o +o -O`), boolChar: "euxvlisH", stop: set(`-c -s -- --rcfile --init-file`), strict: true, maxPos: 1}
	goSpec   = argSpec{
		valued: set(`-run -bench -count -timeout -tags -o -ldflags -gcflags -asmflags -p -parallel -cpu -coverprofile -covermode -coverpkg -mod -modfile -exec -vet -skip -shuffle -fuzz -fuzztime -C -overlay -pkgdir -toolexec -benchtime -blockprofile -cpuprofile -memprofile -mutexprofile -trace -outputdir -tags`),
		bools:  set(`-v -race -cover -json -short -failfast -x -n -a -trimpath -work -msan -asan -i -benchmem -buildvcs -linkshared -fuzzminimizetime -list`),
		stop:   set(`-args --`),
	}
)

func isRunnerPrefix(toks []string) (n int) {
	if len(toks) >= 2 {
		switch {
		case (toks[0] == "uv" || toks[0] == "poetry" || toks[0] == "pdm" || toks[0] == "pipenv") && toks[1] == "run":
			return 2
		case (toks[0] == "pnpm" || toks[0] == "yarn") && toks[1] == "exec":
			return 2
		}
	}
	if len(toks) >= 1 && toks[0] == "npx" {
		return 1
	}
	return 0
}

var runnerValueFlags = set(`--with --python -p --extra --group --project --directory --env-file --with-requirements --package -c --cwd`)

func stripRunner(toks []string) []string {
	n := isRunnerPrefix(toks)
	if n == 0 {
		return toks
	}
	toks = toks[n:]
	for len(toks) > 0 && strings.HasPrefix(toks[0], "-") {
		if runnerValueFlags[toks[0]] && !strings.Contains(toks[0], "=") {
			if len(toks) < 2 {
				return nil
			}
			toks = toks[2:]
		} else {
			toks = toks[1:]
		}
	}
	return toks
}

// pathCommand checks path arguments of pytest/python/bash/node/go/./exe forms.
func (x cmdCtx) pathCommand(toks []string) {
	viaRunner := isRunnerPrefix(toks) > 0
	toks = stripRunner(toks)
	if len(toks) == 0 {
		return
	}
	cmd, args := toks[0], toks[1:]
	switch {
	case cmd == "pytest" || cmd == "py.test":
		x.pytestArgs(args)
	case pythonCmdRe.MatchString(cmd):
		// leading flags
		for len(args) > 0 && strings.HasPrefix(args[0], "-") {
			switch a := args[0]; {
			case a == "-m" && len(args) > 1:
				if args[1] == "pytest" {
					x.pytestArgs(args[2:])
				}
				return
			case a == "-c" || a == "-" || a == "--":
				return
			case a == "-W" || a == "-X" || a == "-Q":
				if len(args) < 2 {
					return
				}
				args = args[2:]
			case regexp.MustCompile(`^-[uBOEsSvqIbd]+$`).MatchString(a):
				args = args[1:]
			default:
				return
			}
		}
		if len(args) > 0 && strings.HasSuffix(args[0], ".py") {
			x.checkPathArg(args[0], pathOpts{})
		}
	case viaRunner && strings.HasSuffix(cmd, ".py"):
		x.checkPathArg(cmd, pathOpts{})
	case cmd == "bash" || cmd == "sh" || cmd == "zsh":
		if pos := scanArgs(args, bashSpec); len(pos) > 0 && (strings.Contains(pos[0], "/") || scriptExt(pos[0])) {
			x.checkPathArg(pos[0], pathOpts{})
		}
	case cmd == "node" || cmd == "nodejs" || cmd == "tsx" || cmd == "ts-node" || cmd == "ts-node-esm":
		spec := argSpec{valued: set(`-r --require --import --loader --experimental-loader --env-file --conditions -C --tsconfig -P --project`), stop: set(`-e -p --eval --print -- -`), strict: true, maxPos: 1}
		if pos := scanArgs(args, spec); len(pos) > 0 && (scriptExt(pos[0]) || strings.Contains(pos[0], "/")) {
			x.checkPathArg(pos[0], pathOpts{})
		}
	case cmd == "go":
		x.goArgs(args)
	case strings.HasPrefix(cmd, "./") && scriptExt(cmd):
		// Extension-less ./x is usually a build output (./main, ./bin/tool)
		// that legitimately does not exist until built; only scripts are judged.
		x.checkPathArg(cmd, pathOpts{exec: true})
	}
}

func scriptExt(s string) bool {
	switch strings.ToLower(path.Ext(s)) {
	case ".sh", ".bash", ".zsh", ".py", ".js", ".mjs", ".cjs", ".ts", ".mts", ".cts", ".jsx", ".tsx":
		return true
	}
	return false
}

func (x cmdCtx) pytestArgs(args []string) {
	for _, a := range args {
		if a == "--pyargs" {
			return
		}
	}
	for _, p := range scanArgs(args, pytestSpec) {
		if strings.Contains(p, "/") || strings.Contains(strings.SplitN(p, "::", 2)[0], ".py") {
			x.checkPathArg(p, pathOpts{pytest: true})
		}
	}
}

func (x cmdCtx) goArgs(args []string) {
	if len(args) == 0 {
		return
	}
	sub := args[0]
	if sub != "test" && sub != "build" && sub != "run" && sub != "vet" {
		return
	}
	spec := goSpec
	if sub == "run" {
		spec.maxPos = 1
	}
	for _, p := range scanArgs(args[1:], spec) {
		if strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") {
			if strings.Contains(strings.TrimSuffix(p, "/..."), "...") {
				continue
			}
			x.checkPathArg(p, pathOpts{pkg: true})
		}
	}
}
