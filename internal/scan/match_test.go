package scan

import (
	"strings"
	"testing"
)

func TestMatchPathRecursiveSegments(t *testing.T) {
	for _, c := range []struct {
		pattern, file string
		want          bool
	}{
		{"**/*.go", "x.go", true},
		{"**/*.go", "a/b/x.go", true},
		{"a/**/b", "a/b", true},
		{"a/**/b", "a/x/y/b", true},
		{"a/*/b", "a/x/y/b", false},
		{"a/**", "a", true},
		{"*.go", "a/x.go", false},
		{"a/?.go", "a/x.go", true},
		{"[", "a", false},
	} {
		if got := MatchPath(c.pattern, c.file); got != c.want {
			t.Errorf("%q %q: %v", c.pattern, c.file, got)
		}
	}
	// This mismatch has exponentially many partitions in a recursive matcher.
	if MatchPath(strings.Repeat("**/", 30)+"absent", strings.Repeat("a/", 40)+"present") {
		t.Fatal("matched a missing final segment")
	}
}
