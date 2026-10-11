package instructions

import (
	"regexp"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
)

var shellLangs = set(`"" sh bash shell zsh console shell-session sh-session terminal fish`)

var negation = regexp.MustCompile(`(?i)\b(never|don'?t|do not|not|avoid|instead of|rather than|without|no longer|deprecated|forbidden)\b`)

// skipCtx matches wording that makes a command mention a negation or a
// hypothetical example rather than an instruction.
var skipCtx = regexp.MustCompile(`(?i)(\b(never|don['’]?t|do not|avoid|instead of|rather than|should not|shouldn['’]?t|must not|mustn['’]?t|hypothetical(ly)?|e\.g|i\.e|for example|for instance|such as|example|imagine|suppose)\b)`)

var sentenceEnd = regexp.MustCompile(`[.!?;]\s+`)

// skipPrefix reports whether text (the part of a line before a command) puts
// the command in a negated or hypothetical context. Only the current sentence
// is considered.
func skipPrefix(prefix string) bool {
	prefix = strings.NewReplacer("e.g.", "e.g", "i.e.", "i.e", "etc.", "etc").Replace(prefix)
	if locs := sentenceEnd.FindAllStringIndex(prefix, -1); len(locs) > 0 {
		prefix = prefix[locs[len(locs)-1][1]:]
	}
	return skipCtx.MatchString(prefix)
}

// spanSkipped reports whether the code span it sits in a negated/example sentence.
func spanSkipped(it mdItem) bool {
	idx := strings.Index(it.full, it.text)
	if idx < 0 {
		return skipPrefix(it.full)
	}
	return skipPrefix(it.full[:idx])
}

// lintMarkdown runs the per-file rules (commands, paths, package managers).
func (c *checker) lintMarkdown(f model.InstructionFile, text string) {
	wp := &wrongPM{c: c, f: f, reported: map[string]bool{}}
	prevFence := -10
	lastPlain := ""
	fenceSkip := false
	c.cwd = ""
	for _, it := range scanMarkdown(text) {
		if it.kind != itemFenceLine {
			c.cwd = ""
		}
		switch it.kind {
		case itemSpan:
			if !spanSkipped(it) {
				c.lintCommandText(f, it.line, it.text)
			}
			c.cwd = ""
			c.checkSpanPath(f, it.line, it.text)
			wp.scan(it)
		case itemFenceLine:
			if it.line != prevFence+1 {
				c.cwd = ""
				// First line of a fence: is the lead-in sentence a negation or example?
				t := strings.TrimSpace(lastPlain)
				fenceSkip = strings.HasSuffix(t, ":") && skipPrefix(t)
			}
			prevFence = it.line
			if !shellLangs[it.lang] && !(it.lang == "") {
				continue
			}
			t := strings.TrimSpace(it.text)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			if !fenceSkip {
				c.lintCommandText(f, it.line, t)
			}
			wp.scan(it)
		case itemPlain:
			if t := strings.TrimSpace(it.text); strings.HasPrefix(t, "$ ") {
				c.lintCommandText(f, it.line, t)
				c.cwd = ""
				wp.scan(mdItem{line: it.line, kind: itemSpan, text: t[2:], full: it.full})
			} else if t != "" {
				lastPlain = t
				c.checkProsePaths(f, it)
			}
		case itemLink:
			c.checkLink(f, it.line, it.text)
		}
	}
	c.cwd = ""
}
