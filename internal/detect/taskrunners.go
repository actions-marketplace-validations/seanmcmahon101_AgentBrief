package detect

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	"go.yaml.in/yaml/v3"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { register("taskrunners", detectTaskRunners) }

// ---- helpers shared by the ecosystem detectors in this package ----

// noisyDirs are directory names whose contents are fixtures, vendored code or
// build output rather than the project's own manifests.
var noisyDirs = map[string]bool{
	"testdata": true, "node_modules": true, "vendor": true, "third_party": true,
	"fixtures": true, "vendor-bin": true, "__fixtures__": true, "target": true, "bin": true, "obj": true,
	"_build": true, "deps": true, ".git": true,
}

// noisyPath reports whether p lies inside a directory that should not
// contribute manifests (the file's own name is not considered).
func noisyPath(p string) bool {
	for _, seg := range strings.Split(path.Dir(p), "/") {
		if noisyDirs[seg] {
			return true
		}
	}
	return false
}

// filesNamed returns all non-noisy files whose base name is one of names,
// shallowest first, then lexically.
func filesNamed(r *scan.Repo, names ...string) []string {
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	var out []string
	for _, p := range r.Files() {
		if want[path.Base(p)] && !noisyPath(p) {
			out = append(out, p)
		}
	}
	sortByDepth(out)
	return out
}

func sortByDepth(ps []string) {
	sort.SliceStable(ps, func(i, j int) bool {
		di, dj := strings.Count(ps[i], "/"), strings.Count(ps[j], "/")
		if di != dj {
			return di < dj
		}
		return ps[i] < ps[j]
	})
}

func joinPath(dir, name string) string {
	if dir == "." || dir == "" {
		return name
	}
	return dir + "/" + name
}

func dirOf(p string) string { return path.Dir(p) }

// lineNumber returns the 1-based line of the first occurrence of needle, or 0.
func lineNumber(content, needle string) int {
	i := strings.Index(content, needle)
	if i < 0 {
		return 0
	}
	return strings.Count(content[:i], "\n") + 1
}

func at(p string, line int) model.Source { return model.Source{Path: p, Line: line} }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return strings.TrimSpace(s[:n-3]) + "..."
}

// firstExisting returns the first of names that exists as a file in dir.
func firstExisting(r *scan.Repo, dir string, names ...string) string {
	for _, n := range names {
		if p := joinPath(dir, n); r.Has(p) {
			return p
		}
	}
	return ""
}

// projectDirs returns "." plus every project directory recorded so far.
func projectDirs(f *Facts) []string {
	seen := map[string]bool{".": true}
	out := []string{"."}
	for _, p := range f.Projects {
		if !seen[p.Dir] {
			seen[p.Dir] = true
			out = append(out, p.Dir)
		}
	}
	sort.Slice(out, func(i, j int) bool { return dirLess(out[i], out[j]) })
	return out
}

// depth is the number of directory segments in a relative directory.
func depth(dir string) int {
	if dir == "." || dir == "" {
		return 0
	}
	return strings.Count(dir, "/") + 1
}

func cmdEvidence(ev string) []string { return []string{ev} }

// ---- task runner detection ----

func detectTaskRunners(r *scan.Repo, f *Facts) {
	for _, dir := range projectDirs(f) {
		detectMake(r, f, dir)
		detectJust(r, f, dir)
		detectTaskfile(r, f, dir)
		detectMise(r, f, dir)
	}
}

// addRunnerCommand turns a conventional task name into a declared command.
func addRunnerCommand(f *Facts, name, run, dir string, src model.Source) {
	kind := KindForTaskName(name)
	if kind == "" {
		return
	}
	// By long-standing convention `make install` installs the built artifact
	// (PREFIX=/usr/local), not the project's dependencies.
	if kind == model.KindInstall && strings.EqualFold(name, "install") && strings.HasPrefix(run, "make") {
		return
	}
	f.AddCommand(model.Command{Kind: kind, Run: run, Dir: dir, Evidence: cmdEvidence(model.EvidenceDeclared), Source: src})
}

