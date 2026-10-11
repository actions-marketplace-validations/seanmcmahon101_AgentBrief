package detect

import (
	"regexp"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { register("swift", detectSwift) }

var (
	swiftToolsRe = regexp.MustCompile(`(?m)^//\s*swift-tools-version\s*:?\s*([0-9][0-9.]*)`)
	swiftNameRe  = regexp.MustCompile(`Package\(\s*name:\s*"([^"]+)"`)
)

func detectSwift(r *scan.Repo, f *Facts) {
	if !r.Has("Package.swift") {
		return
	}
	content := r.ReadString("Package.swift")
	name := "root"
	if m := swiftNameRe.FindStringSubmatch(content); m != nil {
		name = m[1]
	}
	f.AddProject(model.Project{Dir: ".", Name: name, Ecosystem: "swift", Manifest: "Package.swift"})
	f.AddPackageManager(model.PackageManager{Name: "swiftpm", Ecosystem: "swift", Dir: ".", Source: at("Package.swift", 1)})
	if m := swiftToolsRe.FindStringSubmatch(content); m != nil {
		f.AddToolchain(model.Toolchain{Name: "swift", Version: ">=" + m[1], Source: at("Package.swift", lineNumber(content, m[0]))})
	}
	conv := cmdEvidence(model.EvidenceConvention)
	src := at("Package.swift", 1)
	add := func(kind, run, note string, ev []string, s model.Source) {
		f.AddCommand(model.Command{Kind: kind, Run: run, Dir: ".", Evidence: ev, Source: s, Note: note})
	}
	add(model.KindBuild, "swift build", "", conv, src)
	if r.IsDir("Tests") || regexp.MustCompile(`\.testTarget\(`).MatchString(content) {
		add(model.KindTest, "swift test", "", conv, src)
		add(model.KindTestOne, "swift test --filter <Target>.<Class>/<testMethod>", "replace the placeholders", conv, src)
	}
	if r.Has(".swiftlint.yml") {
		add(model.KindLint, "swiftlint", "", cmdEvidence(model.EvidenceDeclared), at(".swiftlint.yml", 1))
	}
}
