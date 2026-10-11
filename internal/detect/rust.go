package detect

import (
	"path"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { register("rust", detectRust) }

type cargoManifest struct {
	Package *struct {
		Name        string `toml:"name"`
		RustVersion any    `toml:"rust-version"`
	} `toml:"package"`
	Workspace *struct {
		Members []string `toml:"members"`
		Exclude []string `toml:"exclude"`
		Package struct {
			RustVersion string `toml:"rust-version"`
		} `toml:"package"`
	} `toml:"workspace"`
}

type cargoCrate struct {
	dir, file string
	m         cargoManifest
}

func detectRust(r *scan.Repo, f *Facts) {
	var crates []cargoCrate
	for _, p := range filesNamed(r, "Cargo.toml") {
		var c cargoCrate
		c.dir, c.file = dirOf(p), p
		if _, err := toml.Decode(r.ReadString(p), &c.m); err != nil {
			continue
		}
		if c.m.Package == nil && c.m.Workspace == nil {
			continue
		}
		crates = append(crates, c)
	}
	if len(crates) == 0 {
		return
	}
	memberOf := workspaceMembership(r, crates)
	seenRust := map[string]bool{}
	crateDirs := map[string]bool{}
	for _, c := range crates {
		crateDirs[c.dir] = true
	}
	for _, c := range crates {
		ws := memberOf[c.dir]
		isRoot := c.m.Workspace != nil
		name := path.Base(c.dir)
		if c.dir == "." {
			name = "root"
			if root := strings.ReplaceAll(r.Root(), "\\", "/"); root != "" {
				name = path.Base(root)
			}
		}
		if c.m.Package != nil && c.m.Package.Name != "" {
			name = c.m.Package.Name
		}
		f.AddProject(model.Project{Dir: c.dir, Name: name, Ecosystem: "rust", Manifest: c.file, Workspace: ws, IsWorkspaceRoot: isRoot})
		if ws != "" {
			continue // members share the workspace root's commands and toolchain
		}
		if hasCrateAncestor(crateDirs, c.dir) {
			continue // nested standalone manifests (fuzz/, test helpers, ...) are tooling, not entry points
		}
		addRustToolchain(r, f, c, seenRust)
		addCargoCommands(r, f, c)
	}
}

// workspaceMembership maps member dir -> workspace root dir, expanding glob
// members through scan and honouring `exclude`.
func workspaceMembership(r *scan.Repo, crates []cargoCrate) map[string]string {
	out := map[string]string{}
	have := map[string]bool{}
	for _, c := range crates {
		have[c.dir] = true
	}
	for _, c := range crates {
		if c.m.Workspace == nil {
			continue
		}
		excluded := map[string]bool{}
		for _, e := range c.m.Workspace.Exclude {
			for _, d := range expandMember(r, c.dir, e) {
				excluded[d] = true
			}
		}
		for _, mem := range c.m.Workspace.Members {
			for _, d := range expandMember(r, c.dir, mem) {
				if have[d] && !excluded[d] && d != c.dir {
					out[d] = c.dir
				}
			}
		}
	}
	return out
}

func expandMember(r *scan.Repo, root, pattern string) []string {
	pattern = strings.TrimSuffix(pattern, "/")
	p := path.Clean(joinPath(root, pattern))
	if strings.HasPrefix(p, "..") {
		return nil
	}
	if strings.ContainsAny(p, "*?[") {
		return r.GlobDirs(p)
	}
	return []string{p}
}

func addRustToolchain(r *scan.Repo, f *Facts, c cargoCrate, seen map[string]bool) {
	for _, name := range []string{"rust-toolchain.toml", "rust-toolchain"} {
		p := joinPath(c.dir, name)
		if !r.Has(p) {
			continue
		}
		content := r.ReadString(p)
		ch := toolchainChannel(content)
		if ch != "" && !seen[ch] {
			seen[ch] = true
			f.AddToolchain(model.Toolchain{Name: "rust", Version: ch, Source: at(p, lineNumber(content, ch))})
		}
		break
	}
	rv := ""
	if c.m.Package != nil {
		if s, ok := c.m.Package.RustVersion.(string); ok {
			rv = s
		}
	}
	if rv == "" && c.m.Workspace != nil {
		rv = c.m.Workspace.Package.RustVersion
	}
	if rv != "" && !seen["msrv"+rv] {
		seen["msrv"+rv] = true
		f.AddToolchain(model.Toolchain{Name: "rust", Version: ">=" + rv, Source: at(c.file, lineNumber(r.ReadString(c.file), "rust-version"))})
	}
}

// toolchainChannel handles both the TOML form and the legacy one-line form.
func toolchainChannel(content string) string {
	var t struct {
		Toolchain struct {
			Channel string `toml:"channel"`
		} `toml:"toolchain"`
	}
	if _, err := toml.Decode(content, &t); err == nil && t.Toolchain.Channel != "" {
		return t.Toolchain.Channel
	}
	s := strings.TrimSpace(content)
	if s != "" && !strings.ContainsAny(s, "\n[=") {
		return s
	}
	return ""
}

func addCargoCommands(r *scan.Repo, f *Facts, c cargoCrate) {
	ws := ""
	single := true
	if c.m.Workspace != nil {
		ws = " --workspace"
		single = false
	}
	conv := cmdEvidence(model.EvidenceConvention)
	src := at(c.file, 1)
	lock := joinPath(c.dir, "Cargo.lock")
	pmSrc := src
	if r.Has(lock) {
		pmSrc = at(lock, 1)
	}
	f.AddPackageManager(model.PackageManager{Name: "cargo", Ecosystem: "rust", Dir: c.dir, Source: pmSrc})

	add := func(kind, run, note string, ev []string, s model.Source) {
		f.AddCommand(model.Command{Kind: kind, Run: run, Dir: c.dir, Evidence: ev, Source: s, Note: note})
	}
	add(model.KindBuild, "cargo build"+ws, "", conv, src)
	add(model.KindTest, "cargo test"+ws, "", conv, src)
	if single {
		add(model.KindTestOne, "cargo test <test_name>", "replace <test_name> with a test name filter", conv, src)
	} else {
		add(model.KindTestOne, "cargo test -p <crate> <test_name>", "replace <crate> and <test_name>", conv, src)
	}
	add(model.KindLint, "cargo clippy --all-targets"+ws, "", conv, src)
	add(model.KindFormat, "cargo fmt --check", "", conv, src)

	if nx := firstExisting(r, c.dir, ".config/nextest.toml"); nx != "" {
		add(model.KindTest, "cargo nextest run", "", cmdEvidence(model.EvidenceDeclared), at(nx, 1))
		add(model.KindTestOne, "cargo nextest run <test_name>", "replace <test_name> with a test name filter", cmdEvidence(model.EvidenceDeclared), at(nx, 1))
	}
	addCargoAliases(r, f, c.dir)
}

func addCargoAliases(r *scan.Repo, f *Facts, dir string) {
	cfg := firstExisting(r, dir, ".cargo/config.toml", ".cargo/config")
	if cfg == "" {
		return
	}
	content := r.ReadString(cfg)
	var t struct {
		Alias map[string]any `toml:"alias"`
	}
	if _, err := toml.Decode(content, &t); err != nil {
		return
	}
	names := make([]string, 0, len(t.Alias))
	for n := range t.Alias {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		body := ""
		switch v := t.Alias[n].(type) {
		case string:
			body = v
		case []any:
			var parts []string
			for _, e := range v {
				if s, ok := e.(string); ok {
					parts = append(parts, s)
				}
			}
			body = strings.Join(parts, " ")
		}
		src := at(cfg, lineNumber(content, n))
		f.AddTask(model.Task{Runner: "cargo-alias", Name: n, Dir: dir, Body: truncate("cargo "+body, 120), Source: src})
		// Aliases named dev/run/setup are usually maintainer tooling, not a dev server.
		if k := KindForTaskName(n); k != model.KindDev && k != model.KindRun && k != model.KindInstall {
			addRunnerCommand(f, n, "cargo "+n, dir, src)
		}
	}
}

func hasCrateAncestor(crateDirs map[string]bool, dir string) bool {
	for d := dir; d != "."; {
		d = path.Dir(d)
		if crateDirs[d] {
			return true
		}
	}
	return false
}
