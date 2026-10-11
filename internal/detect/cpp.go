package detect

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { register("cpp", detectCpp) }

const maxPresetsPerKind = 4

var (
	cmakeProjectRe = regexp.MustCompile(`(?im)^\s*project\s*\(\s*([A-Za-z0-9_.\-]+)`)
	cmakeMinRe     = regexp.MustCompile(`(?im)^\s*cmake_minimum_required\s*\(\s*VERSION\s+([0-9][0-9.]*)`)
	cmakeTestRe    = regexp.MustCompile(`(?im)^\s*(?:enable_testing\s*\(|add_test\s*\(|include\s*\(\s*CTest|gtest_discover_tests|catch_discover_tests)`)
	mesonProjectRe = regexp.MustCompile(`(?m)^\s*project\(\s*'([^']+)'`)
	mesonTestRe    = regexp.MustCompile(`(?m)^\s*test\(`)
)

func detectCpp(r *scan.Repo, f *Facts) {
	detectCMake(r, f)
	detectMeson(r, f)
	detectBazel(r, f)
}

type cmakePreset struct {
	Name   string `json:"name"`
	Hidden bool   `json:"hidden"`
}

func detectCMake(r *scan.Repo, f *Facts) {
	if !r.Has("CMakeLists.txt") {
		return
	}
	content := r.ReadString("CMakeLists.txt")
	name := "root"
	if m := cmakeProjectRe.FindStringSubmatch(content); m != nil {
		name = m[1]
	}
	f.AddProject(model.Project{Dir: ".", Name: name, Ecosystem: "c-cpp", Manifest: "CMakeLists.txt"})
	if m := cmakeMinRe.FindStringSubmatch(content); m != nil {
		f.AddToolchain(model.Toolchain{Name: "cmake", Version: ">=" + m[1], Source: at("CMakeLists.txt", lineNumber(content, m[0]))})
	}
	src := at("CMakeLists.txt", 1)
	if r.Has("CMakePresets.json") && addCMakePresets(r, f) {
		return
	}
	conv := cmdEvidence(model.EvidenceConvention)
	add := func(kind, run, note string) {
		f.AddCommand(model.Command{Kind: kind, Run: run, Dir: ".", Evidence: conv, Source: src, Note: note})
	}
	add(model.KindConfigure, "cmake -S . -B build", "")
	add(model.KindBuild, "cmake --build build", "")
	if cmakeUsesTests(r, content) {
		add(model.KindTest, "ctest --test-dir build", "")
	}
}

// cmakeUsesTests looks for test registration in the root and the first few
// other CMakeLists.txt files (tests usually live in a subdirectory).
func cmakeUsesTests(r *scan.Repo, rootContent string) bool {
	if cmakeTestRe.MatchString(rootContent) {
		return true
	}
	n := 0
	for _, p := range filesNamed(r, "CMakeLists.txt") {
		if p == "CMakeLists.txt" {
			continue
		}
		if n++; n > 200 {
			break
		}
		if cmakeTestRe.MatchString(r.ReadString(p)) {
			return true
		}
	}
	return false
}

// addCMakePresets reports whether any usable preset was found.
func addCMakePresets(r *scan.Repo, f *Facts) bool {
	content := r.ReadString("CMakePresets.json")
	var doc struct {
		Configure []cmakePreset `json:"configurePresets"`
		Build     []cmakePreset `json:"buildPresets"`
		Test      []cmakePreset `json:"testPresets"`
	}
	if json.Unmarshal([]byte(content), &doc) != nil {
		return false
	}
	decl := cmdEvidence(model.EvidenceDeclared)
	found := false
	emit := func(presets []cmakePreset, kind, format, note string) {
		n := 0
		for _, p := range presets {
			if p.Hidden || p.Name == "" || strings.ContainsAny(p.Name, " \t'\"$") {
				continue
			}
			if n++; n > maxPresetsPerKind {
				return
			}
			found = true
			f.AddCommand(model.Command{Kind: kind, Run: strings.ReplaceAll(format, "%s", p.Name), Dir: ".", Evidence: decl,
				Source: at("CMakePresets.json", lineNumber(content, `"`+p.Name+`"`)), Note: note})
		}
	}
	emit(doc.Configure, model.KindConfigure, "cmake --preset %s", "")
	emit(doc.Build, model.KindBuild, "cmake --build --preset %s", "")
	emit(doc.Test, model.KindTest, "ctest --preset %s", "")
	return found
}

func detectMeson(r *scan.Repo, f *Facts) {
	if !r.Has("meson.build") {
		return
	}
	content := r.ReadString("meson.build")
	name := "root"
	if m := mesonProjectRe.FindStringSubmatch(content); m != nil {
		name = m[1]
	}
	f.AddProject(model.Project{Dir: ".", Name: name, Ecosystem: "c-cpp", Manifest: "meson.build"})
	conv := cmdEvidence(model.EvidenceConvention)
	src := at("meson.build", 1)
	add := func(kind, run, note string) {
		f.AddCommand(model.Command{Kind: kind, Run: run, Dir: ".", Evidence: conv, Source: src, Note: note})
	}
	add(model.KindConfigure, "meson setup build", "")
	add(model.KindBuild, "meson compile -C build", "")
	if mesonHasTests(r, content) {
		add(model.KindTest, "meson test -C build", "")
		add(model.KindTestOne, "meson test -C build <test_name>", "replace <test_name>")
	}
}

func mesonHasTests(r *scan.Repo, root string) bool {
	if mesonTestRe.MatchString(root) {
		return true
	}
	n := 0
	for _, p := range filesNamed(r, "meson.build") {
		if p == "meson.build" {
			continue
		}
		if n++; n > 100 {
			break
		}
		if mesonTestRe.MatchString(r.ReadString(p)) {
			return true
		}
	}
	return false
}

func detectBazel(r *scan.Repo, f *Facts) {
	marker := firstExisting(r, ".", "MODULE.bazel", "WORKSPACE.bazel", "WORKSPACE", "WORKSPACE.bzlmod")
	if marker == "" {
		return
	}
	f.AddProject(model.Project{Dir: ".", Name: "bazel", Ecosystem: "bazel", Manifest: marker})
	if r.Has(".bazelversion") {
		v := strings.TrimSpace(firstLineOf(r.ReadString(".bazelversion")))
		if v != "" {
			f.AddToolchain(model.Toolchain{Name: "bazel", Version: v, Source: at(".bazelversion", 1)})
		}
	}
	conv := cmdEvidence(model.EvidenceConvention)
	src := at(marker, 1)
	f.AddCommand(model.Command{Kind: model.KindBuild, Run: "bazel build //...", Dir: ".", Evidence: conv, Source: src})
	f.AddCommand(model.Command{Kind: model.KindTest, Run: "bazel test //...", Dir: ".", Evidence: conv, Source: src})
}
