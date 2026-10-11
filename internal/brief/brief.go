// Package brief curates a model.Repo into a small, sourced set of operational
// facts and renders it as the AgentBrief managed block (Markdown) or JSON.
package brief

import (
	"path"
	"sort"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/redact"
)

// Schema identifies the JSON form of a Brief.
const Schema = "agentbrief/brief/v1"

// Limits that keep the block small.
const (
	MaxCommandLen   = 160
	MaxListed       = 8 // workspace members
	MaxDoNotEdit    = 6
	MaxNotes        = 5
	MaxInstructions = 5
	maxToolVersions = 2
)

// Brief is the curated, scope-specific view of a repository.
type Brief struct {
	Schema string `json:"schema"`
	// Scope is "." for the whole repo or the directory the brief is scoped to.
	Scope string `json:"scope"`
	// Project is the project that owns Scope (nil for the repo root scope).
	Project *ProjectRef `json:"project,omitempty"`

	Toolchain       []Tool    `json:"toolchain"`
	PackageManagers []PM      `json:"package_managers"`
	Commands        []Command `json:"commands"`
	// Ecosystems lists the ecosystems at the root when more than one is present.
	Ecosystems []ProjectRef `json:"ecosystems"`
	Workspace  *Workspace   `json:"workspace,omitempty"`
	DoNotEdit  []DoNotEdit  `json:"do_not_edit"`
	// InstructionFiles lists other agent-instruction files (never the root AGENTS.md).
	InstructionFiles []string `json:"instruction_files"`
	Notes            []string `json:"notes"`
	// OtherBuildTool names a second JVM build tool present alongside the chosen one.
	OtherBuildTool *Alt `json:"other_build_tool,omitempty"`
}

// ProjectRef names a project.
type ProjectRef struct {
	Dir       string `json:"dir"`
	Name      string `json:"name,omitempty"`
	Ecosystem string `json:"ecosystem"`
	Manifest  string `json:"manifest,omitempty"`
}

// Tool is a pinned toolchain version.
type Tool struct {
	Name    string   `json:"name"`
	Version string   `json:"version"`
	Sources []string `json:"sources"`
}

// PM is a package manager.
type PM struct {
	Name      string   `json:"name"`
	Version   string   `json:"version,omitempty"`
	Ecosystem string   `json:"ecosystem"`
	Source    string   `json:"source"`
	Avoid     []string `json:"avoid,omitempty"` // "use X, not Y"
}

// Command is the single best command for a kind.
type Command struct {
	Kind     string   `json:"kind"`
	Run      string   `json:"run"`
	Dir      string   `json:"dir"`
	Label    string   `json:"source"` // short provenance label, e.g. "package.json, CI"
	Evidence []string `json:"evidence"`
	// Verified is true when the command is declared in the repo or run in CI.
	Verified bool `json:"verified"`
	// Adapted is true when Run was rewritten for the scoped project (e.g. cargo -p).
	Adapted bool `json:"adapted,omitempty"`
	// Alternate is a second declared command of another runner for the same kind.
	Alternate *Alt   `json:"alternate,omitempty"`
	Note      string `json:"note,omitempty"`
}

// Alt is an alternative command or build tool.
type Alt struct {
	Tool  string `json:"tool,omitempty"`
	Run   string `json:"run"`
	Label string `json:"source"`
}

// Workspace summarises sub-projects.
type Workspace struct {
	Kind    string         `json:"kind"` // "workspace" or "projects"
	Manager string         `json:"manager,omitempty"`
	Count   int            `json:"count"`
	Capped  bool           `json:"count_capped,omitempty"` // detection stopped at its member limit
	Members []ProjectRef   `json:"members"`
	Omitted int            `json:"omitted"`
	Extra   map[string]int `json:"-"`
}

// DoNotEdit is a path the agent should not hand-edit.
type DoNotEdit struct {
	Pattern string `json:"pattern"`
	Reason  string `json:"reason"`
	// Group is set for the compact lockfile item; Patterns lists its members.
	Patterns []string `json:"patterns,omitempty"`
}

