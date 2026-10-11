package scan

import (
	"path"
	"strings"
)

// This file is a small gitignore matcher. We cannot shell out to
// `git check-ignore`: running git inside an untrusted repository can execute
// hooks (core.fsmonitor), which the design forbids.

// ignoreRule is one parsed gitignore line.
type ignoreRule struct {
	segs     []string // pattern split on "/"; a single element when !anchored
	negate   bool
	dirOnly  bool // pattern ended in "/": matches directories only
	anchored bool // contained a "/" (other than trailing): match from the base dir
	literal  bool // single segment with no glob metacharacters: plain compare
}

// ignoreFile holds the rules of one .gitignore (or info/exclude) file.
// base is the slash-separated directory the file applies to ("" for the root).
type ignoreFile struct {
	base  string
	rules []ignoreRule
}

// parseIgnore parses gitignore text. base is the directory containing the file,
// relative to the repository root ("" or "." for the root).
func parseIgnore(base, text string) *ignoreFile {
	if base == "." {
		base = ""
	}
	f := &ignoreFile{base: base}
	for _, line := range strings.Split(text, "\n") {
		if r, ok := parseIgnoreLine(line); ok {
			f.rules = append(f.rules, r)
		}
	}
	return f
}

func parseIgnoreLine(line string) (ignoreRule, bool) {
	line = strings.TrimSuffix(line, "\r")
	line = trimTrailingSpace(line)
	if line == "" || line[0] == '#' {
		return ignoreRule{}, false
	}
	var r ignoreRule
	if line[0] == '!' {
		r.negate = true
		line = line[1:]
	}
	// "\#" and "\!" escape a leading special character; path.Match already
	// treats "\x" as a literal x, so nothing more to do for them.
	if strings.HasSuffix(line, "/") {
		r.dirOnly = true
		line = strings.TrimRight(line, "/")
	}
	if line == "" {
		return ignoreRule{}, false
	}
	if strings.Contains(line, "/") {
		r.anchored = true
		line = strings.TrimPrefix(line, "/")
	}
	r.segs = strings.Split(line, "/")
	for i, s := range r.segs {
		r.segs[i] = fixClass(s)
	}
	r.literal = !r.anchored && !strings.ContainsAny(r.segs[0], `*?[\`)
	return r, true
}

// trimTrailingSpace drops unescaped trailing spaces, as git does.
func trimTrailingSpace(s string) string {
	for len(s) > 0 && s[len(s)-1] == ' ' {
		if len(s) >= 2 && s[len(s)-2] == '\\' {
			return s
		}
		s = s[:len(s)-1]
	}
	return s
}

// fixClass converts gitignore's "[!x]" negated class to path.Match's "[^x]".
func fixClass(s string) string {
	if !strings.Contains(s, "[!") {
		return s
	}
	return strings.ReplaceAll(s, "[!", "[^")
}

// match reports whether rule r matches rel (relative to the file's base).
func (r *ignoreRule) match(rel string, isDir bool) bool {
	if r.dirOnly && !isDir {
		return false
	}
	if !r.anchored {
		name := rel
		if i := strings.LastIndexByte(rel, '/'); i >= 0 {
			name = rel[i+1:]
		}
		if r.literal {
			return name == r.segs[0]
		}
		ok, _ := path.Match(r.segs[0], name)
		return ok
	}
	return matchIgnoreSegs(r.segs, strings.Split(rel, "/"))
}

// matchIgnoreSegs is like MatchPath but a trailing "**" needs at least one
// segment: "a/**" matches everything inside a, not a itself.
func matchIgnoreSegs(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			if len(pat) == 1 {
				return len(name) > 0
			}
			for i := 0; i <= len(name); i++ {
				if matchIgnoreSegs(pat[1:], name[i:]) {
					return true
				}
			}
			return false
		}
		if len(name) == 0 {
			return false
		}
		if ok, _ := path.Match(pat[0], name[0]); !ok {
			return false
		}
		pat, name = pat[1:], name[1:]
	}
	return len(name) == 0
}

// result of consulting one file: 0 no opinion, 1 ignored, -1 explicitly re-included.
func (f *ignoreFile) verdict(rel string, isDir bool) int {
	if f.base != "" {
		if !strings.HasPrefix(rel, f.base+"/") {
			return 0
		}
		rel = rel[len(f.base)+1:]
	}
	for i := len(f.rules) - 1; i >= 0; i-- { // last matching rule wins
		if f.rules[i].match(rel, isDir) {
			if f.rules[i].negate {
				return -1
			}
			return 1
		}
	}
	return 0
}

// ignoreStack is the ordered list of ignore files in effect. Later entries
// (deeper directories) take precedence over earlier ones.
type ignoreStack []*ignoreFile

// entryIgnored reports whether the path itself matches, without considering
// its parents (the walker never descends into ignored directories, so a
// negated file under an ignored directory is never reached).
func (s ignoreStack) entryIgnored(rel string, isDir bool) bool {
	for i := len(s) - 1; i >= 0; i-- {
		switch s[i].verdict(rel, isDir) {
		case 1:
			return true
		case -1:
			return false
		}
	}
	return false
}

// Ignored reports whether rel is ignored, including through any ignored
// ancestor directory.
func (s ignoreStack) Ignored(rel string, isDir bool) bool {
	parts := strings.Split(rel, "/")
	for i := 1; i < len(parts); i++ {
		if s.entryIgnored(strings.Join(parts[:i], "/"), true) {
			return true
		}
	}
	return s.entryIgnored(rel, isDir)
}
