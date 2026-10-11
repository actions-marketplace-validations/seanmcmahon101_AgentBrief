package detect

import (
	"path"
	"sort"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { register("languages", detectLanguages) }

// langByExt maps file extensions to a language. Documentation and data formats
// (md, json, yaml, txt, lock, ...) are deliberately absent: they say nothing
// about what toolchain a repository needs.
var langByExt = map[string]string{
	".go": "Go", ".rs": "Rust",
	".py": "Python", ".pyi": "Python", ".pyx": "Python",
	".js": "JavaScript", ".mjs": "JavaScript", ".cjs": "JavaScript", ".jsx": "JavaScript",
	".ts": "TypeScript", ".tsx": "TypeScript", ".mts": "TypeScript", ".cts": "TypeScript",
	".vue": "Vue", ".svelte": "Svelte", ".astro": "Astro",
	".java": "Java", ".kt": "Kotlin", ".kts": "Kotlin", ".scala": "Scala", ".groovy": "Groovy",
	".rb": "Ruby", ".php": "PHP",
	".cs": "C#", ".fs": "F#", ".vb": "Visual Basic",
	".swift": "Swift", ".m": "Objective-C", ".mm": "Objective-C",
	".c": "C", ".h": "C",
	".cc": "C++", ".cpp": "C++", ".cxx": "C++", ".hpp": "C++", ".hh": "C++", ".hxx": "C++",
	".ex": "Elixir", ".exs": "Elixir", ".erl": "Erlang", ".hs": "Haskell", ".clj": "Clojure",
	".lua": "Lua", ".dart": "Dart", ".zig": "Zig", ".nim": "Nim", ".jl": "Julia", ".r": "R",
	".ml": "OCaml", ".pl": "Perl", ".sh": "Shell", ".bash": "Shell", ".zsh": "Shell",
	".ps1": "PowerShell", ".sol": "Solidity",
}

// maxLanguages bounds the output; a brief only needs the dominant ones.
const maxLanguages = 8

// vendoredDirs hold code that is generated, copied in or built, so it is not
// counted as the project's own and agents should not be pointed at it.
var vendoredDirs = map[string]bool{
	"vendor": true, "vendored": true, "third_party": true, "third-party": true, "thirdparty": true,
	"dist": true, "build": true, "generated": true, "__generated__": true, "site-packages": true,
	".yarn": true, "node_modules": true, "external": true, "bower_components": true, "out": true,
}

// fixtureDirs hold test inputs and demo apps: files there are not the
// project's own manifests, lockfiles or generated outputs.
var fixtureDirs = map[string]bool{
	"__tests__": true, "__fixtures__": true, "fixtures": true, "fixture": true, "testdata": true,
	"playground": true, "playgrounds": true, "__snapshots__": true,
}

// hasFixtureDir reports whether any directory of p is a fixture dir.
func hasFixtureDir(p string) bool {
	for _, seg := range strings.Split(path.Dir(p), "/") {
		if fixtureDirs[seg] {
			return true
		}
	}
	return false
}

// demoDirs hold samples and benchmarks; their lockfiles are noise in a
// do-not-edit list (but their packages may still be workspace members).
var demoDirs = setOf("examples", "example", "samples", "sample", "benchmarks", "benchmark", "bench", "demo", "demos")

func hasDemoDir(p string) bool {
	for _, seg := range strings.Split(path.Dir(p), "/") {
		if demoDirs[seg] {
			return true
		}
	}
	return false
}

// isVendoredPath reports whether any directory of p is a vendored/output dir.
func isVendoredPath(p string) bool {
	dir := path.Dir(p)
	if dir == "." {
		return false
	}
	for _, seg := range strings.Split(dir, "/") {
		if vendoredDirs[seg] {
			return true
		}
	}
	return false
}

func detectLanguages(r *scan.Repo, f *Facts) {
	counts := map[string]int{}
	total := 0
	for _, p := range r.Files() {
		lang, ok := langByExt[strings.ToLower(path.Ext(p))]
		if !ok || isVendoredPath(p) {
			continue
		}
		counts[lang]++
		total++
	}
	for lang, n := range counts {
		// Drop stragglers below 2% so a single helper script does not
		// appear next to the real languages.
		if n*50 < total {
			continue
		}
		f.Languages = append(f.Languages, model.Language{Name: lang, Files: n})
	}
	// normalize() sorts later, but truncation needs the order now.
	sort.Slice(f.Languages, func(i, j int) bool {
		a, b := f.Languages[i], f.Languages[j]
		if a.Files != b.Files {
			return a.Files > b.Files
		}
		return a.Name < b.Name
	})
	if len(f.Languages) > maxLanguages {
		f.Languages = f.Languages[:maxLanguages]
	}
}