var kindOrder = []string{
	model.KindInstall, model.KindBuild, model.KindTest, model.KindTestOne, model.KindLint,
	model.KindFormat, model.KindTypecheck, model.KindCheck, model.KindGenerate, model.KindDev,
}

// KindLabel is the display name of a command kind.
func KindLabel(k string) string {
	if k == model.KindTestOne {
		return "single test"
	}
	return k
}

// CleanScope normalises a scope path ("" and "./" become ".").
func CleanScope(scope string) string {
	scope = strings.ReplaceAll(scope, "\\", "/")
	scope = path.Clean(scope)
	scope = strings.TrimPrefix(scope, "./")
	if scope == "" || scope == "/" {
		return "."
	}
	return scope
}

func within(dir, base string) bool { // is dir == base or under base
	if base == "." {
		return true
	}
	return dir == base || strings.HasPrefix(dir, base+"/")
}

// OwningProject returns the project whose Dir is the longest prefix of scope.
func OwningProject(repo *model.Repo, scope string) (model.Project, bool) {
	scope = CleanScope(scope)
	var best model.Project
	found := false
	for _, p := range repo.Projects {
		if !within(scope, p.Dir) {
			continue
		}
		if !found || len(p.Dir) > len(best.Dir) {
			best, found = p, true
		}
	}
	return best, found
}

// Build curates repo for scope ("." for the whole repository, or a repo-relative
// path). A path that is not inside any project falls back to the root scope.
func Build(repo *model.Repo, scope string) Brief {
	scope = CleanScope(scope)
	b := Brief{
		Schema:           Schema,
		Scope:            ".",
		Toolchain:        []Tool{},
		PackageManagers:  []PM{},
		Commands:         []Command{},
		Ecosystems:       []ProjectRef{},
		DoNotEdit:        []DoNotEdit{},
		InstructionFiles: []string{},
		Notes:            []string{},
	}
	pdir := "."
	if scope != "." {
		if p, ok := OwningProject(repo, scope); ok && p.Dir != "." {
			pdir = p.Dir
			b.Scope = p.Dir
			b.Project = &ProjectRef{Dir: p.Dir, Name: ident(p.Name), Ecosystem: p.Ecosystem, Manifest: p.Manifest}
		}
	}

	b.Toolchain = buildToolchain(repo, pdir)
	b.PackageManagers = buildPMs(repo, pdir, b.Toolchain)
	b.Commands, b.OtherBuildTool = buildCommands(repo, pdir)
	b.Ecosystems = rootEcosystems(repo, pdir)
	b.Workspace = buildWorkspace(repo, pdir)
	b.DoNotEdit = buildDoNotEdit(repo, pdir)
	b.InstructionFiles = buildInstructionList(repo, pdir)
	b.Notes = buildNotes(repo)
	return b
}

func isCIPath(p string) bool {
	return strings.HasPrefix(p, ".github/") || strings.HasPrefix(p, ".gitlab") || strings.HasPrefix(p, ".circleci/") ||
		strings.HasPrefix(p, ".buildkite/") || p == "azure-pipelines.yml" || p == "Jenkinsfile"
}

// sourceRank orders pin sources: root-level files, then shallower paths, CI last.
func sourceRank(p string) int {
	r := strings.Count(p, "/")
	if isCIPath(p) {
		r += 100
	}
	return r
}

// ---- toolchain -------------------------------------------------------------

