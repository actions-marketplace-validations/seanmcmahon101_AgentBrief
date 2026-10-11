package detect

import (
	"path"
	"sort"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

// ManagedStartMarker marks the beginning of the AgentBrief managed block.
// (Duplicated in internal/instructions; detect must not import it.)
const ManagedStartMarker = "<!-- agentbrief:start -->"

func init() { register("agentfiles", detectAgentFiles) }

func detectAgentFiles(r *scan.Repo, f *Facts) {
	f.Instructions = append(f.Instructions, InstructionFiles(r)...)
}

// InstructionFiles finds every agent-instruction file in r, sorted by path.
func InstructionFiles(r *scan.Repo) []model.InstructionFile {
	var out []model.InstructionFile
	for _, p := range r.Files() {
		kind, scope, ok := classifyInstruction(p)
		if !ok {
			continue
		}
		fi := model.InstructionFile{Path: p, Kind: kind, Scope: scope}
		if b, err := r.Read(p); err == nil {
			text := string(b)
			fi.Lines = countLines(text)
			fi.Managed = strings.Contains(text, ManagedStartMarker)
			if kind == "claude-md" || kind == "gemini-md" {
				fi.Imports = resolveImports(r, path.Dir(p), ExtractImports(text))
			}
		}
		out = append(out, fi)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// scopeAbove returns the first segIdx path segments of p joined, or ".".
func scopeAbove(p string, segIdx int) string {
	parts := strings.Split(p, "/")
	if segIdx <= 0 {
		return "."
	}
	return strings.Join(parts[:segIdx], "/")
}

func hasExt(name string, exts ...string) bool {
	for _, e := range exts {
		if strings.HasSuffix(name, e) {
			return true
		}
	}
	return false
}

// classifyInstruction maps a repo-relative path to (kind, scope).
func classifyInstruction(p string) (kind, scope string, ok bool) {
	parts := strings.Split(p, "/")
	base := parts[len(parts)-1]
	dir := path.Dir(p)
	// index of the directory-segment that holds a tool config dir.
	findSeg := func(names ...string) int {
		for i := 0; i < len(parts)-1; i++ {
			for _, n := range names {
				if parts[i] == n {
					return i
				}
			}
		}
		return -1
	}
	switch base {
	case "AGENTS.md", "AGENTS.override.md":
		if dir == ".junie" {
			return "junie", ".", true
		}
		return "agents-md", dir, true
	case "CLAUDE.md":
		if dir == ".claude" {
			return "claude-md", ".", true
		}
		if strings.HasSuffix(dir, "/.claude") {
			return "claude-md", path.Dir(dir), true
		}
		return "claude-md", dir, true
	case "CLAUDE.local.md":
		return "claude-md", dir, true
	case "GEMINI.md":
		return "gemini-md", dir, true
	case ".cursorrules":
		return "cursor-rules", dir, true
	case ".windsurfrules":
		return "windsurf", dir, true
	case "CONVENTIONS.md":
		return "aider", dir, true
	}
	if p == ".github/copilot-instructions.md" {
		return "copilot", ".", true
	}
	if dir == ".github/instructions" && strings.HasSuffix(base, ".instructions.md") {
		return "copilot", ".", true
	}
	if i := findSeg(".claude"); i >= 0 && parts[i+1] == "rules" && strings.HasSuffix(base, ".md") {
		return "claude-md", scopeAbove(p, i), true
	}
	if i := findSeg(".cursor"); i >= 0 && parts[i+1] == "rules" && hasExt(base, ".mdc", ".md") {
		return "cursor-rules", scopeAbove(p, i), true
	}
	if i := findSeg(".windsurf"); i >= 0 && parts[i+1] == "rules" && len(parts) == i+3 && strings.HasSuffix(base, ".md") {
		return "windsurf", scopeAbove(p, i), true
	}
	if i := findSeg(".devin"); i >= 0 && parts[i+1] == "rules" && len(parts) == i+3 && strings.HasSuffix(base, ".md") {
		return "other", scopeAbove(p, i), true
	}
	if i := findSeg(".kiro"); i >= 0 && parts[i+1] == "steering" && len(parts) == i+3 && strings.HasSuffix(base, ".md") {
		return "other", scopeAbove(p, i), true
	}
	if i := findSeg(".junie"); i >= 0 && len(parts) == i+2 && base == "guidelines.md" {
		return "junie", scopeAbove(p, i), true
	}
	// .clinerules as a file or a directory
	for i, seg := range parts {
		if seg == ".clinerules" {
			if i == len(parts)-1 || strings.HasSuffix(base, ".md") || strings.HasSuffix(base, ".txt") {
				return "cline", scopeAbove(p, i), true
			}
		}
	}
	return "", "", false
}

// ExtractImports returns the raw `@path` import tokens in Claude/Gemini style
// memory files, ignoring code spans and fenced code blocks.
func ExtractImports(text string) []string {
	var out []string
	seen := map[string]bool{}
	var fence string
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if fence != "" {
			if strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
				fence = ""
			}
			continue
		}
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			c := t[:1]
			n := len(t) - len(strings.TrimLeft(t, c))
			fence = strings.Repeat(c, n)
			continue
		}
		line = stripCodeSpans(line)
		for i := 0; i < len(line); i++ {
			if line[i] != '@' || (i > 0 && line[i-1] != ' ' && line[i-1] != '\t' && line[i-1] != '(' && line[i-1] != '-') {
				continue
			}
			j := i + 1
			for j < len(line) && line[j] != ' ' && line[j] != '\t' {
				j++
			}
			tok := strings.TrimRight(line[i+1:j], ".,;:!?)\"'")
			if tok != "" && (strings.Contains(tok, "/") || strings.Contains(tok, ".")) && !seen[tok] {
				seen[tok] = true
				out = append(out, tok)
			}
			i = j
		}
	}
	return out
}

// stripCodeSpans blanks out inline code spans.
func stripCodeSpans(line string) string {
	var b strings.Builder
	for i := 0; i < len(line); {
		if line[i] != '`' {
			b.WriteByte(line[i])
			i++
			continue
		}
		n := 0
		for i+n < len(line) && line[i+n] == '`' {
			n++
		}
		closing := strings.Index(line[i+n:], strings.Repeat("`", n))
		if closing < 0 {
			b.WriteString(line[i : i+n])
			i += n
			continue
		}
		b.WriteByte(' ')
		i += n + closing + n
	}
	return b.String()
}

func resolveImports(r *scan.Repo, dir string, toks []string) []string {
	var out []string
	for _, t := range toks {
		if strings.HasPrefix(t, "~") || strings.HasPrefix(t, "/") {
			continue
		}
		p := path.Clean(path.Join(dir, t))
		if strings.HasPrefix(p, "..") {
			continue
		}
		if r.Has(p) {
			out = append(out, p)
		}
	}
	return out
}
