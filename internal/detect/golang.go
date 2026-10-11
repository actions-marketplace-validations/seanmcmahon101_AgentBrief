package detect

import (
	"bytes"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { register("go", detectGo) }

// maxGoFilesRead bounds the //go:generate scan on huge repositories.
const maxGoFilesRead = 2000

type goModule struct {
	dir, file, path string
	goVersion       string
	goLine          int
	toolchain       string
	toolchainLine   int
}

func detectGo(r *scan.Repo, f *Facts) {
	mods := parseGoModules(r)
	if len(mods) == 0 {
		return
	}
	workDir, members := parseGoWork(r)
	seenVersion := map[string]bool{}

	for _, m := range mods {
		p := model.Project{Dir: m.dir, Name: m.path, Ecosystem: "go", Manifest: m.file}
		if p.Name == "" {
			p.Name = path.Base(m.dir)
		}
		if workDir != "" && members[m.dir] {
			p.Workspace = workDir
			p.IsWorkspaceRoot = m.dir == workDir
		}
		f.AddProject(p)
		if isExampleDir(m.dir) {
			continue // example modules get a Project only
		}
		addGoToolchains(f, m, seenVersion)
		f.AddPackageManager(model.PackageManager{Name: "go", Ecosystem: "go", Dir: m.dir, Source: at(m.file, 1)})
	}
	if workDir != "" && !hasModuleAt(mods, workDir) {
		// The workspace root is not itself a module: record it so members can point at it.
		name := "workspace"
		if workDir != "." {
			name = path.Base(workDir)
		}
		f.AddProject(model.Project{Dir: workDir, Name: name, Ecosystem: "go", Manifest: joinPath(workDir, "go.work"), IsWorkspaceRoot: true})
	}
	if workDir != "" {
		if v := goWorkVersion(r.ReadString(joinPath(workDir, "go.work"))); v != "" {
			addGoToolchains(f, goModule{file: joinPath(workDir, "go.work"), goVersion: v, goLine: lineNumber(r.ReadString(joinPath(workDir, "go.work")), "\ngo ") + 1}, seenVersion)
		}
	}

	gen := firstGoGenerate(r, mods)
	for _, m := range mods {
		if isExampleDir(m.dir) {
			continue // modules under examples/ get a Project but no commands
		}
		addGoCommands(r, f, m, gen[m.dir])
	}
}

func hasModuleAt(mods []goModule, dir string) bool {
	for _, m := range mods {
		if m.dir == dir {
			return true
		}
	}
	return false
}

func isExampleDir(dir string) bool {
	for _, seg := range strings.Split(dir, "/") {
		if seg == "examples" || seg == "_examples" || seg == "example" {
			return true
		}
	}
	return false
}

func parseGoModules(r *scan.Repo) []goModule {
	var mods []goModule
	for _, p := range filesNamed(r, "go.mod") {
		m := goModule{dir: dirOf(p), file: p}
		parseGoMod(r.ReadString(p), &m)
		mods = append(mods, m)
	}
	return mods
}

// parseGoMod reads the module, go and toolchain directives. A hand parser is
// enough: these are single-line directives and x/mod is not a dependency.
func parseGoMod(content string, m *goModule) {
	for i, line := range strings.Split(content, "\n") {
		if c := strings.Index(line, "//"); c >= 0 {
			line = line[:c]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "module":
			if m.path == "" {
				m.path = strings.Trim(fields[1], `"`)
			}
		case "go":
			m.goVersion, m.goLine = fields[1], i+1
		case "toolchain":
			m.toolchain, m.toolchainLine = strings.TrimPrefix(fields[1], "go"), i+1
		}
	}
}

func goWorkVersion(content string) string {
	var m goModule
	parseGoMod(content, &m)
	return m.goVersion
}

func addGoToolchains(f *Facts, m goModule, seen map[string]bool) {
	if m.goVersion != "" && !seen["go"+m.goVersion] {
		seen["go"+m.goVersion] = true
		f.AddToolchain(model.Toolchain{Name: "go", Version: m.goVersion, Source: at(m.file, m.goLine)})
	}
	if m.toolchain != "" && m.toolchain != "default" && !seen["go"+m.toolchain] {
		seen["go"+m.toolchain] = true
		f.AddToolchain(model.Toolchain{Name: "go", Version: m.toolchain, Source: at(m.file, m.toolchainLine)})
	}
}

var goWorkUseRe = regexp.MustCompile(`(?m)^\s*use\s*(?:\(([^)]*)\)|(\S+))`)

// parseGoWork returns the go.work directory and the set of module dirs it uses.
func parseGoWork(r *scan.Repo) (string, map[string]bool) {
	files := filesNamed(r, "go.work")
	if len(files) == 0 {
		return "", nil
	}
	wf := files[0]
	dir := dirOf(wf)
	members := map[string]bool{}
	content := stripGoComments(r.ReadString(wf))
	for _, m := range goWorkUseRe.FindAllStringSubmatch(content, -1) {
		list := m[2]
		if m[1] != "" {
			list = m[1]
		}
		for _, d := range strings.Fields(list) {
			d = strings.Trim(d, `"`+"`")
			if p := path.Clean(joinPath(dir, d)); !strings.HasPrefix(p, "..") {
				members[p] = true
			}
		}
	}
	return dir, members
}

func stripGoComments(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if c := strings.Index(l, "//"); c >= 0 {
			lines[i] = l[:c]
		}
	}
	return strings.Join(lines, "\n")
}