// primaryDirs are the directories whose pins count for the repo-wide brief: the
// root plus top-level projects that are workspace roots or the only project of
// their ecosystem (e.g. codex-rs for Rust).
func primaryDirs(repo *model.Repo) map[string]bool {
	out := map[string]bool{".": true}
	perEco := map[string]int{}
	type cand struct {
		dir  string
		size int
	}
	best := map[string]cand{}
	rootEco := map[string]bool{}
	for _, p := range repo.Projects {
		perEco[p.Ecosystem]++
		if p.Dir == "." {
			rootEco[p.Ecosystem] = true
		}
	}
	for _, p := range repo.Projects {
		if p.Dir == "." || rootEco[p.Ecosystem] {
			continue
		}
		nested := false
		for _, q := range repo.Projects {
			if q.Dir != "." && q.Dir != p.Dir && within(p.Dir, q.Dir) {
				nested = true
			}
		}
		if !nested && (p.IsWorkspaceRoot || perEco[p.Ecosystem] == 1) {
			size := 0
			for _, q := range repo.Projects {
				if within(q.Dir, p.Dir) {
					size++
				}
			}
			cur, ok := best[p.Ecosystem]
			if !ok || size > cur.size || (size == cur.size && p.Dir < cur.dir) {
				best[p.Ecosystem] = cand{p.Dir, size}
			}
		}
	}
	for _, c := range best {
		out[c.dir] = true
	}
	return out
}

func buildToolchain(repo *model.Repo, pdir string) []Tool {
	byName := map[string][]model.Toolchain{}
	var names []string
	for _, t := range repo.Toolchains {
		if t.Name == "" || t.Version == "" {
			continue
		}
		if _, ok := byName[t.Name]; !ok {
			names = append(names, t.Name)
		}
		byName[t.Name] = append(byName[t.Name], t)
	}
	sort.Strings(names)
	primary := primaryDirs(repo)
	var out []Tool
	for _, n := range names {
		cands := byName[n]
		var nonCI []model.Toolchain
		for _, t := range cands {
			if !isCIPath(t.Source.Path) {
				nonCI = append(nonCI, t)
			}
		}
		cands = nonCI
		if len(cands) == 0 {
			continue
		}
		// File-sourced pins beat CI-workflow pins.
		var filePins []model.Toolchain
		for _, t := range cands {
			if !isCIPath(t.Source.Path) {
				filePins = append(filePins, t)
			}
		}
		if len(filePins) > 0 {
			cands = filePins
		}
		sort.SliceStable(cands, func(i, j int) bool { return sourceRank(cands[i].Source.Path) < sourceRank(cands[j].Source.Path) })
		// Prefer pins that apply to the scope (source dir is an ancestor of, or equal to, pdir).
		var applicable []model.Toolchain
		for _, t := range cands {
			d := path.Dir(t.Source.Path)
			if pdir == "." {
				if primary[d] {
					applicable = append(applicable, t)
				}
			} else if within(pdir, d) {
				applicable = append(applicable, t)
			}
		}
		if pdir == "." {
			var root []model.Toolchain
			for _, t := range applicable {
				if path.Dir(t.Source.Path) == "." {
					root = append(root, t)
				}
			}
			if len(root) > 0 {
				applicable = root
			}
		}
		// If the scope itself pins the tool, ignore ancestors' pins.
		var own []model.Toolchain
		for _, t := range applicable {
			if path.Dir(t.Source.Path) == pdir && pdir != "." {
				own = append(own, t)
			}
		}
		if len(own) > 0 {
			applicable = own
		}
		var versions []string
		srcs := map[string][]string{}
		for _, t := range applicable {
			v := ident(t.Version)
			if v == "" {
				continue
			}
			if _, ok := srcs[v]; !ok {
				versions = append(versions, v)
			}
			s := ident(t.Source.Path)
			if s != "" && !contains(srcs[v], s) {
				srcs[v] = append(srcs[v], s)
			} else if _, ok := srcs[v]; !ok {
				srcs[v] = nil
			}
		}
		for i, v := range versions {
			if i >= maxToolVersions {
				break
			}
			s := srcs[v]
			if len(s) > 2 {
				s = s[:2]
			}
			out = append(out, Tool{Name: n, Version: v, Sources: nonNil(s)})
		}
	}
	return nonNilTools(out)
}

func nonNilTools(t []Tool) []Tool {
	if t == nil {
		return []Tool{}
	}
	return t
}

