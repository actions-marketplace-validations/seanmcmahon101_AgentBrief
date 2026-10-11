package instructions

import (
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
)

var osStat = os.Stat

func (x cmdCtx) reportNamed(sev, msg, sug string) {
	x.c.add(model.Finding{Rule: RuleUnknownScript, Severity: sev, Path: x.file.Path, Line: x.line, Message: msg, Suggestion: sug})
}

var simpleNameRe = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.\-]*$`)

func okName(n string) bool { return simpleNameRe.MatchString(n) && !isPlaceholder(n) }

// inventoryCommand dispatches the inventory-backed name checks.
func (x cmdCtx) inventoryCommand(toks []string) {
	toks = stripRunner(toks)
	if len(toks) == 0 {
		return
	}
	switch toks[0] {
	case "cargo":
		x.cargoCommand(toks[1:])
	case "poe", "poethepoet":
		x.poeCommand(toks[1:])
	case "tox":
		x.toxCommand(toks[1:])
	case "nox":
		x.noxCommand(toks[1:])
	case "docker", "docker-compose":
		x.composeCommand(toks)
	}
}

// ---- cargo -----------------------------------------------------------------

var cargoNameRe = regexp.MustCompile(`name\s*=\s*["']([^"']+)["']`)

// cargoManifestNames returns every `name = "..."` in all Cargo.toml files.
func (c *checker) cargoManifestText() string {
	if c.cargoText != nil {
		return *c.cargoText
	}
	c.idx.build()
	var sb strings.Builder
	files := c.idx.byBase["Cargo.toml"]
	sort.Strings(files)
	for i, f := range files {
		if i >= 300 {
			break
		}
		sb.WriteString(c.readCached(f))
		sb.WriteByte('\n')
	}
	if c.exists("Cargo.lock") {
		sb.WriteString(c.readCached("Cargo.lock"))
	}
	s := sb.String()
	c.cargoText = &s
	return s
}

func (x cmdCtx) cargoCommand(args []string) {
	if len(args) > 0 && strings.HasPrefix(args[0], "+") {
		args = args[1:]
	}
	if len(args) == 0 {
		return
	}
	sub := args[0]
	switch sub {
	case "test", "build", "run", "clippy", "check", "doc", "bench":
	default:
		return
	}
	var rust []model.Project
	for _, p := range x.c.repo.Projects {
		if p.Ecosystem == "rust" {
			rust = append(rust, p)
		}
	}
	if len(rust) == 0 {
		return
	}
	sev := model.SeverityError
	if x.c.repo.Stats.Truncated {
		sev = model.SeverityWarning
	}
	rest := args[1:]
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if a == "--" {
			return
		}
		var val string
		switch {
		case (a == "-p" || a == "--package") && i+1 < len(rest):
			val = rest[i+1]
			i++
		case strings.HasPrefix(a, "--package="):
			val = a[len("--package="):]
		case strings.HasPrefix(a, "-p=") && len(a) > 3:
			val = a[3:]
		case a == "--test" && i+1 < len(rest) && sub == "test":
			x.cargoTest(rest[i+1], rust, sev)
			i++
			continue
		case strings.HasPrefix(a, "--test=") && sub == "test":
			x.cargoTest(a[len("--test="):], rust, sev)
			continue
		default:
			continue
		}
		name, _, _ := strings.Cut(val, "@")
		if !okName(name) {
			continue
		}
		found := false
		for _, p := range rust {
			if p.Name == name {
				found = true
			}
		}
		if !found {
			for _, m := range cargoNameRe.FindAllStringSubmatch(x.c.cargoManifestText(), -1) {
				if m[1] == name {
					found = true
					break
				}
			}
		}
		if found {
			continue
		}
		var names []string
		for _, p := range rust {
			names = append(names, p.Name)
		}
		sug := ""
		if s := closest(name, names); s != "" {
			sug = fmt.Sprintf("did you mean `-p %s`?", s)
		}
		x.reportNamed(sev, fmt.Sprintf("`%s` refers to crate `%s`, which is not a package in this Cargo workspace", x.text, name), sug)
	}
}

func (x cmdCtx) cargoTest(name string, rust []model.Project, sev string) {
	if !okName(name) {
		return
	}
	for _, p := range rust {
		for _, f := range []string{"tests/" + name + ".rs", "tests/" + name + "/main.rs"} {
			if x.c.exists(path.Join(p.Dir, f)) {
				return
			}
		}
	}
	for _, m := range cargoNameRe.FindAllStringSubmatch(x.c.cargoManifestText(), -1) {
		if m[1] == name {
			return
		}
	}
	x.reportNamed(sev, fmt.Sprintf("`%s` runs integration test `%s`, but no tests/%s.rs or [[test]] target exists", x.text, name, name), "")
}

// ---- poe -------------------------------------------------------------------

func (x cmdCtx) poeCommand(args []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-C" || a == "--directory" || a == "--root" || a == "-e" || a == "--executor" || a == "--ansi":
			i++
		case strings.HasPrefix(a, "-"):
			if a == "-h" || a == "--help" || a == "--version" {
				return
			}
		default:
			if !okName(a) || !x.c.tix.any("poe") {
				return
			}
			if x.c.tix.has("poe", a, "") {
				return
			}
			sev := model.SeverityError
			if x.c.poeMayInclude() {
				sev = model.SeverityWarning
			}
			sug := ""
			if s := closest(a, x.c.tix.names("poe", "")); s != "" {
				sug = fmt.Sprintf("did you mean `poe %s`?", s)
			}
			x.reportNamed(sev, fmt.Sprintf("`%s` runs poe task `%s`, which is not defined in pyproject.toml", x.text, a), sug)
			return
		}
	}
}

// poeMayInclude reports whether poe tasks might come from files the detector
// does not read (include = ..., poe_tasks.*).
func (c *checker) poeMayInclude() bool {
	c.idx.build()
	for _, b := range []string{"poe_tasks.toml", "poe_tasks.yaml", "poe_tasks.yml", "poe_tasks.json"} {
		if len(c.idx.byBase[b]) > 0 {
			return true
		}
	}
	for _, f := range c.idx.byBase["pyproject.toml"] {
		t := c.readCached(f)
		if strings.Contains(t, "tool.poe") && regexp.MustCompile(`(?m)^\s*include\s*=`).MatchString(t) {
			return true
		}
	}
	return false
}

// ---- tox -------------------------------------------------------------------

var toxDefaultEnv = regexp.MustCompile(`^(py(2[0-9]|3[0-9]{0,2})?|pypy[0-9]*|jython|cov|coverage|\.pkg|\.tox|ALL|\.)$`)

// toxGenerative reports whether the tox config may define env names the
// detector cannot see (generator syntax, tox.toml, setup.cfg).
func (c *checker) toxGenerative() bool {
	for _, t := range c.repo.Tasks {
		if t.Runner == "tox" && strings.ContainsAny(t.Name, "{}") {
			return true
		}
	}
	c.idx.build()
	for _, b := range []string{"tox.toml", "tox.ini", "pyproject.toml", "setup.cfg"} {
		for _, f := range c.idx.byBase[b] {
			t := c.readCached(f)
			if b == "tox.toml" || (b == "setup.cfg" && strings.Contains(t, "[tox:tox]")) {
				return true
			}
			lines := strings.Split(t, "\n")
			for i := 0; i < len(lines); i++ {
				l := strings.TrimSpace(lines[i])
				if !strings.HasPrefix(l, "envlist") && !strings.HasPrefix(l, "env_list") {
					if strings.HasPrefix(l, "[testenv:") && strings.ContainsAny(l, "{}") {
						return true
					}
					continue
				}
				if strings.ContainsAny(l, "{}") {
					return true
				}
				for j := i + 1; j < len(lines) && (strings.HasPrefix(lines[j], " ") || strings.HasPrefix(lines[j], "\t")); j++ {
					if strings.ContainsAny(lines[j], "{}") {
						return true
					}
				}
			}
		}
	}
	return false
}

func (x cmdCtx) toxCommand(args []string) {
	if !x.c.tix.any("tox") || x.c.toxGenerative() {
		return
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		var val string
		switch {
		case (a == "-e" || a == "--env") && i+1 < len(args):
			val = args[i+1]
			i++
		case strings.HasPrefix(a, "--env="):
			val = a[len("--env="):]
		case strings.HasPrefix(a, "-e") && len(a) > 2 && !strings.HasPrefix(a, "--"):
			val = strings.TrimPrefix(a[2:], "=")
		case a == "--":
			return
		default:
			continue
		}
		for _, env := range strings.Split(val, ",") {
			if !okName(env) || toxDefaultEnv.MatchString(env) || x.c.tix.has("tox", env, "") {
				continue
			}
			if first, _, ok := strings.Cut(env, "-"); ok && toxDefaultEnv.MatchString(first) {
				continue // factor combination such as py311-django
			}
			sug := ""
			if s := closest(env, x.c.tix.names("tox", "")); s != "" {
				sug = fmt.Sprintf("did you mean `tox -e %s`?", s)
			}
			x.reportNamed(model.SeverityError, fmt.Sprintf("`%s` runs tox environment `%s`, which is not defined in the tox configuration", x.text, env), sug)
		}
	}
}

// ---- nox -------------------------------------------------------------------

var noxParamSuffix = regexp.MustCompile(`-[0-9][0-9.]*$`)

func (x cmdCtx) noxCommand(args []string) {
	if !x.c.tix.any("nox") {
		return
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		var vals []string
		switch {
		case a == "-s" || a == "--session" || a == "--sessions":
			for i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				vals = append(vals, args[i+1])
				i++
			}
		case strings.HasPrefix(a, "--session="), strings.HasPrefix(a, "--sessions="):
			_, v, _ := strings.Cut(a, "=")
			vals = strings.Split(v, ",")
		case a == "--":
			return
		default:
			continue
		}
		for _, v := range vals {
			v = noxParamSuffix.ReplaceAllString(v, "")
			if !okName(v) || x.c.tix.has("nox", v, "") {
				continue
			}
			sug := ""
			if s := closest(v, x.c.tix.names("nox", "")); s != "" {
				sug = fmt.Sprintf("did you mean `nox -s %s`?", s)
			}
			// sessions can be registered programmatically, so this is a warning.
			x.reportNamed(model.SeverityWarning, fmt.Sprintf("`%s` runs nox session `%s`, which is not defined in noxfile.py", x.text, v), sug)
		}
	}
}

// ---- docker compose --------------------------------------------------------

var composeSpec = argSpec{
	valued: set(`-e --env -v --volume -u --user -w --workdir --name --entrypoint -p --publish -l --label --scale -t --timeout --exit-code-from --attach --no-attach --pull --wait-timeout --index --cap-add --cap-drop --env-from-file --since --until -n --tail --progress --format`),
	bools:  set(`-d --detach --build --rm -T --no-deps --force-recreate --no-recreate --remove-orphans --abort-on-container-exit --wait -V --renew-anon-volumes --no-build --no-start --quiet-pull --no-color --no-log-prefix -f --follow --timestamps --service-ports -P --no-TTY --privileged -i --interactive --always-recreate-deps --no-attach-deps --attach-dependencies --quiet -q --dry-run --volumes --no-cache --pull-always -a --all`),
	strict: true,
}

var composeSubs = set(`up run exec build logs start stop restart pull rm kill pause unpause port top`)

func (x cmdCtx) composeCommand(toks []string) {
	args := toks[1:]
	if toks[0] == "docker" {
		if len(args) == 0 || args[0] != "compose" {
			return
		}
		args = args[1:]
	}
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		switch a := args[0]; {
		case a == "--profile" || a == "--env-file" || a == "-p" || a == "--project-name" || a == "--ansi" || a == "--parallel":
			if len(args) < 2 {
				return
			}
			args = args[2:]
		case a == "--compatibility" || a == "--dry-run" || a == "--verbose":
			args = args[1:]
		default:
			return // -f / --file / --project-directory / unknown: stay quiet
		}
	}
	if len(args) == 0 || !composeSubs[args[0]] {
		return
	}
	sub := args[0]
	spec := composeSpec
	if sub == "run" || sub == "exec" {
		spec.maxPos = 1
	}
	pos := scanArgs(args[1:], spec)
	if len(pos) == 0 {
		return
	}
	services, ok := x.composeServices()
	if !ok {
		return
	}
	for _, s := range pos {
		if !okName(s) || services[s] {
			continue
		}
		var names []string
		for n := range services {
			names = append(names, n)
		}
		sort.Strings(names)
		sug := ""
		if c := closest(s, names); c != "" {
			sug = fmt.Sprintf("did you mean `%s`?", c)
		}
		x.reportNamed(model.SeverityError, fmt.Sprintf("`%s` refers to compose service `%s`, which is not defined in the compose file", x.text, s), sug)
	}
}

// composeServices unions the service names of the compose files in the
// candidate directories. ok=false when nothing can be judged.
func (x cmdCtx) composeServices() (map[string]bool, bool) {
	bases := x.pathBases()
	if len(bases) == 0 {
		return nil, false
	}
	var files []string
	for _, b := range bases {
		if strings.Contains(x.c.readCached(path.Join(b, ".env")), "COMPOSE_FILE") {
			return nil, false
		}
		ents, err := os.ReadDir(x.c.abs(b))
		if err != nil {
			continue
		}
		for _, e := range ents {
			n := e.Name()
			if e.IsDir() {
				continue
			}
			if (strings.HasPrefix(n, "compose.") || strings.HasPrefix(n, "docker-compose.")) && (strings.HasSuffix(n, ".yml") || strings.HasSuffix(n, ".yaml")) {
				files = append(files, path.Join(b, n))
			}
		}
	}
	if len(files) == 0 {
		return nil, false
	}
	out := map[string]bool{}
	for _, f := range files {
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(x.c.readCached(f)), &doc); err != nil || doc == nil {
			return nil, false
		}
		if _, inc := doc["include"]; inc {
			return nil, false
		}
		svcs, _ := doc["services"].(map[string]any)
		for n, v := range svcs {
			out[n] = true
			if m, ok := v.(map[string]any); ok {
				if _, ext := m["extends"]; ext {
					return nil, false
				}
			}
		}
	}
	return out, true
}
