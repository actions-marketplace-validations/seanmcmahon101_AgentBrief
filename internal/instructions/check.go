package instructions

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/brief"
	"github.com/seanmcmahon101/AgentBrief/internal/detect"
	"github.com/seanmcmahon101/AgentBrief/internal/model"
)

// Rule identifiers.
const (
	RuleStaleBlock        = "stale-block"
	RuleUnknownScript     = "unknown-script"
	RuleMissingPath       = "missing-path"
	RuleWrongPM           = "wrong-package-manager"
	RuleAgentsNotLoaded   = "agents-md-not-loaded"
	RuleOversized         = "oversized"
	maxInstructionBytes   = 32 * 1024
	maxClaudeMdLines      = 200
	maxReadInstructionLen = 4 << 20
)

// CheckOptions is reserved for future switches.
type CheckOptions struct{}

type checker struct {
	root string
	repo *model.Repo
	idx  *pathIndex
	tix  *taskIndex
	seen map[string]bool
	out  []model.Finding
	// textCache caches file reads for manifest lookups.
	textCache map[string]string
	// cargoText caches the concatenated Cargo manifests (see cargoManifestText).
	cargoText *string
	// cwd is the directory set by a preceding `cd` in the current fence ("?" = unknown).
	cwd string
}

// Check lints the instruction files of the repository at root against repo.
func Check(root string, repo *model.Repo, opts CheckOptions) []model.Finding {
	c := &checker{root: root, repo: repo, seen: map[string]bool{}, textCache: map[string]string{}}
	c.tix = newTaskIndex(repo)
	c.idx = &pathIndex{root: root}

	files := append([]model.InstructionFile(nil), repo.Instructions...)
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })

	var texts = map[string]string{}
	for _, f := range files {
		text, ok := c.readFile(f.Path)
		if !ok {
			continue
		}
		texts[f.Path] = text
		c.checkOversized(f, text)
		c.lintMarkdown(f, text)
		c.checkVersions(f, text)
	}
	c.checkStale(files, texts)
	c.checkLoaded(files, texts)

	sort.SliceStable(c.out, func(i, j int) bool {
		a, b := c.out[i], c.out[j]
		if a.Path != b.Path {
			return a.Path < b.Path
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Message < b.Message
	})
	if c.out == nil {
		return []model.Finding{}
	}
	return c.out
}

func (c *checker) add(f model.Finding) {
	key := fmt.Sprintf("%s|%s|%d|%s", f.Rule, f.Path, f.Line, f.Message)
	if c.seen[key] {
		return
	}
	c.seen[key] = true
	c.out = append(c.out, f)
}

func (c *checker) abs(rel string) string { return filepath.Join(c.root, filepath.FromSlash(rel)) }

func (c *checker) readFile(rel string) (string, bool) {
	f, err := os.Open(c.abs(rel))
	if err != nil {
		return "", false
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxReadInstructionLen))
	if err != nil {
		return "", false
	}
	return string(b), true
}

// readCached reads a repo file for manifest text lookups (capped at 1 MiB).
func (c *checker) readCached(rel string) string {
	if s, ok := c.textCache[rel]; ok {
		return s
	}
	var s string
	if f, err := os.Open(c.abs(rel)); err == nil {
		b, _ := io.ReadAll(io.LimitReader(f, 1<<20))
		f.Close()
		s = string(b)
	}
	c.textCache[rel] = s
	return s
}

func (c *checker) exists(rel string) bool {
	_, err := os.Stat(c.abs(rel))
	return err == nil
}

// ---- oversized -------------------------------------------------------------

func (c *checker) checkOversized(f model.InstructionFile, text string) {
	if len(text) > maxInstructionBytes {
		c.add(model.Finding{Rule: RuleOversized, Severity: model.SeverityWarning, Path: f.Path, Line: 1,
			Message:    fmt.Sprintf("file is %.1f KiB; Codex stops reading instruction files at 32 KiB", float64(len(text))/1024),
			Suggestion: "move rarely-needed detail into docs and link to it"})
	}
	if path.Base(f.Path) == "CLAUDE.md" {
		lines := strings.Count(text, "\n")
		if !strings.HasSuffix(text, "\n") && text != "" {
			lines++
		}
		if lines > maxClaudeMdLines {
			c.add(model.Finding{Rule: RuleOversized, Severity: model.SeverityWarning, Path: f.Path, Line: 1,
				Message:    fmt.Sprintf("CLAUDE.md has %d lines; Anthropic recommends keeping it under %d", lines, maxClaudeMdLines),
				Suggestion: "split it with @imports or .claude/rules/"})
		}
	}
}

// ---- stale-block -----------------------------------------------------------

func (c *checker) checkStale(files []model.InstructionFile, texts map[string]string) {
	text, ok := texts["AGENTS.md"]
	if !ok {
		return
	}
	want := brief.Build(c.repo, ".").Markdown()
	have, err := BlockText(text)
	switch {
	case errors.Is(err, ErrNoBlock):
		return
	case err != nil:
		c.add(model.Finding{Rule: RuleStaleBlock, Severity: model.SeverityError, Path: "AGENTS.md", Line: 1,
			Message: "managed block is malformed: " + err.Error(), Suggestion: "fix the markers by hand, then run `agentbrief sync`"})
		return
	}
	if have != want {
		b, _ := FindBlock(text)
		c.add(model.Finding{Rule: RuleStaleBlock, Severity: model.SeverityError, Path: "AGENTS.md", Line: b.StartLine,
			Message:    "the AgentBrief block no longer matches the repository",
			Suggestion: "run `agentbrief sync` and commit the result"})
	}
}

// ---- agents-md-not-loaded --------------------------------------------------

func (c *checker) checkLoaded(files []model.InstructionFile, texts map[string]string) {
	if _, ok := texts["AGENTS.md"]; !ok {
		return
	}
	for _, g := range []struct {
		kind  string
		paths []string
		who   string
	}{
		{"claude-md", []string{"CLAUDE.md", ".claude/CLAUDE.md"}, "Claude Code"},
		{"gemini-md", []string{"GEMINI.md"}, "Gemini CLI"},
	} {
		var present []string
		loaded := false
		for _, p := range g.paths {
			t, ok := texts[p]
			if !ok {
				continue
			}
			present = append(present, p)
			if c.importsReach(p, t, "AGENTS.md", texts, 0) {
				loaded = true
			}
		}
		if len(present) == 0 || loaded {
			continue
		}
		p := present[0]
		c.add(model.Finding{Rule: RuleAgentsNotLoaded, Severity: model.SeverityWarning, Path: p, Line: 1,
			Message:    fmt.Sprintf("%s does not import AGENTS.md, so %s will not read it", path.Base(p), g.who),
			Suggestion: "add `@AGENTS.md` as the first line (`agentbrief sync` does this)"})
	}
}

// importsReach reports whether the file (transitively) imports target.
func (c *checker) importsReach(from, text, target string, texts map[string]string, depth int) bool {
	if depth > 4 {
		return false
	}
	dir := path.Dir(from)
	for _, tok := range detect.ExtractImports(text) {
		if strings.HasPrefix(tok, "~") || strings.HasPrefix(tok, "/") {
			continue
		}
		p := path.Clean(path.Join(dir, tok))
		if p == target {
			return true
		}
		t, ok := texts[p]
		if !ok {
			t, ok = c.readFile(p)
		}
		if ok && c.importsReach(p, t, target, texts, depth+1) {
			return true
		}
	}
	return false
}