// ===================== Makefile =====================

type mkTarget struct {
	name  string
	line  int
	file  string // file the rule was found in (Makefile or an include)
	desc  string
	body  string
	phony bool
}

var (
	mkDefineRe  = regexp.MustCompile(`^(?:(?:export|override|private)\s+)*define\s`)
	mkCondRe    = regexp.MustCompile(`^(?:ifeq|ifneq|ifdef|ifndef|else|endif)\b`)
	mkIncludeRe = regexp.MustCompile(`^(?:-include|sinclude|include)\s+(.*)$`)
	mkTSVarRe   = regexp.MustCompile(`^\s*(?:(?:export|override|private)\s+)*[\w.\-${}()]+\s*(?:::?:?|[+?!])?=`)
)

func detectMake(r *scan.Repo, f *Facts, dir string) {
	mf := firstExisting(r, dir, "GNUmakefile", "makefile", "Makefile")
	if mf == "" {
		return
	}
	targets := parseMakefileTree(r, dir, mf)
	seenTask := map[string]bool{}
	for _, t := range targets {
		if seenTask[t.name] {
			continue
		}
		seenTask[t.name] = true
		src := at(t.file, t.line)
		f.AddTask(model.Task{Runner: "make", Name: t.name, Dir: dir, Body: t.body, Desc: t.desc, Source: src})
		if !mkCommandable(r, dir, t) {
			continue
		}
		addRunnerCommand(f, t.name, "make "+t.name, dir, src)
	}
}

// mkCommandable keeps file-like and shadowed targets out of Commands.
func mkCommandable(r *scan.Repo, dir string, t mkTarget) bool {
	if strings.ContainsAny(t.name, "/\\") || strings.Contains(t.name, ".") {
		return false
	}
	// A non-.PHONY target that shares its name with a file or directory is
	// skipped by make as "up to date": `make test` would silently do nothing.
	if !t.phony && r.Exists(joinPath(dir, t.name)) {
		return false
	}
	return true
}

// parseMakefileTree parses mf and the files it includes (one level).
func parseMakefileTree(r *scan.Repo, dir, mf string) []mkTarget {
	targets, includes, phony := parseMakefile(r.ReadString(mf), mf)
	for _, inc := range includes {
		for _, p := range expandInclude(r, dir, inc) {
			if p == mf {
				continue
			}
			ts, _, ph := parseMakefile(r.ReadString(p), p)
			targets = append(targets, ts...)
			for k := range ph {
				phony[k] = true
			}
		}
	}
	for i := range targets {
		targets[i].phony = phony[targets[i].name]
	}
	return targets
}

func expandInclude(r *scan.Repo, dir, inc string) []string {
	if strings.Contains(inc, "$") {
		return nil
	}
	p := path.Clean(joinPath(dir, inc))
	if strings.HasPrefix(p, "../") || path.IsAbs(inc) {
		return nil
	}
	if strings.ContainsAny(inc, "*?[") {
		g := r.Glob(p)
		if len(g) > 20 {
			g = g[:20]
		}
		return g
	}
	if r.Has(p) {
		return []string{p}
	}
	return nil
}

// mkLogicalLines joins backslash continuations, remembering the first line number.
type mkLine struct {
	text string
	no   int
}

func mkLogicalLines(content string) []mkLine {
	raw := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var out []mkLine
	for i := 0; i < len(raw); i++ {
		l := raw[i]
		start := i + 1
		for strings.HasSuffix(l, "\\") && i+1 < len(raw) {
			i++
			l = strings.TrimSuffix(l, "\\") + " " + strings.TrimLeft(raw[i], " \t")
		}
		out = append(out, mkLine{l, start})
	}
	return out
}

