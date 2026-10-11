package detect

import (
	"path"
	"regexp"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { register("elixir", detectElixir) }

var (
	mixAppRe      = regexp.MustCompile(`app:\s*:(\w+)`)
	mixElixirRe   = regexp.MustCompile(`elixir:\s*"([^"]+)"`)
	mixAppsPathRe = regexp.MustCompile(`apps_path:\s*"([^"]+)"`)
	mixAliasKeyRe = regexp.MustCompile(`^\s*"?([\w.:\-]+)"?:\s*(.*?),?\s*$`)
	mixStringsRe  = regexp.MustCompile(`"([^"]*)"`)
	mixAliasFnRe  = regexp.MustCompile(`(?m)^\s*defp?\s+aliases\b`)
)

func detectElixir(r *scan.Repo, f *Facts) {
	var root string
	for _, p := range filesNamed(r, "mix.exs") {
		dir := dirOf(p)
		content := r.ReadString(p)
		name := path.Base(dir)
		if m := mixAppRe.FindStringSubmatch(content); m != nil {
			name = m[1]
		} else if dir == "." {
			name = "root"
		}
		proj := model.Project{Dir: dir, Name: name, Ecosystem: "elixir", Manifest: p}
		switch {
		case dir == ".":
			root = p
			proj.IsWorkspaceRoot = mixAppsPathRe.MatchString(content)
		case root != "" && strings.HasPrefix(dir, "apps/"):
			proj.Workspace = "."
		case depth(dir) > 2:
			continue
		}
		f.AddProject(proj)
		if dir == "." {
			addMixCommands(r, f, p, content)
		}
	}
}

func addMixCommands(r *scan.Repo, f *Facts, file, content string) {
	conv := cmdEvidence(model.EvidenceConvention)
	decl := cmdEvidence(model.EvidenceDeclared)
	src := at(file, 1)
	add := func(kind, run, note string, ev []string, s model.Source) {
		f.AddCommand(model.Command{Kind: kind, Run: run, Dir: ".", Evidence: ev, Source: s, Note: note})
	}
	pmSrc := src
	if r.Has("mix.lock") {
		pmSrc = at("mix.lock", 1)
	}
	f.AddPackageManager(model.PackageManager{Name: "mix", Ecosystem: "elixir", Dir: ".", Source: pmSrc})
	if m := mixElixirRe.FindStringSubmatch(content); m != nil {
		f.AddToolchain(model.Toolchain{Name: "elixir", Version: m[1], Source: at(file, lineNumber(content, m[0]))})
	}
	add(model.KindInstall, "mix deps.get", "", conv, src)
	add(model.KindTest, "mix test", "", conv, src)
	add(model.KindTestOne, "mix test <file>:<line>", "replace <file>:<line>, e.g. test/x_test.exs:42", conv, src)
	add(model.KindFormat, "mix format --check-formatted", "checks only; `mix format` rewrites", conv, src)
	if r.Has(".credo.exs") {
		add(model.KindLint, "mix credo", "", decl, at(".credo.exs", 1))
	} else if strings.Contains(content, ":credo") {
		add(model.KindLint, "mix credo", "", decl, at(file, lineNumber(content, ":credo")))
	}
	if strings.Contains(content, ":dialyxir") {
		add(model.KindTypecheck, "mix dialyzer", "", decl, at(file, lineNumber(content, ":dialyxir")))
	}
	addMixAliases(f, file, content)
}

func addMixAliases(f *Facts, file, content string) {
	loc := mixAliasFnRe.FindStringIndex(content)
	if loc == nil {
		return
	}
	lines := strings.Split(content, "\n")
	start := strings.Count(content[:loc[0]], "\n")
	for i := start + 1; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "end" && !strings.HasPrefix(line, "   ") {
			return // end of the aliases function (2-space indent in mix format)
		}
		m := mixAliasKeyRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name, rest := m[1], m[2]
		body := mixAliasBody(rest)
		src := at(file, i+1)
		f.AddTask(model.Task{Runner: "mix-alias", Name: name, Dir: ".", Body: body, Source: src})
		addRunnerCommand(f, name, "mix "+name, ".", src)
	}
}

// mixAliasBody renders `["a", "b"]` or `"a"` as shell-ish text. Anything that
// is not made only of string literals (fns, multi-line lists) yields "".
func mixAliasBody(rest string) string {
	rest = strings.TrimSpace(rest)
	if !(strings.HasPrefix(rest, `"`) || (strings.HasPrefix(rest, "[") && strings.HasSuffix(rest, "]"))) {
		return ""
	}
	stripped := mixStringsRe.ReplaceAllString(rest, "")
	if strings.Trim(stripped, "[], ") != "" {
		return ""
	}
	var cmds []string
	for _, m := range mixStringsRe.FindAllStringSubmatch(rest, -1) {
		cmds = append(cmds, "mix "+m[1])
	}
	return truncate(strings.Join(cmds, " && "), 120)
}