func buildPMs(repo *model.Repo, pdir string, tools []Tool) []PM {
	hidden := map[string]bool{"go": true, "cargo": true}
	// nearest PM per ecosystem governing pdir
	best := map[string]model.PackageManager{}
	for _, pm := range repo.PackageManagers {
		if !within(pdir, pm.Dir) {
			continue
		}
		if cur, ok := best[pm.Ecosystem]; !ok || len(pm.Dir) > len(cur.Dir) {
			best[pm.Ecosystem] = pm
		}
	}
	var ecos []string
	for e := range best {
		ecos = append(ecos, e)
	}
	sort.Strings(ecos)
	var out []PM
	for _, e := range ecos {
		pm := best[e]
		if hidden[pm.Name] {
			continue
		}
		dup := false
		for _, t := range tools {
			if t.Name == pm.Name {
				dup = true
			}
		}
		if dup {
			continue
		}
		if pm.Version == "" && len(avoidFor(pm)) == 0 {
			continue
		}
		out = append(out, PM{Name: ident(pm.Name), Version: ident(pm.Version), Ecosystem: pm.Ecosystem,
			Source: ident(pm.Source.Path), Avoid: avoidFor(pm)})
	}
	if out == nil {
		return []PM{}
	}
	return out
}

func avoidFor(pm model.PackageManager) []string {
	switch pm.Ecosystem {
	case "node":
		switch pm.Name {
		case "pnpm", "bun":
			return []string{"npm", "yarn"}
		case "yarn":
			return []string{"npm", "pnpm"}
		}
	case "python":
		switch pm.Name {
		case "uv", "poetry", "pdm", "pipenv", "hatch", "rye":
			return []string{"pip"}
		}
	}
	return nil
}

// ---- commands --------------------------------------------------------------

var runnerTools = map[string]bool{
	"make": true, "just": true, "task": true, "mise": true, "npm": true, "pnpm": true, "yarn": true,
	"bun": true, "composer": true, "rake": true, "poe": true, "nox": true, "invoke": true, "inv": true,
}

func isRunnerCommand(run string) bool {
	f := strings.Fields(run)
	if len(f) == 0 || !runnerTools[f[0]] {
		return false
	}
	if len(f) > 1 {
		switch f[1] {
		case "exec", "dlx", "x":
			return false
		}
	}
	return true
}

// evidenceScore: 0 declared+ci, 1 declared, 2 ci, 3 convention.
func evidenceScore(ev []string) int {
	var ci, decl bool
	for _, e := range ev {
		switch e {
		case model.EvidenceCI:
			ci = true
		case model.EvidenceDeclared:
			decl = true
		}
	}
	switch {
	case ci && decl:
		return 0
	case decl:
		return 1
	case ci:
		return 2
	}
	return 3
}

func usableRun(run string) (string, bool) {
	run = strings.TrimSpace(run)
	if run == "" || len(run) > MaxCommandLen {
		return "", false
	}
	for _, r := range run {
		if r < 0x20 || r == 0x7f {
			return "", false
		}
	}
	run = redact.String(run)
	if len(run) > MaxCommandLen {
		return "", false
	}
	return run, true
}

func firstTool(run string) string {
	f := strings.Fields(run)
	if len(f) == 0 {
		return ""
	}
	return path.Base(f[0])
}

func jvmTool(run string) string {
	switch firstTool(run) {
	case "gradlew", "gradlew.bat", "gradle":
		return "gradle"
	case "mvnw", "mvnw.cmd", "mvn":
		return "maven"
	}
	return ""
}

var jsPMTools = set("npm pnpm yarn bun")

var alternateKinds = map[string]bool{
	model.KindFormat: true, model.KindLint: true, model.KindTest: true, model.KindTypecheck: true,
}

var adaptKinds = map[string]bool{
	model.KindTest: true, model.KindTestOne: true, model.KindBuild: true, model.KindLint: true,
	model.KindTypecheck: true, model.KindCheck: true,
}

