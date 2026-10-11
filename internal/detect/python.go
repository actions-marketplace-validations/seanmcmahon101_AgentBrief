package detect

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/redact"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { register("python", detectPython) }

const maxPyStandalones = 8

// pyDir is everything we know about one Python project directory.
type pyDir struct {
	dir      string
	toml     map[string]any // parsed pyproject.toml (nil if absent/invalid)
	raw      string         // pyproject.toml text
	tool     string         // uv, poetry, pdm, hatch, pipenv, pip
	toolSrc  model.Source
	manifest string
}

func (d *pyDir) file(name string) string { return coreJoin(d.dir, name) }

// tget walks nested TOML tables: tget(m, "tool", "uv", "workspace").
func tget(m map[string]any, keys ...string) any {
	var cur any = m
	for _, k := range keys {
		t, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = t[k]
	}
	return cur
}

func tstr(m map[string]any, keys ...string) string {
	s, _ := tget(m, keys...).(string)
	return s
}

func tstrings(v any) []string {
	arr, _ := v.([]any)
	var out []string
	for _, e := range arr {
		if s, ok := e.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// tomlLine returns the line of the first "[header" table start in raw, or 0.
func tomlLine(raw, header string) int {
	for i, line := range strings.Split(raw, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "["+header+"]") || strings.HasPrefix(t, "["+header+".") {
			return i + 1
		}
	}
	return 0
}

func loadPyDir(r *scan.Repo, dir string) *pyDir {
	d := &pyDir{dir: dir}
	if r.Has(d.file("pyproject.toml")) {
		d.raw = r.ReadString(d.file("pyproject.toml"))
		var m map[string]any
		if _, err := toml.Decode(d.raw, &m); err == nil {
			d.toml = m
			d.manifest = d.file("pyproject.toml")
		}
	}
	return d
}

var requirementsRe = regexp.MustCompile(`^requirements[^/]*\.txt$`)

// pyRequirements lists requirements*.txt files directly in dir.
func pyRequirements(r *scan.Repo, dir string) []string {
	var out []string
	for _, f := range r.Files() {
		if path.Dir(f) == dir && requirementsRe.MatchString(path.Base(f)) {
			out = append(out, f)
		}
	}
	return out
}

func hasPyFiles(r *scan.Repo) bool {
	for _, f := range r.Files() {
		if strings.HasSuffix(f, ".py") {
			return true
		}
	}
	return false
}

func detectPython(r *scan.Repo, f *Facts) {
	root := loadPyDir(r, ".")
	var dirs []*pyDir
	var members []*pyDir
	if isPyProject(r, root) {
		dirs = append(dirs, root)
		members = uvMembers(r, root)
	}
	memberSet := map[string]bool{}
	for _, m := range members {
		memberSet[m.dir] = true
	}
	dirs = append(dirs, pyStandalones(r, root, memberSet)...)
	if len(dirs) == 0 {
		return
	}
	for _, d := range dirs {
		d.tool, d.toolSrc = pyTool(r, d)
		isRoot := d.dir == "."
		if d == root {
			if name := pyProjectName(d); name != "" && (f.Name == "" || f.Name == path.Base(slash(r.Root()))) {
				f.Name = name
			}
		}
		f.AddPackageManager(model.PackageManager{Name: d.tool, Ecosystem: "python", Dir: d.dir, Source: d.toolSrc})
		f.AddProject(model.Project{Dir: d.dir, Name: firstNonEmpty(pyProjectName(d), path.Base(d.dir)), Ecosystem: "python",
			Manifest: pyManifest(r, d), IsWorkspaceRoot: isRoot && len(members) > 0})
		pyToolchains(r, f, d)
		pyInstall(r, f, d)
		pyTasks(r, f, d)
		pyCommands(r, f, d)
	}
	for _, m := range members {
		m.tool, m.toolSrc = "uv", root.toolSrc
		f.AddProject(model.Project{Dir: m.dir, Name: firstNonEmpty(pyProjectName(m), path.Base(m.dir)), Ecosystem: "python",
			Manifest: m.manifest, Workspace: "."})
		pyTasks(r, f, m)
		pyCommands(r, f, m)
	}
}

// isPyProject: a pyproject.toml, setup.py, Pipfile, or (at the root of a
// repo that really contains Python) a requirements file.
func isPyProject(r *scan.Repo, d *pyDir) bool {
	if d.toml != nil || r.Has(d.file("setup.py")) || r.Has(d.file("setup.cfg")) || r.Has(d.file("Pipfile")) {
		return true
	}
	return len(pyRequirements(r, d.dir)) > 0 && hasPyFiles(r)
}

func pyManifest(r *scan.Repo, d *pyDir) string {
	for _, n := range []string{"pyproject.toml", "setup.py", "setup.cfg", "Pipfile"} {
		if r.Has(d.file(n)) {
			return d.file(n)
		}
	}
	if reqs := pyRequirements(r, d.dir); len(reqs) > 0 {
		return reqs[0]
	}
	return ""
}

func pyProjectName(d *pyDir) string {
	return firstNonEmpty(tstr(d.toml, "project", "name"), tstr(d.toml, "tool", "poetry", "name"))
}

// pyTool picks the package manager: uv > poetry > pdm > hatch > pipenv > pip.
func pyTool(r *scan.Repo, d *pyDir) (string, model.Source) {
	src := func(p, header string) model.Source {
		return model.Source{Path: d.file(p), Line: tomlLine(d.raw, header)}
	}
	has := func(keys ...string) bool { return tget(d.toml, keys...) != nil }
	switch {
	case r.Has(d.file("uv.lock")):
		return "uv", model.Source{Path: d.file("uv.lock")}
	case has("tool", "uv"):
		return "uv", src("pyproject.toml", "tool.uv")
	case r.Has(d.file("poetry.lock")):
		return "poetry", model.Source{Path: d.file("poetry.lock")}
	case has("tool", "poetry"):
		return "poetry", src("pyproject.toml", "tool.poetry")
	case r.Has(d.file("pdm.lock")):
		return "pdm", model.Source{Path: d.file("pdm.lock")}
	case has("tool", "pdm"):
		return "pdm", src("pyproject.toml", "tool.pdm")
	case r.Has(d.file("hatch.toml")):
		return "hatch", model.Source{Path: d.file("hatch.toml")}
	case has("tool", "hatch", "envs"):
		return "hatch", src("pyproject.toml", "tool.hatch.envs")
	case r.Has(d.file("Pipfile")):
		return "pipenv", model.Source{Path: d.file("Pipfile")}
	}
	return "pip", model.Source{Path: pyManifest(r, d)}
}

func uvMembers(r *scan.Repo, root *pyDir) []*pyDir {
	ws, _ := tget(root.toml, "tool", "uv", "workspace").(map[string]any)
	if ws == nil {
		return nil
	}
	exclude := tstrings(ws["exclude"])
	seen := map[string]bool{}
	var dirs []string
	for _, pat := range tstrings(ws["members"]) {
		for _, d := range r.GlobDirs(pat) {
			if d == "." || seen[d] || !r.Has(coreJoin(d, "pyproject.toml")) || nodeExcluded(exclude, d) {
				continue
			}
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	sort.Strings(dirs)
	var out []*pyDir
	for _, d := range dirs {
		out = append(out, loadPyDir(r, d))
	}
	return out
}

// pyStandalones finds pyproject.toml files one or two levels down when the
// root is not itself a Python project or a uv workspace.
func pyStandalones(r *scan.Repo, root *pyDir, members map[string]bool) []*pyDir {
	if root.toml != nil && tget(root.toml, "tool", "uv", "workspace") != nil {
		return nil
	}
	var out []*pyDir
	for _, file := range r.Files() {
		if path.Base(file) != "pyproject.toml" {
			continue
		}
		dir := path.Dir(file)
		if dir == "." || strings.Count(dir, "/") >= 2 || members[dir] || isVendoredPath(file) {
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
		if d := loadPyDir(r, dir); d.toml != nil {
			out = append(out, d)
		}
		if len(out) >= maxPyStandalones {
			break
		}
	}
	return out
}

func pyToolchains(r *scan.Repo, f *Facts, d *pyDir) {
	if file := d.file(".python-version"); r.Has(file) {
		if v := firstLine(r.ReadString(file)); v != "" && len(v) <= 40 {
			f.AddToolchain(model.Toolchain{Name: "python", Version: v, Source: model.Source{Path: file, Line: 1}})
		}
	}
	if v := tstr(d.toml, "project", "requires-python"); v != "" {
		f.AddToolchain(model.Toolchain{Name: "python", Version: v, Source: model.Source{Path: d.manifest, Line: tomlLine(d.raw, "project")}})
	} else if v := tstr(d.toml, "tool", "poetry", "dependencies", "python"); v != "" {
		f.AddToolchain(model.Toolchain{Name: "python", Version: v, Source: model.Source{Path: d.manifest, Line: tomlLine(d.raw, "tool.poetry.dependencies")}})
	}
}

// prefix is what goes in front of a tool binary so it runs in the project env.
func (d *pyDir) prefix() string {
	switch d.tool {
	case "uv":
		return "uv run "
	case "poetry":
		return "poetry run "
	case "pdm":
		return "pdm run "
	case "hatch":
		return "hatch run "
	case "pipenv":
		return "pipenv run "
	}
	return ""
}

func pyInstall(r *scan.Repo, f *Facts, d *pyDir) {
	run, src := "", d.toolSrc
	switch d.tool {
	case "uv":
		run = "uv sync"
	case "poetry":
		run = "poetry install"
	case "pdm":
		run = "pdm install"
	case "pipenv":
		run = "pipenv install --dev"
	case "pip":
		reqs := pyRequirements(r, d.dir)
		packaged := tget(d.toml, "project") != nil || tget(d.toml, "build-system") != nil ||
			r.Has(d.file("setup.py")) || r.Has(d.file("setup.cfg"))
		switch {
		case packaged:
			run = "pip install -e ."
		case len(reqs) > 0:
			run, src = "pip install -r "+path.Base(reqs[0]), model.Source{Path: reqs[0]}
			if d.dir != "." {
				run = "pip install -r " + path.Base(reqs[0])
			}
		}
	}
	if run != "" {
		f.AddCommand(model.Command{Kind: model.KindInstall, Run: run, Dir: d.dir, Evidence: []string{model.EvidenceConvention}, Source: src})
	}
}

var pytestRe = regexp.MustCompile(`(?im)(^|[\s"',\[])pytest([\s"'>=<~!\[,;\]]|$)`)

// pytestSource returns the file proving pytest is used in d, or "".
func pytestSource(r *scan.Repo, d *pyDir) model.Source {
	switch {
	case tget(d.toml, "tool", "pytest") != nil:
		return model.Source{Path: d.manifest, Line: tomlLine(d.raw, "tool.pytest")}
	case r.Has(d.file("pytest.ini")):
		return model.Source{Path: d.file("pytest.ini")}
	}
	for _, n := range []string{"tox.ini", "setup.cfg"} {
		if t := r.ReadString(d.file(n)); strings.Contains(t, "[pytest]") || strings.Contains(t, "[tool:pytest]") {
			return model.Source{Path: d.file(n)}
		}
	}
	candidates := append([]string{d.file("pyproject.toml"), d.file("Pipfile"), d.file("setup.py"), d.file("setup.cfg"), d.file("noxfile.py")}, pyRequirements(r, d.dir)...)
	for _, c := range candidates {
		if r.Has(c) && pytestRe.MatchString(r.ReadString(c)) {
			return model.Source{Path: c}
		}
	}
	if r.Has(d.file("conftest.py")) && (r.IsDir(d.file("tests")) || r.IsDir(d.file("test"))) {
		return model.Source{Path: d.file("conftest.py")}
	}
	return model.Source{}
}

func pyCommands(r *scan.Repo, f *Facts, d *pyDir) {
	conv := []string{model.EvidenceConvention}
	p := d.prefix()
	if d.tool == "hatch" {
		p = "hatch run "
	}
	add := func(kind, run string, src model.Source, note string) {
		f.AddCommand(model.Command{Kind: kind, Run: run, Dir: d.dir, Evidence: conv, Source: src, Note: note})
	}

	if src := pytestSource(r, d); src.Path != "" {
		test, one := p+"pytest", p+"pytest <file>::<test_name>"
		if d.tool == "hatch" {
			test, one = "hatch test", "hatch test <file>::<test_name>"
		} else if d.tool == "pip" {
			test, one = "python -m pytest", "python -m pytest <file>::<test_name>"
		}
		add(model.KindTest, test, src, "")
		add(model.KindTestOne, one, src, "pytest; replace <file> and <test_name>")
	}
	if r.Has(d.file("manage.py")) {
		py := "python"
		if p != "" && d.tool != "hatch" {
			py = p + "python"
		}
		add(model.KindTestOne, py+" manage.py test <app.tests.Class.test>", model.Source{Path: d.file("manage.py")}, "Django test runner")
	}
	if src := pyConfigSource(r, d, "ruff", []string{"ruff.toml", ".ruff.toml"}); src.Path != "" {
		add(model.KindLint, p+"ruff check .", src, "")
		add(model.KindFormat, p+"ruff format .", src, "")
	}
	if src := pyConfigSource(r, d, "mypy", []string{"mypy.ini", ".mypy.ini"}); src.Path != "" {
		add(model.KindTypecheck, p+"mypy .", src, "")
	} else if src := pyConfigSource(r, d, "pyright", []string{"pyrightconfig.json"}); src.Path != "" {
		add(model.KindTypecheck, p+"pyright", src, "")
	}
}

// pyConfigSource finds a tool's config: [tool.<name>] in pyproject.toml or one
// of the stand-alone config files.
func pyConfigSource(r *scan.Repo, d *pyDir, name string, files []string) model.Source {
	if tget(d.toml, "tool", name) != nil {
		return model.Source{Path: d.manifest, Line: tomlLine(d.raw, "tool."+name)}
	}
	for _, n := range files {
		if r.Has(d.file(n)) {
			return model.Source{Path: d.file(n)}
		}
	}
	if name == "mypy" && strings.Contains(r.ReadString(d.file("setup.cfg")), "[mypy]") {
		return model.Source{Path: d.file("setup.cfg")}
	}
	return model.Source{}
}

// ---- task inventories: poe, pdm, hatch, tox, nox ----

func pyTasks(r *scan.Repo, f *Facts, d *pyDir) {
	poeTasks(f, d)
	pdmScripts(f, d)
	hatchScripts(r, f, d)
	toxEnvs(r, f, d)
	noxSessions(r, f, d)
}

func addPyTask(f *Facts, d *pyDir, runner, name, body string, src model.Source, runCmd string) {
	t := model.Task{Runner: runner, Name: name, Dir: d.dir, Source: src}
	if len(body) <= maxBodyLen {
		t.Body = redact.String(body)
	}
	f.AddTask(t)
	if kind := KindForTaskName(name); kind != "" && runCmd != "" && kind != model.KindInstall && kind != model.KindRun {
		f.AddCommand(model.Command{Kind: kind, Run: runCmd, Dir: d.dir, Evidence: []string{model.EvidenceDeclared}, Source: src})
	}
}

func pyKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// taskBody extracts a short command from a poe/pdm/hatch task value.
func taskBody(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []any:
		if s := tstrings(t); len(s) > 0 {
			return strings.Join(s, " && ")
		}
	case map[string]any:
		for _, k := range []string{"cmd", "shell", "call", "script", "composite"} {
			if s, ok := t[k].(string); ok {
				return s
			}
		}
	}
	return ""
}

func poeTasks(f *Facts, d *pyDir) {
	tasks, _ := tget(d.toml, "tool", "poe", "tasks").(map[string]any)
	for _, name := range pyKeys(tasks) {
		run := "poe " + name
		if p := d.prefix(); p != "" && d.tool != "hatch" {
			run = p + run
		}
		addPyTask(f, d, "poe", name, taskBody(tasks[name]), model.Source{Path: d.manifest, Line: tomlLine(d.raw, "tool.poe.tasks")}, run)
	}
}

func pdmScripts(f *Facts, d *pyDir) {
	scripts, _ := tget(d.toml, "tool", "pdm", "scripts").(map[string]any)
	for _, name := range pyKeys(scripts) {
		if name == "_" {
			continue // shared options, not a script
		}
		addPyTask(f, d, "pdm", name, taskBody(scripts[name]), model.Source{Path: d.manifest, Line: tomlLine(d.raw, "tool.pdm.scripts")}, "pdm run "+name)
	}
}

func hatchScripts(r *scan.Repo, f *Facts, d *pyDir) {
	envs, _ := tget(d.toml, "tool", "hatch", "envs").(map[string]any)
	file, raw, header := d.manifest, d.raw, "tool.hatch.envs"
	if r.Has(d.file("hatch.toml")) {
		var m map[string]any
		file, raw = d.file("hatch.toml"), r.ReadString(d.file("hatch.toml"))
		if _, err := toml.Decode(raw, &m); err == nil {
			envs, _ = m["envs"].(map[string]any)
		}
		header = "envs"
	}
	for _, env := range pyKeys(envs) {
		scripts, _ := envs[env].(map[string]any)["scripts"].(map[string]any)
		for _, name := range pyKeys(scripts) {
			full, run := name, "hatch run "+name
			if env != "default" {
				full = env + ":" + name
				run = "hatch run " + full
			}
			addPyTask(f, d, "hatch", full, taskBody(scripts[name]), model.Source{Path: file, Line: tomlLine(raw, header+"."+env)}, run)
		}
	}
}

// ---- tox ----

type iniSection struct {
	line int
	keys map[string]string
}

// parseINI is a minimal tox.ini reader: sections, key = value, indented
// continuation lines. It exists to avoid a dependency for one file format.
func parseINI(text string) (order []string, secs map[string]*iniSection) {
	secs = map[string]*iniSection{}
	var cur *iniSection
	lastKey := ""
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		t := strings.TrimSpace(line)
		switch {
		case t == "" || strings.HasPrefix(t, "#") || strings.HasPrefix(t, ";"):
		case strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]"):
			name := t[1 : len(t)-1]
			cur = &iniSection{line: i + 1, keys: map[string]string{}}
			if _, dup := secs[name]; !dup {
				order = append(order, name)
			}
			secs[name] = cur
			lastKey = ""
		case cur != nil && (line[0] == ' ' || line[0] == '\t') && lastKey != "":
			cur.keys[lastKey] += "\n" + t
		case cur != nil:
			if k, v, ok := strings.Cut(t, "="); ok {
				lastKey = strings.TrimSpace(k)
				cur.keys[lastKey] = strings.TrimSpace(v)
			}
		}
	}
	return order, secs
}

func toxEnvs(r *scan.Repo, f *Facts, d *pyDir) {
	var text, file string
	line0 := 0
	switch {
	case r.Has(d.file("tox.ini")):
		text, file = r.ReadString(d.file("tox.ini")), d.file("tox.ini")
	case tstr(d.toml, "tool", "tox", "legacy_tox_ini") != "":
		text, file, line0 = tstr(d.toml, "tool", "tox", "legacy_tox_ini"), d.manifest, tomlLine(d.raw, "tool.tox")
	}
	names := map[string]int{}
	if text != "" {
		order, secs := parseINI(text)
		if tox := secs["tox"]; tox != nil {
			for _, e := range splitEnvList(tox.keys["envlist"]) {
				names[e] = tox.line
			}
		}
		for _, sec := range order {
			if n, ok := strings.CutPrefix(sec, "testenv:"); ok && !strings.HasPrefix(n, ".") {
				names[n] = secs[sec].line
			}
		}
	} else if envs := tget(d.toml, "tool", "tox"); envs != nil {
		file, line0 = d.manifest, tomlLine(d.raw, "tool.tox")
		for _, e := range tstrings(tget(d.toml, "tool", "tox", "env_list")) {
			names[e] = 0
		}
		if m, ok := tget(d.toml, "tool", "tox", "env").(map[string]any); ok {
			for n := range m {
				names[n] = 0
			}
		}
	}
	keys := make([]string, 0, len(names))
	for n := range names {
		keys = append(keys, n)
	}
	sort.Strings(keys)
	for _, n := range keys {
		line := names[n]
		if text != "" && line0 > 0 {
			line = line0 + line - 1 // legacy_tox_ini sits inside pyproject
		} else if line == 0 {
			line = line0
		}
		addPyTask(f, d, "tox", n, "", model.Source{Path: file, Line: line}, "tox -e "+n)
	}
}

// splitEnvList splits a tox envlist on commas/newlines outside braces and
// drops generative entries such as py{39,310}, which are not stable names.
func splitEnvList(s string) []string {
	var out []string
	depth, start := 0, 0
	flush := func(end int) {
		e := strings.TrimSpace(s[start:end])
		if e != "" && !strings.ContainsAny(e, "{}") {
			out = append(out, e)
		}
	}
	for i, c := range s {
		switch c {
		case '{':
			depth++
		case '}':
			depth--
		case ',', '\n':
			if depth == 0 {
				flush(i)
				start = i + 1
			}
		}
	}
	flush(len(s))
	return out
}

// ---- nox ----

var (
	noxDecoRe = regexp.MustCompile(`^@(?:nox\.)?session\b`)
	noxNameRe = regexp.MustCompile(`\bname\s*=\s*["']([^"']+)["']`)
	noxDefRe  = regexp.MustCompile(`^def\s+(\w+)\s*\(`)
)

func noxSessions(r *scan.Repo, f *Facts, d *pyDir) {
	file := d.file("noxfile.py")
	if !r.Has(file) {
		return
	}
	lines := strings.Split(r.ReadString(file), "\n")
	for i := 0; i < len(lines); i++ {
		if !noxDecoRe.MatchString(lines[i]) {
			continue
		}
		deco, start := lines[i], i+1
		// A decorator may span lines; stop once parentheses balance.
		for depth := strings.Count(deco, "(") - strings.Count(deco, ")"); depth > 0 && i+1 < len(lines); {
			i++
			deco += " " + lines[i]
			depth += strings.Count(lines[i], "(") - strings.Count(lines[i], ")")
		}
		name := ""
		if m := noxNameRe.FindStringSubmatch(deco); m != nil {
			name = m[1]
		}
		for j := i + 1; j < len(lines) && j < i+20; j++ {
			if m := noxDefRe.FindStringSubmatch(lines[j]); m != nil {
				if name == "" {
					name = m[1]
				}
				break
			}
			if strings.TrimSpace(lines[j]) != "" && !strings.HasPrefix(lines[j], "@") && !strings.HasPrefix(lines[j], " ") && !strings.HasPrefix(lines[j], ")") {
				break
			}
		}
		if name != "" {
			addPyTask(f, d, "nox", name, "", model.Source{Path: file, Line: start}, "nox -s "+name)
		}
	}
}