// parseMakefile extracts rule targets, include operands and .PHONY names.
func parseMakefile(content, file string) (targets []mkTarget, includes []string, phony map[string]bool) {
	phony = map[string]bool{}
	defineDepth := 0
	var current []int // indexes into targets of the rule receiving recipe lines
	for _, ln := range mkLogicalLines(content) {
		text := ln.text
		if defineDepth > 0 {
			t := strings.TrimSpace(text)
			if mkDefineRe.MatchString(t + " ") {
				defineDepth++
			} else if t == "endef" || strings.HasPrefix(t, "endef ") || strings.HasPrefix(t, "endef#") {
				defineDepth--
			}
			continue
		}
		if strings.HasPrefix(text, "\t") {
			cmd := strings.TrimSpace(text)
			cmd = strings.TrimLeft(cmd, "@-+ \t")
			if cmd == "" || strings.HasPrefix(cmd, "#") || isMakeNoise(cmd) {
				continue
			}
			for _, i := range current {
				if targets[i].body == "" {
					targets[i].body = truncate(strings.Join(strings.Fields(cmd), " "), 120)
				}
			}
			continue
		}
		t := strings.TrimSpace(text)
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		if mkDefineRe.MatchString(t + " ") {
			defineDepth = 1
			current = nil
			continue
		}
		if mkCondRe.MatchString(t) {
			continue
		}
		if m := mkIncludeRe.FindStringSubmatch(t); m != nil {
			includes = append(includes, strings.Fields(stripMkComment(m[1]))...)
			current = nil
			continue
		}
		current = nil
		colon := mkRuleColon(t)
		if colon < 0 {
			continue // assignment, directive, or junk
		}
		lhs := strings.TrimSpace(t[:colon])
		rest := t[colon+1:]
		rest = strings.TrimPrefix(rest, ":") // double-colon rule
		if strings.HasPrefix(strings.TrimSpace(rest), "=") || mkTSVarRe.MatchString(rest) {
			continue // target-specific variable, not a rule
		}
		lhs = strings.TrimSuffix(lhs, "&") // grouped targets
		desc := ""
		if i := strings.Index(rest, "##"); i >= 0 {
			desc = strings.TrimSpace(strings.TrimLeft(rest[i:], "#"))
			rest = rest[:i]
		}
		rest = stripMkComment(rest)
		inline := ""
		if i := strings.Index(rest, ";"); i >= 0 {
			inline = strings.TrimSpace(rest[i+1:])
			rest = rest[:i]
		}
		names := strings.Fields(lhs)
		if lhs == ".PHONY" {
			for _, d := range strings.Fields(rest) {
				phony[d] = true
			}
			continue
		}
		for _, n := range names {
			if !validMakeTarget(n) {
				continue
			}
			targets = append(targets, mkTarget{name: n, line: ln.no, file: file, desc: truncate(desc, 120), body: truncate(strings.TrimLeft(inline, "@-+ "), 120)})
			current = append(current, len(targets)-1)
		}
	}
	return targets, includes, phony
}

func validMakeTarget(n string) bool {
	if n == "" || strings.HasPrefix(n, ".") {
		return false
	}
	return !strings.ContainsAny(n, "%$*()=?\"'`{}<>|")
}

func stripMkComment(s string) string {
	if i := strings.Index(s, "#"); i >= 0 {
		return s[:i]
	}
	return s
}

// mkRuleColon returns the index of the colon that separates targets from
// prerequisites, or -1 when the line is a variable assignment or has no rule.
func mkRuleColon(t string) int {
	depth := 0
	for i := 0; i < len(t); i++ {
		switch c := t[i]; c {
		case '$':
			if i+1 < len(t) && (t[i+1] == '(' || t[i+1] == '{') {
				depth++
				i++
			}
		case ')', '}':
			if depth > 0 {
				depth--
			}
		case '=':
			if depth == 0 {
				return -1 // "=" before any rule colon: assignment
			}
		case '#':
			if depth == 0 {
				return -1
			}
		case ':':
			if depth > 0 {
				continue
			}
			rest := t[i+1:]
			if strings.HasPrefix(rest, "=") || strings.HasPrefix(rest, ":=") || strings.HasPrefix(rest, "::=") {
				return -1 // :=, ::= and :::= assignments
			}
			return i
		}
	}
	return -1
}

