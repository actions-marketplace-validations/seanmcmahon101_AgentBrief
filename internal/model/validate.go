package model

import (
	"regexp"
	"strings"
	"unicode"
)

// Everything repository-derived that reaches any output must pass one of these
// checks; values that fail are dropped, never sanitised into prose.
var (
	versionRe = regexp.MustCompile(`^[0-9A-Za-z.<>=^~|*+ -]{1,40}$`)
	pathRe    = regexp.MustCompile(`^[A-Za-z0-9@._+/-]{1,120}$`)
	patternRe = regexp.MustCompile(`^[A-Za-z0-9@._+/*?-]{1,120}$`)
	pkgRe     = regexp.MustCompile(`^[@A-Za-z0-9._/+:-]{1,100}$`)
	nameRe    = regexp.MustCompile(`^[a-z0-9._+#-]{1,30}$`)
	reasonRe  = regexp.MustCompile(`^[a-z-]{1,20}$`)
)

// Words (2+ letters) that may appear in a version string.
var versionWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields("stable beta nightly lts latest rc alpha dev preview canary current go jdk openjdk temurin zulu corretto graalvm liberica semeru oracle") {
		versionWords[w] = true
	}
}

// ValidVersion reports whether v looks like a version or version range and
// contains no words beyond release-channel names.
func ValidVersion(v string) bool {
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

// ValidPath reports whether p is a plain relative repository path.
func ValidPath(p string) bool { return pathRe.MatchString(p) && !strings.Contains(p, "..") }

// ValidPattern reports whether p is a plain path glob.
func ValidPattern(p string) bool { return patternRe.MatchString(p) }

// ValidPkg reports whether p looks like a package or project name.
func ValidPkg(p string) bool { return pkgRe.MatchString(p) }

// ValidName reports whether p is a short lowercase identifier (tool, ecosystem).
func ValidName(p string) bool { return nameRe.MatchString(p) }

// ValidReason reports whether p is a short lowercase reason label.
func ValidReason(p string) bool { return reasonRe.MatchString(p) }

// ValidCommand reports whether a command line is safe to emit: one line, no
// control characters, at most 160 bytes.
func ValidCommand(c string) bool {
	if c == "" || len(c) > 160 {
		return false
	}
	for _, r := range c {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
