package instructions

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
)

// RuleStaleVersion flags prose such as "requires Node 16" when the repository
// pins a different version (.nvmrc says 20). It is deliberately narrow:
//
//   - only Node, Python, Go, Ruby and Java, the runtimes people actually write
//     down in instruction files;
//   - only when the repository pins that runtime unambiguously (one version,
//     from a file, not a CI workflow);
//   - never in code fences, inside the managed block, or in sentences about
//     history or compatibility ("dropped Node 14", "still supports Python 3.8").
//
// It is a warning: prose is ambiguous, and a wrong error would erode trust.
const RuleStaleVersion = "stale-version"

var versionMention = regexp.MustCompile(`(?i)\b(node(?:\.js|js)?|python|go(?:lang)?|ruby|java|jdk)\s+v?(\d+(?:\.\d+){0,2})(\+|\.x)?`)

// historyCtx marks a sentence as talking about other versions on purpose.
var historyCtx = regexp.MustCompile(`(?i)\b(support|supports|supported|compat\w*|drop\w*|older|earlier|previous|legacy|deprecat\w*|upgrad\w*|migrat\w*|until|before|since|was|were|used to|no longer|end[- ]of[- ]life|eol|minimum|at least|or (newer|later|above|higher)|matrix|test(ed|s)? (on|against|with))\b`)

var versionToolAlias = map[string]string{
	"node": "node", "nodejs": "node", "node.js": "node",
	"python": "python", "go": "go", "golang": "go", "ruby": "ruby", "java": "java", "jdk": "java",
}

func (c *checker) checkVersions(f model.InstructionFile, text string) {
	pins := c.versionPins()
	if len(pins) == 0 {
		return
	}
	reported := map[string]bool{}
	inFence, inBlock := false, false
	for i, line := range strings.Split(text, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~"):
			inFence = !inFence
			continue
		case strings.Contains(t, StartMarker):
			inBlock = true
			continue
		case strings.Contains(t, EndMarker):
			inBlock = false
			continue
		}
		if inFence || inBlock {
			continue
		}
		for _, sentence := range splitSentences(line) {
			if historyCtx.MatchString(sentence) || skipPrefix(sentence) {
				continue
			}
			for _, m := range versionMention.FindAllStringSubmatch(sentence, -1) {
				tool := versionToolAlias[strings.ToLower(m[1])]
				pin, ok := pins[tool]
				if !ok || reported[tool] {
					continue
				}
				said := m[2]
				if m[3] == "+" {
					continue // "Node 18+" is a floor, not a pin
				}
				if versionsAgree(tool, said, pin.Version) {
					continue
				}
				reported[tool] = true
				c.add(model.Finding{
					Rule: RuleStaleVersion, Severity: model.SeverityWarning, Path: f.Path, Line: i + 1,
					Message:    fmt.Sprintf("says %s %s, but `%s` pins %s", displayTool(tool), said, pin.Source.Path, pin.Version),
					Suggestion: fmt.Sprintf("update the instruction to %s %s, or the pin if the instruction is right", displayTool(tool), pin.Version),
				})
			}
		}
	}
}

// versionPins returns, per runtime, the single unambiguous exact pin from a
// repository file. Ranges are ignored; conflicting exact pins disable the rule.
func (c *checker) versionPins() map[string]model.Toolchain {
	byTool := map[string][]model.Toolchain{}
	for _, t := range c.repo.Toolchains {
		if strings.HasPrefix(t.Source.Path, ".github/") || strings.HasPrefix(t.Source.Path, ".gitlab") {
			continue
		}
		if _, ok := versionToolAlias[t.Name]; !ok {
			continue
		}
		byTool[t.Name] = append(byTool[t.Name], t)
	}
	out := map[string]model.Toolchain{}
	for tool, ts := range byTool {
		// Ranges (requires-python ">=3.10", engines "^20") routinely sit next
		// to an exact pin and do not compete with it; only exact pins count,
		// and they must agree with each other.
		var exact []model.Toolchain
		for _, t := range ts {
			if exactVersion(t.Version) {
				exact = append(exact, t)
			}
		}
		agree := len(exact) > 0
		for _, t := range exact {
			if majorMinor(tool, t.Version) != majorMinor(tool, exact[0].Version) {
				agree = false
			}
		}
		if agree {
			out[tool] = exact[0]
		}
	}
	return out
}

var exactRe = regexp.MustCompile(`^v?\d+(\.\d+){0,2}$`)

func exactVersion(v string) bool { return exactRe.MatchString(strings.TrimSpace(v)) }

// majorMinor reduces a version to the precision people use in prose: the major
// for Node, Ruby and Java ("Node 20"), major.minor for Python and Go ("Python 3.12").
func majorMinor(tool, v string) string {
	parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".")
	switch tool {
	case "python", "go":
		if len(parts) >= 2 {
			return parts[0] + "." + parts[1]
		}
	}
	return parts[0]
}

// versionsAgree reports whether a version mentioned in prose is consistent with
// the pin. Prose may be less precise than the pin ("Python 3" vs 3.12).
func versionsAgree(tool, said, pin string) bool {
	s := strings.Split(said, ".")
	p := strings.Split(strings.TrimPrefix(pin, "v"), ".")
	n := len(strings.Split(majorMinor(tool, said), "."))
	if len(s) < n {
		n = len(s) // "Python 3" only states a major
	}
	for i := 0; i < n && i < len(p); i++ {
		a, err1 := strconv.Atoi(s[i])
		b, err2 := strconv.Atoi(p[i])
		if err1 != nil || err2 != nil || a != b {
			return false
		}
	}
	return true
}

func displayTool(t string) string {
	switch t {
	case "node":
		return "Node"
	case "python":
		return "Python"
	case "go":
		return "Go"
	case "ruby":
		return "Ruby"
	case "java":
		return "Java"
	}
	return t
}

func splitSentences(line string) []string {
	line = strings.NewReplacer("e.g.", "e.g", "i.e.", "i.e").Replace(line)
	var out []string
	start := 0
	for _, loc := range sentenceEnd.FindAllStringIndex(line, -1) {
		out = append(out, line[start:loc[1]])
		start = loc[1]
	}
	return append(out, line[start:])
}
