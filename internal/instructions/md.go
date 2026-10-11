package instructions

import (
	"regexp"
	"strings"
)

type itemKind int

const (
	itemSpan      itemKind = iota // inline code span
	itemFenceLine                 // a line inside a fenced code block
	itemPlain                     // a prose line (outside fences)
	itemLink                      // a Markdown link destination
)

type mdItem struct {
	line int
	kind itemKind
	text string // span content, fence line, plain line, or link destination
	lang string // fence info word, for itemFenceLine
	full string // the whole source line (for context checks)
}

var (
	linkRe = regexp.MustCompile(`!?\[[^\]\n]*\]\(\s*(<[^>\n]+>|[^)\s]+)(?:\s+(?:"[^"]*"|'[^']*'))?\s*\)`)
	refRe  = regexp.MustCompile(`^\s{0,3}\[[^\]\n]+\]:\s*(<[^>\n]+>|\S+)`)
)

// scanMarkdown splits text into items, skipping YAML front matter and the
// AgentBrief managed block (those are verified by other means).
func scanMarkdown(text string) []mdItem {
	text = normalizeEOL(text)
	lines := strings.Split(text, "\n")
	var items []mdItem
	start := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i := 1; i < len(lines); i++ {
			if strings.TrimSpace(lines[i]) == "---" {
				start = i + 1
				break
			}
		}
	}
	var fence, lang string
	inBlock := false
	for i := start; i < len(lines); i++ {
		ln := i + 1
		raw := lines[i]
		t := strings.TrimSpace(raw)
		if inBlock {
			if t == EndMarker {
				inBlock = false
			}
			continue
		}
		if fence != "" {
			if strings.HasPrefix(t, fence) && strings.Trim(t, fence[:1]) == "" {
				fence = ""
				continue
			}
			items = append(items, mdItem{line: ln, kind: itemFenceLine, text: raw, lang: lang, full: raw})
			continue
		}
		if t == StartMarker {
			inBlock = true
			continue
		}
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			c := t[:1]
			n := len(t) - len(strings.TrimLeft(t, c))
			fence = strings.Repeat(c, n)
			info := strings.Fields(strings.Trim(t[n:], " {}."))
			lang = ""
			if len(info) > 0 {
				lang = strings.ToLower(info[0])
			}
			continue
		}
		items = append(items, mdItem{line: ln, kind: itemPlain, text: raw, full: raw})
		stripped, spans := splitSpans(raw)
		for _, s := range spans {
			items = append(items, mdItem{line: ln, kind: itemSpan, text: s, full: raw})
		}
		for _, m := range linkRe.FindAllStringSubmatch(stripped, -1) {
			items = append(items, mdItem{line: ln, kind: itemLink, text: unangle(m[1]), full: raw})
		}
		if m := refRe.FindStringSubmatch(stripped); m != nil {
			items = append(items, mdItem{line: ln, kind: itemLink, text: unangle(m[1]), full: raw})
		}
	}
	return items
}

func unangle(s string) string {
	if strings.HasPrefix(s, "<") && strings.HasSuffix(s, ">") {
		return s[1 : len(s)-1]
	}
	return s
}

// splitSpans returns line with code spans blanked out, plus the span contents.
func splitSpans(line string) (string, []string) {
	var b strings.Builder
	var spans []string
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
		closing := indexRun(line[i+n:], n)
		if closing < 0 {
			b.WriteString(line[i : i+n])
			i += n
			continue
		}
		content := line[i+n : i+n+closing]
		if len(content) >= 2 && content[0] == ' ' && content[len(content)-1] == ' ' && strings.TrimSpace(content) != "" {
			content = content[1 : len(content)-1]
		}
		spans = append(spans, content)
		b.WriteString(strings.Repeat(" ", closing+2*n))
		i += n + closing + n
	}
	return b.String(), spans
}

// indexRun finds a backtick run of exactly n backticks in s.
func indexRun(s string, n int) int {
	for i := 0; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(s) && s[j] == '`' {
			j++
		}
		if j-i == n {
			return i
		}
		i = j
	}
	return -1
}