type goGenerate struct {
	file string
	line int
}

// firstGoGenerate finds, per module, the first //go:generate directive. It
// reads at most maxGoFilesRead non-test-data .go files.
func firstGoGenerate(r *scan.Repo, mods []goModule) map[string]goGenerate {
	out := map[string]goGenerate{}
	read := 0
	for _, p := range r.Files() {
		if !strings.HasSuffix(p, ".go") || noisyPath(p) {
			continue
		}
		if read++; read > maxGoFilesRead {
			break
		}
		owner := owningModule(mods, p)
		if owner == "" {
			continue
		}
		if _, done := out[owner]; done {
			continue
		}
		b, err := r.Read(p)
		if err != nil {
			continue
		}
		if line := generateLine(b); line > 0 {
			out[owner] = goGenerate{p, line}
		}
	}
	return out
}

// generateLine returns the line of a column-0 //go:generate directive, or 0.
func generateLine(b []byte) int {
	const d = "//go:generate "
	if bytes.HasPrefix(b, []byte(d)) {
		return 1
	}
	i := bytes.Index(b, []byte("\n"+d))
	if i < 0 {
		return 0
	}
	return bytes.Count(b[:i+1], []byte("\n")) + 1
}

// owningModule returns the dir of the deepest module containing file p.
func owningModule(mods []goModule, p string) string {
	best := ""
	for _, m := range mods {
		if m.dir == "." || strings.HasPrefix(p, m.dir+"/") {
			if best == "" || len(m.dir) > len(best) || best == "." {
				best = m.dir
			}
		}
	}
	return best
}

var golangciConfigs = []string{".golangci.yml", ".golangci.yaml", ".golangci.toml", ".golangci.json"}

// golangciConfig finds the config golangci-lint would use: in dir or an ancestor.
func golangciConfig(r *scan.Repo, dir string) string {
	for d := dir; ; d = path.Dir(d) {
		if p := firstExisting(r, d, golangciConfigs...); p != "" {
			return p
		}
		if d == "." || d == "/" {
			return ""
		}
	}
}

func addGoCommands(r *scan.Repo, f *Facts, m goModule, gen goGenerate) {
	conv := cmdEvidence(model.EvidenceConvention)
	src := at(m.file, 1)
	add := func(kind, run, note string, ev []string, s model.Source) {
		f.AddCommand(model.Command{Kind: kind, Run: run, Dir: m.dir, Evidence: ev, Source: s, Note: note})
	}
	add(model.KindBuild, "go build ./...", "", conv, src)
	add(model.KindTest, "go test ./...", "", conv, src)
	add(model.KindTestOne, "go test ./<pkg> -run '^TestName$'", "replace <pkg> and TestName", conv, src)
	add(model.KindFormat, "gofmt -l .", "lists files needing formatting", conv, src)
	if cfg := golangciConfig(r, m.dir); cfg != "" {
		add(model.KindLint, "golangci-lint run", "", cmdEvidence(model.EvidenceDeclared), at(cfg, 1))
	} else {
		add(model.KindLint, "go vet ./...", "", conv, src)
	}
	if gen.file != "" {
		add(model.KindGenerate, "go generate ./...", "", conv, at(gen.file, gen.line))
	}
	if vm := joinPath(m.dir, "vendor/modules.txt"); r.Has(vm) {
		f.AddGenerated(model.PathRule{Pattern: joinPath(m.dir, "vendor/**"), Reason: "vendored", Source: at(vm, 1)})
		f.AddNote(model.Note{Text: "Go dependencies are vendored; builds use -mod=vendor", Source: at(vm, 1)})
	}
	addGoRunCommands(r, add, m)
}

// addGoRunCommands emits `go run ./cmd/<name>` when there are few enough
// binaries that listing them is useful.
func addGoRunCommands(r *scan.Repo, add func(kind, run, note string, ev []string, s model.Source), m goModule) {
	var mains []string
	for _, name := range r.ChildDirs(joinPath(m.dir, "cmd")) {
		if p := joinPath(m.dir, "cmd/"+name+"/main.go"); r.Has(p) {
			mains = append(mains, name)
		}
	}
	sort.Strings(mains)
	if len(mains) == 0 || len(mains) > 3 {
		return
	}
	for _, name := range mains {
		add(model.KindRun, "go run ./cmd/"+name, "", cmdEvidence(model.EvidenceConvention), at(joinPath(m.dir, "cmd/"+name+"/main.go"), 1))
	}
}