// ===================== justfile =====================

type justRecipe struct {
	name      string
	line      int
	desc      string
	body      string
	private   bool
	needsArgs bool // has a parameter without a default
}

func detectJust(r *scan.Repo, f *Facts, dir string) {
	jf := firstExisting(r, dir, "justfile", "Justfile", ".justfile")
	if jf == "" {
		return
	}
	seen := map[string]bool{}
	for _, rec := range parseJustfile(r.ReadString(jf)) {
		if seen[rec.name] {
			continue // OS-specific duplicates share a name
		}
		seen[rec.name] = true
		src := at(jf, rec.line)
		f.AddTask(model.Task{Runner: "just", Name: rec.name, Dir: dir, Body: rec.body, Desc: rec.desc, Source: src})
		if rec.private || rec.needsArgs {
			continue
		}
		addRunnerCommand(f, rec.name, "just "+rec.name, dir, src)
	}
}

var (
	justNameRe = regexp.MustCompile(`^@?([A-Za-z_][A-Za-z0-9_-]*)`)
	justPrivRe = regexp.MustCompile(`\bprivate\b`)
	justSkipKw = regexp.MustCompile(`^(set|alias|import|mod|export|unexport)\s`)
)

func parseJustfile(content string) []justRecipe {
	var out []justRecipe
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")
	var cur *justRecipe
	pendingPrivate := false
	doc := ""
	prevComment := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if line != "" && (line[0] == ' ' || line[0] == '\t') {
			// recipe body (or continuation of an assignment)
			if cur != nil && cur.body == "" && trimmed != "" {
				if strings.HasPrefix(trimmed, "#!") {
					cur = nil // shebang recipe: no single command to show
				} else if !strings.HasPrefix(trimmed, "#") {
					cur.body = truncate(strings.TrimLeft(trimmed, "@- "), 120)
				}
			}
			continue
		}
		cur = nil
		if trimmed == "" {
			doc, prevComment, pendingPrivate = "", false, false
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			if strings.HasPrefix(trimmed, "#!") {
				continue
			}
			doc = strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
			prevComment = true
			continue
		}
		if strings.HasPrefix(trimmed, "[") {
			if justPrivRe.MatchString(trimmed) {
				pendingPrivate = true
			}
			continue // attribute line; keeps the doc comment above it
		}
		thisDoc := ""
		if prevComment {
			thisDoc = doc
		}
		priv := pendingPrivate
		doc, prevComment, pendingPrivate = "", false, false
		if justSkipKw.MatchString(trimmed) {
			continue
		}
		rec, ok := parseJustHeader(trimmed)
		if !ok {
			continue
		}
		rec.line = i + 1
		rec.desc = truncate(thisDoc, 120)
		rec.private = rec.private || priv
		out = append(out, rec)
		cur = &out[len(out)-1]
	}
	return out
}

// parseJustHeader parses `[@]name params: deps`, rejecting `name := value`.
func parseJustHeader(t string) (justRecipe, bool) {
	m := justNameRe.FindStringSubmatch(t)
	if m == nil {
		return justRecipe{}, false
	}
	rest := t[len(m[0]):]
	colon := justColon(rest)
	if colon < 0 {
		return justRecipe{}, false
	}
	if strings.HasPrefix(rest[colon:], ":=") {
		return justRecipe{}, false
	}
	// "name = value" (deprecated assignment) has no colon before "=" and is rejected above.
	rec := justRecipe{name: m[1], private: strings.HasPrefix(m[1], "_")}
	for _, p := range justParams(rest[:colon]) {
		if p.required {
			rec.needsArgs = true
		}
	}
	return rec, true
}

