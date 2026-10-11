package detect

import (
	"encoding/json"
	"path"
	"regexp"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { register("dotnet", detectDotnet) }

const maxDotnetProjects = 300

var (
	slnProjectRe  = regexp.MustCompile(`=\s*"[^"]*",\s*"([^"]+\.(?:cs|fs|vb)proj)"`)
	slnxProjectRe = regexp.MustCompile(`Path="([^"]+\.(?:cs|fs|vb)proj)"`)
	testSdkRe     = regexp.MustCompile(`(?i)<IsTestProject>\s*true|Include="(?:Microsoft\.NET\.Test\.Sdk|xunit[\w.]*|NUnit[\w.]*|MSTest[\w.]*)"`)
)

func detectDotnet(r *scan.Repo, f *Facts) {
	slns := dotnetSolutions(r)
	projFiles := filesWithExt(r, ".csproj", ".fsproj", ".vbproj")
	if len(slns) == 0 && len(projFiles) == 0 {
		return
	}
	if len(projFiles) > maxDotnetProjects {
		projFiles = projFiles[:maxDotnetProjects]
	}

	// Which projects does the primary solution own?
	wsDir := ""
	members := map[string]bool{}
	if len(slns) > 0 {
		wsDir = dirOf(slns[0])
		for _, p := range slnProjects(r, slns[0]) {
			members[p] = true
		}
	}
	hasTests := false
	for _, p := range projFiles {
		dir := dirOf(p)
		proj := model.Project{Dir: dir, Name: strings.TrimSuffix(path.Base(p), path.Ext(p)), Ecosystem: "dotnet", Manifest: p}
		if wsDir != "" && members[p] {
			proj.Workspace = wsDir
			proj.IsWorkspaceRoot = dir == wsDir
		}
		f.AddProject(proj)
		if testSdkRe.MatchString(r.ReadString(p)) {
			hasTests = true
		}
	}
	if wsDir != "" && !hasProjectAt(f, wsDir) {
		f.AddProject(model.Project{Dir: wsDir, Name: strings.TrimSuffix(path.Base(slns[0]), path.Ext(slns[0])), Ecosystem: "dotnet", Manifest: slns[0], IsWorkspaceRoot: true})
	}
	addDotnetGlobalJSON(r, f)
	addDotnetCommands(r, f, slns, projFiles, hasTests)
}

func hasProjectAt(f *Facts, dir string) bool {
	for _, p := range f.Projects {
		if p.Dir == dir && p.Ecosystem == "dotnet" {
			return true
		}
	}
	return false
}

func filesWithExt(r *scan.Repo, exts ...string) []string {
	var out []string
	for _, p := range r.Files() {
		if noisyPath(p) {
			continue
		}
		for _, e := range exts {
			if strings.HasSuffix(p, e) {
				out = append(out, p)
				break
			}
		}
	}
	sortByDepth(out)
	return out
}

// dotnetSolutions returns the solutions at the repo root, or failing that the
// shallowest ones (depth 1), since `dotnet` commands run where the .sln lives.
func dotnetSolutions(r *scan.Repo) []string {
	all := filesWithExt(r, ".sln", ".slnx")
	var out []string
	for _, p := range all {
		if depth(dirOf(p)) == 0 {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		for _, p := range all {
			if depth(dirOf(p)) == 1 {
				out = append(out, p)
			}
		}
	}
	return out
}

func slnProjects(r *scan.Repo, sln string) []string {
	content := r.ReadString(sln)
	re := slnProjectRe
	if strings.HasSuffix(sln, ".slnx") {
		re = slnxProjectRe
	}
	var out []string
	for _, m := range re.FindAllStringSubmatch(content, -1) {
		out = append(out, path.Clean(joinPath(dirOf(sln), strings.ReplaceAll(m[1], "\\", "/"))))
	}
	return out
}

func addDotnetGlobalJSON(r *scan.Repo, f *Facts) {
	for _, p := range filesNamed(r, "global.json") {
		if depth(dirOf(p)) > 1 {
			continue
		}
		content := r.ReadString(p)
		var g struct {
			SDK struct {
				Version string `json:"version"`
			} `json:"sdk"`
		}
		if json.Unmarshal([]byte(content), &g) == nil && g.SDK.Version != "" {
			f.AddToolchain(model.Toolchain{Name: "dotnet", Version: g.SDK.Version, Source: at(p, lineNumber(content, `"version"`))})
		}
	}
}

func addDotnetCommands(r *scan.Repo, f *Facts, slns, projFiles []string, hasTests bool) {
	type target struct{ dir, file string } // file is appended to commands when set
	var targets []target
	switch {
	case len(slns) == 1:
		dir := dirOf(slns[0])
		t := target{dir: dir}
		// `dotnet build` errors when a directory holds several project/solution files.
		if countBuildFiles(r, dir) > 1 {
			t.file = path.Base(slns[0])
		}
		targets = append(targets, t)
	case len(slns) > 1:
		for _, s := range slns {
			targets = append(targets, target{dir: dirOf(s), file: path.Base(s)})
		}
		f.AddNote(model.Note{Text: "multiple solution files; dotnet commands name the solution explicitly (" + strings.Join(dotnetSlnNames(slns), ", ") + ")", Source: at(slns[0], 1)})
	default:
		if len(projFiles) == 1 || (depth(dirOf(projFiles[0])) == 0 && countBuildFiles(r, ".") == 1) {
			targets = append(targets, target{dir: dirOf(projFiles[0])})
		}
	}
	for _, t := range targets {
		src := t.file
		if src == "" {
			src = firstBuildFile(r, t.dir)
		}
		srcFile := joinPath(t.dir, src)
		suffix := ""
		if t.file != "" {
			suffix = " " + t.file
		}
		conv := cmdEvidence(model.EvidenceConvention)
		add := func(kind, run, note string) {
			f.AddCommand(model.Command{Kind: kind, Run: run, Dir: t.dir, Evidence: conv, Source: at(srcFile, 1), Note: note})
		}
		add(model.KindInstall, "dotnet restore"+suffix, "")
		add(model.KindBuild, "dotnet build"+suffix, "")
		add(model.KindFormat, "dotnet format"+suffix+" --verify-no-changes", "")
		if hasTests {
			add(model.KindTest, "dotnet test"+suffix, "")
			add(model.KindTestOne, "dotnet test"+suffix+` --filter "FullyQualifiedName~Namespace.Class.Method"`, "replace Namespace.Class.Method")
		}
	}
	if len(targets) > 0 {
		f.AddPackageManager(model.PackageManager{Name: "nuget", Ecosystem: "dotnet", Dir: targets[0].dir, Source: at(joinPath(targets[0].dir, firstBuildFile(r, targets[0].dir)), 1)})
	}
}

func dotnetSlnNames(ps []string) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = path.Base(p)
	}
	return capList(out, 5)
}

var buildFileExts = []string{".sln", ".slnx", ".csproj", ".fsproj", ".vbproj"}

func dirBuildFiles(r *scan.Repo, dir string) []string {
	var out []string
	for _, p := range r.FilesUnder(dir) {
		if dirOf(p) != dir {
			continue
		}
		for _, e := range buildFileExts {
			if strings.HasSuffix(p, e) {
				out = append(out, path.Base(p))
			}
		}
	}
	return out
}

func countBuildFiles(r *scan.Repo, dir string) int { return len(dirBuildFiles(r, dir)) }

func firstBuildFile(r *scan.Repo, dir string) string {
	if fs := dirBuildFiles(r, dir); len(fs) > 0 {
		return fs[0]
	}
	return ""
}