// adaptCargo rewrites a workspace-level cargo command to target one crate.
func adaptCargo(run, crate string) (string, bool) {
	if !validPkg(crate) || strings.ContainsAny(crate, " ") {
		return "", false
	}
	f := strings.Fields(run)
	if len(f) < 2 || f[0] != "cargo" {
		return "", false
	}
	for _, w := range f {
		if w == "-p" || w == "--package" || strings.HasPrefix(w, "--package=") {
			return "", false
		}
	}
	for i, w := range f {
		if w == "--workspace" || w == "--all" {
			f[i] = "-p " + crate
			return strings.Join(f, " "), true
		}
	}
	end := 2
	if f[1] == "nextest" && len(f) > 2 && f[2] == "run" {
		end = 3
	}
	if f[1] != "test" && f[1] != "build" && f[1] != "clippy" && f[1] != "check" && f[1] != "nextest" && f[1] != "doc" {
		return "", false
	}
	out := append(append(append([]string{}, f[:end]...), "-p", crate), f[end:]...)
	return strings.Join(out, " "), true
}

func buildCommands(repo *model.Repo, pdir string) ([]Command, *Alt) {
	var out []Command
	var other *Alt
	seen := map[string]bool{}

	owner, hasOwner := model.Project{}, false
	if pdir != "." {
		for _, p := range repo.Projects {
			if p.Dir == pdir {
				owner, hasOwner = p, true
				break
			}
		}
	}

	pick := func(dir string, adapt bool) {
		var all []model.Command
		for _, c := range repo.Commands {
			if c.Dir != dir {
				continue
			}
			if _, ok := usableRun(c.Run); ok {
				all = append(all, c)
			}
		}
		// One JVM build tool per directory.
		ci := map[string]int{}
		for _, c := range all {
			if t := jvmTool(c.Run); t != "" && evidenceScore(c.Evidence) != 3 && (evidenceScore(c.Evidence) == 0 || evidenceScore(c.Evidence) == 2) {
				ci[t]++
			}
		}
		present := map[string]bool{}
		for _, c := range all {
			if t := jvmTool(c.Run); t != "" {
				present[t] = true
			}
		}
		if present["gradle"] && present["maven"] {
			chosen := "gradle"
			switch {
			case ci["maven"] > ci["gradle"]:
				chosen = "maven"
			case ci["maven"] == ci["gradle"] && hasPom(repo, dir, all):
				chosen = "maven"
			}
			notChosen := "maven"
			if chosen == "maven" {
				notChosen = "gradle"
			}
			var keep []model.Command
			var bestOther *model.Command
			for i, c := range all {
				if jvmTool(c.Run) == notChosen {
					if other == nil && (bestOther == nil || (c.Kind == model.KindBuild && bestOther.Kind != model.KindBuild)) {
						bestOther = &all[i]
					}
					continue
				}
				keep = append(keep, c)
			}
			all = keep
			if bestOther != nil && other == nil {
				run, _ := usableRun(bestOther.Run)
				other = &Alt{Tool: map[string]string{"gradle": "Gradle", "maven": "Maven"}[notChosen], Run: run, Label: commandLabel(*bestOther, dir)}
			}
		}
		for _, kind := range kindOrder {
			if seen[kind] {
				continue
			}
			var cands []model.Command
			for _, c := range all {
				if c.Kind == kind {
					cands = append(cands, c)
				}
			}
			if len(cands) == 0 {
				continue
			}
			sort.SliceStable(cands, func(i, j int) bool {
				a, b := cands[i], cands[j]
				if ea, eb := evidenceScore(a.Evidence), evidenceScore(b.Evidence); ea != eb {
					return ea < eb
				}
				if kind == model.KindInstall {
					if ra, rb := restrictsDeps(a.Run), restrictsDeps(b.Run); ra != rb {
						return !ra
					}
					if len(a.Run) != len(b.Run) {
						return len(a.Run) < len(b.Run)
					}
				}
				if ra, rb := isRunnerCommand(a.Run), isRunnerCommand(b.Run); ra != rb {
					return ra
				}
				if len(a.Run) != len(b.Run) {
					return len(a.Run) < len(b.Run)
				}
				return a.Run < b.Run
			})
			c := cands[0]
			run, _ := usableRun(c.Run)
			cmd := Command{
				Kind: kind, Run: run, Dir: dir, Label: commandLabel(c, dir),
				Evidence: nonNil(c.Evidence), Verified: evidenceScore(c.Evidence) < 3, Note: safeNote(c.Note),
			}
			if adapt && hasOwner && owner.Ecosystem == "rust" && adaptKinds[kind] {
				if r2, ok := adaptCargo(run, owner.Name); ok && len(r2) <= MaxCommandLen {
					cmd.Run, cmd.Adapted = r2, true
				}
			}
			if alternateKinds[kind] && evidenceScore(c.Evidence) <= 1 {
				for _, d := range cands[1:] {
					if evidenceScore(d.Evidence) > 1 || firstTool(d.Run) == firstTool(c.Run) ||
						(jsPMTools[firstTool(d.Run)] && jsPMTools[firstTool(c.Run)]) {
						continue
					}
					dr, _ := usableRun(d.Run)
					cmd.Alternate = &Alt{Run: dr, Label: commandLabel(d, dir)}
					break
				}
			}
			out = append(out, cmd)
			seen[kind] = true
		}
	}

	pick(pdir, false)
	if pdir != "." {
		// Walk up: enclosing projects (e.g. a Cargo/pnpm workspace root), then the repo root.
		var anc []string
		for _, p := range repo.Projects {
			if p.Dir != "." && p.Dir != pdir && within(pdir, p.Dir) {
				anc = append(anc, p.Dir)
			}
		}
		sort.Slice(anc, func(i, j int) bool { return len(anc[i]) > len(anc[j]) })
		for _, d := range anc {
			pick(d, true)
		}
		pick(".", false)
	} else {
		pick(".", false)
		if d, ok := soleFallbackDir(repo); ok {
			pick(d, false)
		}
	}
	if out == nil {
		out = []Command{}
	}
	return out, other
}

