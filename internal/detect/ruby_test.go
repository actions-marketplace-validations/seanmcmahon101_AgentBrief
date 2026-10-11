package detect

import "testing"

func TestRubyRspecProject(t *testing.T) {
	files := map[string]string{
		"Gemfile":        "source 'https://rubygems.org'\nruby '3.2.2'\ngem 'rspec'\ngem 'rubocop'\n",
		"Gemfile.lock":   "GEM\n\nBUNDLED WITH\n   2.4.10\n",
		".ruby-version":  "ruby-3.3.0\n",
		".rspec":         "--color\n",
		"spec/a_spec.rb": "",
		".rubocop.yml":   "",
		"foo.gemspec":    "",
	}
	f := detectWith(files, detectRuby)
	needCmds(t, f, ".|install|bundle install", ".|test|bundle exec rspec", ".|test-single|bundle exec rspec <file>:<line>", ".|lint|bundle exec rubocop")
	forbidCmds(t, f, ".|test|bundle exec rake test")
	if !hasToolchain(f, "ruby", "3.3.0") || !hasToolchain(f, "ruby", "3.2.2") {
		t.Errorf("toolchains: %+v", f.Toolchains)
	}
	if len(f.PackageManagers) != 1 || f.PackageManagers[0].Name != "bundler" || f.PackageManagers[0].Version != "2.4.10" {
		t.Errorf("pm: %+v", f.PackageManagers)
	}
	if len(f.Projects) != 1 || f.Projects[0].Name != "foo" {
		t.Errorf("projects: %+v", f.Projects)
	}
}

func TestRubyRails(t *testing.T) {
	files := map[string]string{
		"Gemfile":               "gem 'rails'\n",
		"bin/rails":             "",
		"config/application.rb": "",
		"db/schema.rb":          "",
		"test/models/a_test.rb": "",
		"lib/tasks/db.rake":     "namespace :db do\n  desc \"Seed it\"\n  task :seed do\n  end\n  namespace :x do\n    task :y\n  end\nend\ntask :after\n",
	}
	f := detectWith(files, detectRuby)
	needCmds(t, f, ".|test|bin/rails test", ".|test-single|bin/rails test <file>:<line>", ".|dev|bin/rails server")
	forbidCmds(t, f, ".|lint|bundle exec rubocop")
	for _, n := range []string{"db:seed", "db:x:y", "after"} {
		if findTask(f, "rake", n) == nil {
			t.Errorf("missing rake task %s: %s", n, taskNames(f, "rake"))
		}
	}
	if tk := findTask(f, "rake", "db:seed"); tk.Desc != "Seed it" || tk.Source.Path != "lib/tasks/db.rake" || tk.Source.Line != 3 {
		t.Errorf("seed: %+v", tk)
	}
	if len(f.Generated) != 1 || f.Generated[0].Pattern != "db/schema.rb" {
		t.Errorf("generated: %+v", f.Generated)
	}
}

func TestRubyRakefileForms(t *testing.T) {
	rakefile := `require 'rake/testtask'
desc "Run tests"
Rake::TestTask.new do |t|
end
task default: :test
task :lint => [:a, :b] do
end
task 'docs:build' do
end
task(:fmt) { sh 'x' }
task gem => %w[a b]
# task :commented
`
	files := map[string]string{"Gemfile": "source 'x'", "Rakefile": rakefile, "test/a_test.rb": ""}
	f := detectWith(files, detectRuby)
	for _, n := range []string{"test", "default", "lint", "docs:build", "fmt"} {
		if findTask(f, "rake", n) == nil {
			t.Errorf("missing %s: %s", n, taskNames(f, "rake"))
		}
	}
	for _, n := range []string{"gem", "commented"} {
		if findTask(f, "rake", n) != nil {
			t.Errorf("unexpected %s", n)
		}
	}
	needCmds(t, f, ".|test|bundle exec rake test", ".|lint|bundle exec rake lint", ".|format|bundle exec rake fmt")
	if c := cmdKeys(f)[".|test|bundle exec rake test"]; c.Evidence[0] != "declared" {
		t.Errorf("evidence %v", c.Evidence)
	}
}

func TestRubyNoiseAndNoGemfile(t *testing.T) {
	files := map[string]string{"spec/fixtures/app/Gemfile": "", "test/dummy/Gemfile": "", "Rakefile": "task :test do\nend\n"}
	f := detectWith(files, detectRuby)
	if len(f.Projects) != 0 || len(f.PackageManagers) != 0 {
		t.Errorf("fixtures are not projects: %+v", f.Projects)
	}
	needCmds(t, f, ".|test|rake test")
}
