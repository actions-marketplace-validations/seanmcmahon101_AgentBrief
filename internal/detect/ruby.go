package detect

import (
	"path"
	"regexp"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func init() { register("ruby", detectRuby) }

// rubyNoise are directories whose Gemfiles are test fixtures or CI matrices.
var rubyNoise = map[string]bool{"test": true, "spec": true, "gemfiles": true, "examples": true, "docs": true, "doc": true, "features": true}

var (
	gemfileRubyRe   = regexp.MustCompile(`(?m)^\s*ruby\s+["']([^"']+)["']`)
	bundledWithRe   = regexp.MustCompile(`(?m)^BUNDLED WITH\s*\n\s+(\S+)`)
	rakeTaskRe      = regexp.MustCompile(`^\s*task\s*\(?\s*(?:(?::["']?|["'])([A-Za-z_][\w.\-]*(?::[\w.\-]+)*)|([A-Za-z_][\w.\-]*):(?:[^:]|$))`)
	rakeNamespaceRe = regexp.MustCompile(`^(\s*)namespace\s*\(?\s*:?["']?([\w.\-]+)["']?\)?\s+do\b`)
	rakeTestTaskRe  = regexp.MustCompile(`(?:Rake::TestTask\.new|RSpec::Core::RakeTask\.new|Minitest::TestTask\.(?:new|create))(?:\(\s*:?["']?([\w:\-]+))?`)
	rakeDescRe      = regexp.MustCompile(`^\s*desc\s+["'](.*?)["']`)
)

func detectRuby(r *scan.Repo, f *Facts) {
	dirs := map[string]bool{}
	for _, p := range filesNamed(r, "Gemfile", "Rakefile", "rakefile", "Rakefile.rb") {
		d := dirOf(p)
		if depth(d) > 2 || rubyNoisy(d) {
			continue
		}
		dirs[d] = true
	}
	for _, d := range sortedKeys(dirs) {
		detectRubyDir(r, f, d)
	}
}

func rubyNoisy(dir string) bool {
	for _, seg := range strings.Split(dir, "/") {
		if rubyNoise[seg] {
			return true
		}
	}
	return false
}

func detectRubyDir(r *scan.Repo, f *Facts, dir string) {
	gemfile := joinPath(dir, "Gemfile")
	hasGemfile := r.Has(gemfile)
	conv := cmdEvidence(model.EvidenceConvention)
	decl := cmdEvidence(model.EvidenceDeclared)
	add := func(kind, run, note string, ev []string, s model.Source) {
		f.AddCommand(model.Command{Kind: kind, Run: run, Dir: dir, Evidence: ev, Source: s, Note: note})
	}

	bundle := ""
	if hasGemfile {
		bundle = "bundle exec "
		gemContent := r.ReadString(gemfile)
		name := path.Base(dir)
		manifest := gemfile
		if gs := r.Glob(joinPath(dir, "*.gemspec")); len(gs) > 0 {
			name = strings.TrimSuffix(path.Base(gs[0]), ".gemspec")
		}
		if dir == "." && r.Root() != "" && name == "." {
			name = path.Base(strings.ReplaceAll(r.Root(), "\\", "/"))
		}
		f.AddProject(model.Project{Dir: dir, Name: name, Ecosystem: "ruby", Manifest: manifest})
		addRubyToolchains(r, f, dir, gemfile, gemContent)
		pm := model.PackageManager{Name: "bundler", Ecosystem: "ruby", Dir: dir, Source: at(gemfile, 1)}
		if lock := joinPath(dir, "Gemfile.lock"); r.Has(lock) {
			pm.Source = at(lock, 1)
			if m := bundledWithRe.FindStringSubmatch(r.ReadString(lock)); m != nil {
				pm.Version = m[1]
			}
		}
		f.AddPackageManager(pm)
		add(model.KindInstall, "bundle install", "", conv, at(gemfile, 1))
	}

	// tools listed in the bundle (Gemfile, lockfile or gemspec)
	bundleText := ""
	if hasGemfile {
		bundleText = r.ReadString(gemfile) + r.ReadString(joinPath(dir, "Gemfile.lock"))
		for _, gs := range r.Glob(joinPath(dir, "*.gemspec")) {
			bundleText += r.ReadString(gs)
		}
	}
	rails := r.Has(joinPath(dir, "bin/rails"))
	rspec := r.IsDir(joinPath(dir, "spec")) && (r.Has(joinPath(dir, ".rspec")) || strings.Contains(bundleText, "rspec"))
	tasks := rubyRakeTasks(r, dir)
	for _, t := range tasks {
		f.AddTask(t)
	}

	switch {
	case rspec:
		add(model.KindTest, bundle+"rspec", "", conv, at(rubyTestSource(r, dir, ".rspec", gemfile), 1))
		add(model.KindTestOne, bundle+"rspec <file>:<line>", "replace <file>:<line>", conv, at(rubyTestSource(r, dir, ".rspec", gemfile), 1))
	case rails:
		add(model.KindTest, "bin/rails test", "", conv, at(joinPath(dir, "bin/rails"), 1))
		add(model.KindTestOne, "bin/rails test <file>:<line>", "replace <file>:<line>", conv, at(joinPath(dir, "bin/rails"), 1))
	case r.IsDir(joinPath(dir, "test")) || hasRakeTest(tasks):
		add(model.KindTest, bundle+"rake test", "", conv, at(rubyRakefile(r, dir, gemfile), 1))
		add(model.KindTestOne, bundle+"ruby -Itest <file>", "replace <file>; runs one minitest file", conv, at(rubyRakefile(r, dir, gemfile), 1))
	}
	if rails && r.Has(joinPath(dir, "config/application.rb")) {
		add(model.KindDev, "bin/rails server", "", conv, at(joinPath(dir, "bin/rails"), 1))
	}
	if cfg := joinPath(dir, ".rubocop.yml"); r.Has(cfg) {
		cmd := "rubocop"
		if strings.Contains(bundleText, "rubocop") {
			cmd = bundle + "rubocop"
		}
		add(model.KindLint, cmd, "", decl, at(cfg, 1))
	}
	for _, t := range tasks {
		run := "rake " + t.Name
		if hasGemfile {
			run = "bundle exec rake " + t.Name
		}
		addRunnerCommand(f, t.Name, run, dir, t.Source)
	}
	for _, g := range []string{"db/schema.rb", "db/structure.sql"} {
		if p := joinPath(dir, g); r.Has(p) && rails {
			f.AddGenerated(model.PathRule{Pattern: p, Reason: "generated", Source: at(p, 1)})
		}
	}
}

func rubyTestSource(r *scan.Repo, dir, preferred, fallback string) string {
	if p := joinPath(dir, preferred); r.Has(p) {
		return p
	}
	return fallback
}

func rubyRakefile(r *scan.Repo, dir, fallback string) string {
	if p := firstExisting(r, dir, "Rakefile", "rakefile", "Rakefile.rb"); p != "" {
		return p
	}
	return fallback
}

func hasRakeTest(tasks []model.Task) bool {
	for _, t := range tasks {
		if t.Name == "test" {
			return true
		}
	}
	return false
}

func addRubyToolchains(r *scan.Repo, f *Facts, dir, gemfile, gemContent string) {
	if p := joinPath(dir, ".ruby-version"); r.Has(p) {
		v := strings.TrimPrefix(strings.TrimSpace(firstLineOf(r.ReadString(p))), "ruby-")
		if v != "" {
			f.AddToolchain(model.Toolchain{Name: "ruby", Version: v, Source: at(p, 1)})
		}
	}
	if m := gemfileRubyRe.FindStringSubmatch(gemContent); m != nil {
		f.AddToolchain(model.Toolchain{Name: "ruby", Version: m[1], Source: at(gemfile, lineNumber(gemContent, m[0]))})
	}
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// rubyRakeTasks collects tasks from the Rakefile and lib/tasks/*.rake. Rake is
// Ruby, so this is a line-based regex pass; it never evaluates anything.
func rubyRakeTasks(r *scan.Repo, dir string) []model.Task {
	var files []string
	if p := firstExisting(r, dir, "Rakefile", "rakefile", "Rakefile.rb"); p != "" {
		files = append(files, p)
	}
	files = append(files, r.Glob(joinPath(dir, "lib/tasks/**/*.rake"))...)
	if len(files) > 40 {
		files = files[:40]
	}
	var out []model.Task
	seen := map[string]bool{}
	for _, p := range files {
		for _, t := range parseRake(r.ReadString(p), p, dir) {
			if !seen[t.Name] {
				seen[t.Name] = true
				out = append(out, t)
			}
		}
	}
	return out
}

type rakeNS struct {
	indent string
	name   string
}

func parseRake(content, file, dir string) []model.Task {
	var out []model.Task
	var stack []rakeNS
	desc := ""
	prefix := func() string {
		var parts []string
		for _, n := range stack {
			parts = append(parts, n.name)
		}
		return strings.Join(parts, ":")
	}
	qualify := func(n string) string {
		if p := prefix(); p != "" {
			return p + ":" + n
		}
		return n
	}
	for i, line := range strings.Split(content, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "#") {
			continue
		}
		if m := rakeNamespaceRe.FindStringSubmatch(line); m != nil {
			stack = append(stack, rakeNS{m[1], m[2]})
			continue
		}
		if trim == "end" && len(stack) > 0 && strings.TrimSuffix(line, "end") == stack[len(stack)-1].indent {
			stack = stack[:len(stack)-1]
			continue
		}
		if m := rakeDescRe.FindStringSubmatch(line); m != nil {
			desc = m[1]
			continue
		}
		if m := rakeTaskRe.FindStringSubmatch(line); m != nil {
			n := m[1]
			if n == "" {
				n = m[2]
			}
			out = append(out, model.Task{Runner: "rake", Name: qualify(n), Dir: dir, Desc: truncate(desc, 120), Source: at(file, i+1)})
			desc = ""
			continue
		}
		if m := rakeTestTaskRe.FindStringSubmatch(line); m != nil {
			name := m[1]
			if name == "" {
				name = "test"
			}
			out = append(out, model.Task{Runner: "rake", Name: qualify(name), Dir: dir, Desc: truncate(desc, 120), Source: at(file, i+1)})
			desc = ""
		}
	}
	return out
}
