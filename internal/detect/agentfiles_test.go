package detect

import (
	"reflect"
	"testing"

	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func TestInstructionFiles(t *testing.T) {
	r := scan.FromMap(map[string]string{
		"AGENTS.md":                              "x\n<!-- agentbrief:start -->\n<!-- agentbrief:end -->\n",
		"CLAUDE.md":                              "@AGENTS.md\n\nsee `@nope.md`\n```\n@fenced.md\n```\n",
		"packages/a/CLAUDE.md":                   "hi",
		".claude/rules/style.md":                 "r",
		"GEMINI.md":                              "@./AGENTS.md\n",
		".cursorrules":                           "c",
		".cursor/rules/a.mdc":                    "c",
		"web/.cursor/rules/b.mdc":                "c",
		".github/copilot-instructions.md":        "c",
		".github/instructions/x.instructions.md": "c",
		".clinerules/a.md":                       "c",
		".junie/guidelines.md":                   "c",
		"CONVENTIONS.md":                         "c",
		".kiro/steering/a.md":                    "c",
		"README.md":                              "no",
		"docs/AGENTS.md":                         "no",
	})
	got := map[string]string{}
	for _, f := range InstructionFiles(r) {
		got[f.Path] = f.Kind + "|" + f.Scope
	}
	want := map[string]string{
		"AGENTS.md": "agents-md|.", "CLAUDE.md": "claude-md|.", "packages/a/CLAUDE.md": "claude-md|packages/a",
		".claude/rules/style.md": "claude-md|.", "GEMINI.md": "gemini-md|.", ".cursorrules": "cursor-rules|.",
		".cursor/rules/a.mdc": "cursor-rules|.", "web/.cursor/rules/b.mdc": "cursor-rules|web",
		".github/copilot-instructions.md": "copilot|.", ".github/instructions/x.instructions.md": "copilot|.",
		".clinerules/a.md": "cline|.", ".junie/guidelines.md": "junie|.", "CONVENTIONS.md": "aider|.",
		".kiro/steering/a.md": "other|.", "docs/AGENTS.md": "agents-md|docs",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
	for _, f := range InstructionFiles(r) {
		if f.Path == "CLAUDE.md" && !reflect.DeepEqual(f.Imports, []string{"AGENTS.md"}) {
			t.Errorf("imports = %v", f.Imports)
		}
		if f.Path == "AGENTS.md" && (!f.Managed || f.Lines != 3) {
			t.Errorf("agents: %+v", f)
		}
		if f.Path == "GEMINI.md" && len(f.Imports) != 1 {
			t.Errorf("gemini imports %v", f.Imports)
		}
	}
}