func hasPom(repo *model.Repo, dir string, cmds []model.Command) bool {
	for _, p := range repo.Projects {
		if p.Dir == dir && path.Base(p.Manifest) == "pom.xml" {
			return true
		}
	}
	for _, c := range cmds {
		if path.Base(c.Source.Path) == "pom.xml" {
			return true
		}
	}
	return false
}

// soleFallbackDir returns the single primary non-root project directory that
// may supply commands missing at the repo root.
func soleFallbackDir(repo *model.Repo) (string, bool) {
	members := map[string]bool{}
	for _, p := range repo.Projects {
		if p.Workspace != "" {
			members[p.Dir] = true
		}
	}
	var cands []string
	for d := range primaryDirs(repo) {
		if d != "." && !members[d] {
			cands = append(cands, d)
		}
	}
	if len(cands) == 1 {
		return cands[0], true
	}
	return "", false
}

// restrictsDeps reports whether an install command skips some dependencies.
func restrictsDeps(run string) bool {
	f := strings.Fields(run)
	for i, w := range f {
		switch {
		case w == "--no-dev" || w == "--only" || strings.HasPrefix(w, "--only=") || w == "--production" || w == "--prod" ||
			w == "--omit=dev" || w == "--no-default-groups" || w == "--without" || strings.HasPrefix(w, "--without=") ||
			w == "--no-group" || w == "--production=true":
			return true
		case w == "--omit" && i+1 < len(f) && f[i+1] == "dev":
			return true
		case (w == "--group" || w == "--with") && i+1 < len(f) && f[i+1] != "dev":
			return true
		case strings.HasPrefix(w, "--group=") && w != "--group=dev":
			return true
		}
	}
	return false
}

func commandLabel(c model.Command, dir string) string {
	var parts []string
	ev := evidenceScore(c.Evidence)
	if ev == 3 {
		return toolWord(c.Run) + " convention, unverified"
	}
	if ev <= 1 {
		p := c.Source.Path
		switch {
		case p == "":
			parts = append(parts, "declared")
		case isCIPath(p):
		default:
			if dir != "." && strings.HasPrefix(p, dir+"/") {
				p = p[len(dir)+1:]
			}
			if validPath(p) {
				parts = append(parts, p)
			} else {
				parts = append(parts, "declared")
			}
		}
	}
	if ev == 0 || ev == 2 {
		parts = append(parts, "CI")
	}
	if len(parts) == 0 {
		parts = append(parts, "declared")
	}
	return strings.Join(parts, ", ")
}

