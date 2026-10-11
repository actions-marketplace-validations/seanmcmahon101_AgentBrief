package instructions

import (
	"regexp"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
)

var shellLangs = set(`"" sh bash shell zsh console shell-session sh-session terminal fish`)

var negation = regexp.MustCompile(`(?i)\b(never|don'?t|do not|not|avoid|instead of|rather than|without|no longer|deprecated|forbidden)\b`)

// lintMarkdown runs the per-file rules (commands, paths, package managers).
func (c *checker) lintMarkdown(f model.InstructionFile, text string) {
	wp := &wrongPM{c: c, f: f, reported: map[string]bool{}}
	for _, it := range scanMarkdown(text) {
		switch it.kind {
		case itemSpan:
			c.lintCommandText(f, it.line, it.text)
			c.checkSpanPath(f, it.line, it.text)
			wp.scan(it)
		case itemFenceLine:
			if !shellLangs[it.lang] && !(it.lang == "") {
				continue
			}
			t := strings.TrimSpace(it.text)
			if t == "" || strings.HasPrefix(t, "#") {
				continue
			}
			c.lintCommandText(f, it.line, t)
			wp.scan(it)
		case itemPlain:
			if t := strings.TrimSpace(it.text); strings.HasPrefix(t, "$ ") {
				c.lintCommandText(f, it.line, t)
				wp.scan(mdItem{line: it.line, kind: itemSpan, text: t[2:], full: it.full})
			}
		case itemLink:
			c.checkLink(f, it.line, it.text)
		}
	}
}