// justColon finds the first ':' outside quotes, or -1 if an "=" without a
// default context (plain assignment) comes first.
func justColon(s string) int {
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case ':':
			return i
		}
	}
	return -1
}

type justParam struct{ required bool }

// justParams splits the parameter list (everything between the name and ':').
func justParams(s string) []justParam {
	var out []justParam
	var quote byte
	var tok strings.Builder
	flush := func() {
		t := tok.String()
		tok.Reset()
		if t == "" {
			return
		}
		t = strings.TrimLeft(t, "$")
		optional := strings.HasPrefix(t, "*") || strings.Contains(t, "=")
		out = append(out, justParam{required: !optional})
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			tok.WriteByte(c)
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
			tok.WriteByte(c)
		case ' ', '\t':
			flush()
		default:
			tok.WriteByte(c)
		}
	}
	flush()
	return out
}

// ===================== Taskfile =====================

func detectTaskfile(r *scan.Repo, f *Facts, dir string) {
	tf := firstExisting(r, dir, "Taskfile.yml", "Taskfile.yaml", "taskfile.yml", "taskfile.yaml", "Taskfile.dist.yml", "Taskfile.dist.yaml")
	if tf == "" {
		return
	}
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(r.ReadString(tf)), &doc); err != nil || len(doc.Content) == 0 {
		return
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return
	}
	if inc := yamlKey(root, "includes"); inc != nil && inc.Kind == yaml.MappingNode {
		var names []string
		for i := 0; i+1 < len(inc.Content); i += 2 {
			names = append(names, inc.Content[i].Value)
		}
		if len(names) > 0 {
			f.AddNote(model.Note{Text: "Taskfile includes namespaces: " + strings.Join(capList(names, 8), ", "), Source: at(tf, inc.Line)})
		}
	}
	tasks := yamlKey(root, "tasks")
	if tasks == nil || tasks.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(tasks.Content); i += 2 {
		k, v := tasks.Content[i], tasks.Content[i+1]
		name := k.Value
		body, desc, internal := taskfileEntry(v)
		src := at(tf, k.Line)
		f.AddTask(model.Task{Runner: "task", Name: name, Dir: dir, Body: body, Desc: desc, Source: src})
		if !internal {
			addRunnerCommand(f, name, "task "+name, dir, src)
		}
	}
}

func taskfileEntry(v *yaml.Node) (body, desc string, internal bool) {
	switch v.Kind {
	case yaml.ScalarNode:
		return truncate(v.Value, 120), "", false
	case yaml.SequenceNode:
		return firstCmd(v), "", false
	case yaml.MappingNode:
		if n := yamlKey(v, "internal"); n != nil && n.Value == "true" {
			internal = true
		}
		if n := yamlKey(v, "desc"); n != nil {
			desc = truncate(n.Value, 120)
		}
		if n := yamlKey(v, "cmd"); n != nil {
			body = truncate(n.Value, 120)
		} else if n := yamlKey(v, "cmds"); n != nil && n.Kind == yaml.SequenceNode {
			body = firstCmd(n)
		}
	}
	return
}

// firstCmd returns the first plain-string command of a cmds sequence.
func firstCmd(seq *yaml.Node) string {
	if len(seq.Content) == 0 {
		return ""
	}
	c := seq.Content[0]
	if c.Kind == yaml.ScalarNode {
		return truncate(strings.Join(strings.Fields(c.Value), " "), 120)
	}
	if c.Kind == yaml.MappingNode {
		if n := yamlKey(c, "cmd"); n != nil {
			return truncate(strings.Join(strings.Fields(n.Value), " "), 120)
		}
	}
	return "" // the first command is a task call or another non-string form
}

