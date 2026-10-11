package brief

import (
	"regexp"
	"strings"
	"unicode"
)

// Everything repository-derived that reaches the Markdown block must pass one
// of these checks; values that fail are dropped, never sanitised into prose.
var (
	versionRe = regexp.MustCompile(`^[0-9A-Za-z.<>=^~|*+ -]{1,40}$`)
	pathRe    = regexp.MustCompile(`^[A-Za-z0-9@._+/-]{1,120}$`)
	patternRe = regexp.MustCompile(`^[A-Za-z0-9@._+/*?-]{1,120}$`)
	pkgRe     = regexp.MustCompile(`^[@A-Za-z0-9._/+:-]{1,100}$`)
	nameRe    = regexp.MustCompile(`^[a-z0-9._+#-]{1,30}$`)
	reasonRe  = regexp.MustCompile(`^[a-z-]{1,20}$`)
)

// Words (2+ letters) that may appear in a version string.
var versionWords = set("stable beta nightly lts latest rc alpha dev preview canary current go jdk openjdk temurin zulu corretto graalvm liberica semeru oracle")

func validVersion(v string) bool {
	if !versionRe.MatchString(v) {
		return false
	}
	run := ""
	flush := func() bool {
		ok := len(run) < 2 || versionWords[strings.ToLower(run)]
		run = ""
		return ok
	}
	for _, r := range v {
		if unicode.IsLetter(r) {
			run += string(r)
			continue
		}
		if !flush() {
			return false
		}
	}
	return flush()
}

func validPath(p string) bool    { return pathRe.MatchString(p) && !strings.Contains(p, "..") }
func validPattern(p string) bool { return patternRe.MatchString(p) }
func validPkg(p string) bool     { return pkgRe.MatchString(p) }
func validName(p string) bool    { return nameRe.MatchString(p) }
func validReason(p string) bool  { return reasonRe.MatchString(p) }
