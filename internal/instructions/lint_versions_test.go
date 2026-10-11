package instructions

import (
	"testing"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
)

func versionRepo() model.Repo {
	return model.Repo{Toolchains: []model.Toolchain{
		{Name: "node", Version: "20", Source: model.Source{Path: ".nvmrc"}},
		{Name: "python", Version: "3.12", Source: model.Source{Path: ".python-version"}},
		{Name: "go", Version: ">=1.22", Source: model.Source{Path: "go.mod"}},             // range only: never judged
		{Name: "python", Version: ">=3.10", Source: model.Source{Path: "pyproject.toml"}}, // range beside an exact pin: ignored
		{Name: "ruby", Version: "3.3.0", Source: model.Source{Path: ".ruby-version"}},
		{Name: "ruby", Version: "2.7.8", Source: model.Source{Path: "Gemfile"}},               // conflicting majors: never judged
		{Name: "java", Version: "17", Source: model.Source{Path: ".github/workflows/ci.yml"}}, // CI pin: ignored
	}}
}

func TestStaleVersion(t *testing.T) {
	doc := "This project requires Node 16.\n" + // stale
		"Use Python 3.10 for development.\n" + // stale
		"Use Go 1.18 and Ruby 2.7 and Java 11.\n" + // go range, ruby conflict, java CI-only: silent
		"We dropped Node 14 support last year. Tested on Python 3.9 in the matrix.\n" + // history: silent
		"Node 18+ also works. HTTPX requires Python 3.9+.\n" + // floors: silent
		"Don't use Python 3.8.\n" + // negation: silent
		"Python 3 is fine. Node.js 20 and Python 3.12.1 are expected.\n" + // agree: silent
		"```bash\nnvm use 14 # Node 14\n```\n" + // fence: silent
		"<!-- agentbrief:start -->\nNode 99\n<!-- agentbrief:end -->\n" // managed block: silent
	f := checkDoc(t, versionRepo(), map[string]string{"AGENTS.md": doc})
	if n := count(f, RuleStaleVersion); n != 2 {
		t.Fatalf("want 2 stale-version findings, got %d:\n%s", n, lines(f))
	}
	if !hasSev(f, RuleStaleVersion, model.SeverityWarning, "says Node 16, but `.nvmrc` pins 20") ||
		!hasSev(f, RuleStaleVersion, model.SeverityWarning, "says Python 3.10, but `.python-version` pins 3.12") {
		t.Errorf("%s", lines(f))
	}
}

func TestStaleVersionNoPins(t *testing.T) {
	f := checkDoc(t, model.Repo{}, map[string]string{"AGENTS.md": "Requires Node 16.\n"})
	if count(f, RuleStaleVersion) != 0 {
		t.Errorf("%s", lines(f))
	}
}