var pmWords = set("npm pnpm yarn bun uv poetry pdm python python3 npx bunx")

var pmSubcommands = set("install i ci sync add remove update upgrade test build run exec dlx lock publish pack init check fmt lint format start dev")

func set(words string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(words) {
		m[w] = true
	}
	return m
}

// toolWord names the tool behind a command for "convention" labels.
func toolWord(run string) string {
	f := strings.Fields(run)
	if len(f) == 0 {
		return "tool"
	}
	if !pmWords[f[0]] {
		return ident(f[0])
	}
	j := 1
	skipped := f[0] == "npx" || f[0] == "bunx"
	for j < len(f) && (f[j] == "run" || f[j] == "exec" || f[j] == "dlx" || f[j] == "x" || f[j] == "-m" || f[j] == "--" || strings.HasPrefix(f[j], "-")) {
		skipped = true
		j++
	}
	if j < len(f) && (skipped || !pmSubcommands[f[j]]) {
		return ident(f[j])
	}
	return ident(f[0])
}

func safeNote(n string) string {
	n = strings.TrimSpace(n)
	if n == "" || len(n) > 48 {
		return ""
	}
	for _, r := range n {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune(" ,.:_/<>-", r)) {
			return ""
		}
	}
	return n
}

// ---- projects --------------------------------------------------------------

func rootEcosystems(repo *model.Repo, pdir string) []ProjectRef {
	seen := map[string]bool{}
	var out []ProjectRef
	for _, p := range repo.Projects {
		if p.Dir != pdir {
			continue
		}
		if seen[p.Ecosystem] {
			continue
		}
		seen[p.Ecosystem] = true
		out = append(out, ProjectRef{Dir: p.Dir, Name: ident(p.Name), Ecosystem: p.Ecosystem, Manifest: ident(p.Manifest)})
	}
	if len(out) < 2 {
		return []ProjectRef{}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ecosystem < out[j].Ecosystem })
	return out
}

func buildWorkspace(repo *model.Repo, pdir string) *Workspace {
	var members []model.Project
	isWS := false
	seenDirs := map[string]bool{}
	for _, p := range repo.Projects {
		if p.Dir == pdir {
			if p.IsWorkspaceRoot {
				isWS = true
			}
			continue
		}
		if pdir != "." && !within(p.Dir, pdir) {
			continue
		}
		if p.Workspace != "" {
			isWS = true
		} else if inSampleDir(p.Dir) {
			continue
		}
		if seenDirs[p.Dir] {
			continue
		}
		seenDirs[p.Dir] = true
		members = append(members, p)
	}
	if len(members) == 0 {
		return nil
	}
	sort.Slice(members, func(i, j int) bool { return members[i].Dir < members[j].Dir })
	// de-duplicate directories that host several ecosystems: keep first
	w := &Workspace{Kind: "projects"}
	if isWS {
		w.Kind = "workspace"
		for _, pm := range repo.PackageManagers {
			if pm.Dir == pdir && pm.Ecosystem != "" {
				if w.Manager == "" || pm.Ecosystem == "node" {
					w.Manager = ident(pm.Name)
				}
			}
		}
		if w.Manager == "" {
			for _, p := range repo.Projects {
				if p.Dir == pdir && p.IsWorkspaceRoot {
					w.Manager = p.Ecosystem
				}
			}
		}
	}
	if w.Manager != "" {
		for _, pm := range repo.PackageManagers {
			if pm.Dir == pdir && pm.Name == w.Manager {
				for _, m := range members {
					if m.Ecosystem != pm.Ecosystem {
						w.Manager = ""
					}
				}
			}
		}
	}
	w.Count = len(members)
	perEco := map[string]int{}
	for _, p := range repo.Projects {
		if p.Dir != pdir && within(p.Dir, pdir) {
			perEco[p.Ecosystem]++
			if perEco[p.Ecosystem] >= detectorMemberCap {
				w.Capped = true
			}
		}
	}
	for i, p := range members {
		if i >= MaxListed {
			break
		}
		w.Members = append(w.Members, ProjectRef{Dir: ident(p.Dir), Name: ident(p.Name), Ecosystem: p.Ecosystem})
	}
	w.Omitted = w.Count - len(w.Members)
	return w
}