func yamlKey(m *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

func capList(s []string, n int) []string {
	if len(s) <= n {
		return s
	}
	return append(append([]string(nil), s[:n]...), "...")
}

// ===================== mise =====================

// miseConfigPaths are the mise config locations, in precedence order.
var miseConfigPaths = []string{"mise.toml", ".mise.toml", ".config/mise.toml", ".config/mise/config.toml"}

var miseTaskDirs = []string{"mise-tasks", ".mise/tasks", ".config/mise/tasks", "mise/tasks"}

func detectMise(r *scan.Repo, f *Facts, dir string) {
	for _, rel := range miseConfigPaths {
		p := joinPath(dir, rel)
		if !r.Has(p) {
			continue
		}
		content := r.ReadString(p)
		var cfg struct {
			Tasks map[string]any `toml:"tasks"`
		}
		if _, err := toml.Decode(content, &cfg); err != nil {
			continue
		}
		names := make([]string, 0, len(cfg.Tasks))
		for n := range cfg.Tasks {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			body, desc, hidden := miseTask(cfg.Tasks[n])
			src := at(p, miseTaskLine(content, n))
			f.AddTask(model.Task{Runner: "mise", Name: n, Dir: dir, Body: body, Desc: desc, Source: src})
			if !hidden {
				addRunnerCommand(f, n, "mise run "+n, dir, src)
			}
		}
	}
	for _, td := range miseTaskDirs {
		for _, p := range r.FilesUnder(joinPath(dir, td)) {
			rel := strings.TrimPrefix(p, joinPath(dir, td)+"/")
			if strings.HasPrefix(path.Base(rel), ".") {
				continue
			}
			name := strings.ReplaceAll(rel, "/", ":")
			f.AddTask(model.Task{Runner: "mise", Name: name, Dir: dir, Desc: miseFileDesc(r.ReadString(p)), Source: at(p, 0)})
			addRunnerCommand(f, name, "mise run "+name, dir, at(p, 0))
		}
	}
}

// miseTask reads a [tasks.X] value: a string, an array, or a table.
func miseTask(v any) (body, desc string, hidden bool) {
	switch t := v.(type) {
	case string:
		return truncate(t, 120), "", false
	case []any:
		return miseRun(t), "", false
	case map[string]any:
		if h, ok := t["hide"].(bool); ok {
			hidden = h
		}
		if d, ok := t["description"].(string); ok {
			desc = truncate(d, 120)
		}
		body = miseRun(t["run"])
	}
	return
}

func miseRun(v any) string {
	switch t := v.(type) {
	case string:
		return truncate(strings.Join(strings.Fields(t), " "), 120)
	case []any:
		if len(t) > 0 {
			if s, ok := t[0].(string); ok {
				return truncate(strings.Join(strings.Fields(s), " "), 120)
			}
		}
	}
	return ""
}

func miseTaskLine(content, name string) int {
	for _, pat := range []string{"[tasks." + name + "]", "[tasks.\"" + name + "\"]", "[tasks.'" + name + "']"} {
		if n := lineNumber(content, pat); n > 0 {
			return n
		}
	}
	if n := lineNumber(content, "\n"+name+" ="); n > 0 {
		return n + 1
	}
	return 0
}

var miseDescRe = regexp.MustCompile(`(?m)^#\s*\[?MISE\]?\s+description\s*=\s*"([^"]*)"`)

func miseFileDesc(content string) string {
	if len(content) > 2000 {
		content = content[:2000]
	}
	if m := miseDescRe.FindStringSubmatch(content); m != nil {
		return truncate(m[1], 120)
	}
	return ""
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// isMakeNoise reports recipe lines that only print progress, which say
// nothing about what the target actually does.
func isMakeNoise(cmd string) bool {
	return strings.HasPrefix(cmd, "$(info ") || strings.HasPrefix(cmd, "$(warning ") ||
		strings.HasPrefix(cmd, "echo ") || strings.HasPrefix(cmd, "printf ") || cmd == "echo"
}