// detectorMemberCap is the per-ecosystem member limit the detectors stop at;
// reaching it means the real count is unknown ("300+").
const detectorMemberCap = 300

var sampleSegments = set("example examples _examples testdata fixtures fixture samples sample")

func inSampleDir(dir string) bool {
	for _, seg := range strings.Split(strings.ToLower(dir), "/") {
		if sampleSegments[seg] {
			return true
		}
	}
	return false
}

// ---- do-not-edit -----------------------------------------------------------

func buildDoNotEdit(repo *model.Repo, pdir string) []DoNotEdit {
	var items []DoNotEdit
	var locks []string
	for _, g := range repo.Generated {
		pat := ident(g.Pattern)
		if pat == "" {
			continue
		}
		if g.Reason == "lockfile" {
			locks = append(locks, pat)
			continue
		}
		items = append(items, DoNotEdit{Pattern: pat, Reason: ident(g.Reason)})
	}
	sort.SliceStable(items, func(i, j int) bool {
		ai, aj := within(items[i].Pattern, pdir), within(items[j].Pattern, pdir)
		if ai != aj {
			return ai
		}
		di, dj := strings.Count(items[i].Pattern, "/"), strings.Count(items[j].Pattern, "/")
		if di != dj {
			return di < dj
		}
		return items[i].Pattern < items[j].Pattern
	})
	reserve := 0
	if len(locks) > 0 {
		reserve = 1
	}
	if len(items) > MaxDoNotEdit-reserve {
		items = items[:MaxDoNotEdit-reserve]
	}
	if len(locks) > 0 {
		sort.SliceStable(locks, func(i, j int) bool {
			di, dj := strings.Count(locks[i], "/"), strings.Count(locks[j], "/")
			if di != dj {
				return di < dj
			}
			return locks[i] < locks[j]
		})
		if len(locks) > 3 {
			locks = locks[:3]
		}
		items = append(items, DoNotEdit{Pattern: locks[0], Reason: "lockfile", Patterns: locks})
	}
	if items == nil {
		return []DoNotEdit{}
	}
	return items
}

// ---- instruction files and notes ------------------------------------------

func buildInstructionList(repo *model.Repo, pdir string) []string {
	var files []string
	for _, f := range repo.Instructions {
		if f.Path == "AGENTS.md" {
			continue
		}
		// Only files whose scope contains pdir, or lies inside it.
		if !(within(pdir, f.Scope) || within(f.Scope, pdir)) {
			continue
		}
		files = append(files, f.Path)
	}
	sort.Strings(files)
	// Collapse directories holding 3+ files.
	count := map[string]int{}
	for _, f := range files {
		count[path.Dir(f)]++
	}
	var out []string
	done := map[string]bool{}
	for _, f := range files {
		d := path.Dir(f)
		if d != "." && count[d] >= 3 {
			if !done[d] {
				done[d] = true
				out = append(out, ident(d)+"/")
			}
			continue
		}
		out = append(out, ident(f))
	}
	if out == nil {
		return []string{}
	}
	return out
}

func buildNotes(repo *model.Repo) []string {
	var out []string
	for _, n := range repo.Notes {
		t := oneLine(redact.String(n.Text))
		if t == "" || len(t) > MaxCommandLen {
			continue
		}
		out = append(out, t)
		if len(out) >= MaxNotes {
			break
		}
	}
	if out == nil {
		return []string{}
	}
	return out
}

// ---- helpers ---------------------------------------------------------------

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// ident makes a repository-derived identifier safe for output: single line,
// no backticks, no control characters, at most 80 bytes.
func ident(s string) string {
	s = oneLine(s)
	s = strings.Map(func(r rune) rune {
		if r == '`' || r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) > 80 {
		s = s[:80]
	}
	return redact.String(s)
}
