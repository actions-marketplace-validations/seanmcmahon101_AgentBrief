package scan

import "testing"

func TestIgnoreMatching(t *testing.T) {
	type c struct {
		path string
		dir  bool
		want bool
	}
	tests := []struct {
		name  string
		rules string
		cases []c
	}{
		{"basename any depth", "foo\n", []c{{"foo", false, true}, {"a/b/foo", false, true}, {"foobar", false, false}, {"a/foo/x", false, true}}},
		{"glob", "*.log\n", []c{{"a.log", false, true}, {"d/a.log", false, true}, {"a.logs", false, false}}},
		{"dir only", "build/\n", []c{{"build", true, true}, {"build", false, false}, {"x/build", true, true}, {"build/out.o", false, true}}},
		{"anchored leading slash", "/foo\n", []c{{"foo", false, true}, {"a/foo", false, false}}},
		{"anchored middle slash", "a/b\n", []c{{"a/b", false, true}, {"x/a/b", false, false}}},
		{"star does not cross slash", "a/*.c\n", []c{{"a/x.c", false, true}, {"a/b/x.c", false, false}}},
		{"double star prefix", "**/foo\n", []c{{"foo", false, true}, {"a/b/foo", false, true}}},
		{"double star suffix", "abc/**\n", []c{{"abc/x", false, true}, {"abc/x/y", false, true}, {"abc", true, false}}},
		{"double star middle", "a/**/b\n", []c{{"a/b", false, true}, {"a/x/b", false, true}, {"a/x/y/b", false, true}, {"b", false, false}}},
		{"negation", "*.log\n!keep.log\n", []c{{"a.log", false, true}, {"keep.log", false, false}, {"d/keep.log", false, false}}},
		{"negation order", "!keep.log\n*.log\n", []c{{"keep.log", false, true}}},
		{"negated inside ignored dir stays ignored", "build/\n!build/keep.txt\n", []c{{"build/keep.txt", false, true}}},
		{"re-include with dir wildcard", "build/*\n!build/keep.txt\n", []c{{"build/keep.txt", false, false}, {"build/other", false, true}}},
		{"comment and blank", "# c\n\n  \nfoo\n", []c{{"foo", false, true}, {"c", false, false}}},
		{"escaped hash", "\\#file\n", []c{{"#file", false, true}}},
		{"escaped bang", "\\!file\n", []c{{"!file", false, true}}},
		{"trailing space trimmed", "foo  \n", []c{{"foo", false, true}}},
		{"escaped trailing space", "foo\\ \n", []c{{"foo ", false, true}, {"foo", false, false}}},
		{"class", "[ab].txt\n", []c{{"a.txt", false, true}, {"c.txt", false, false}}},
		{"negated class", "[!ab].txt\n", []c{{"a.txt", false, false}, {"c.txt", false, true}}},
		{"question mark", "f?o\n", []c{{"foo", false, true}, {"fo", false, false}}},
		{"crlf", "foo\r\n", []c{{"foo", false, true}}},
		{"bad pattern no panic", "[abc\n", []c{{"x", false, false}}},
		{"escaped star", "\\*.txt\n", []c{{"*.txt", false, true}, {"a.txt", false, false}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := ignoreStack{parseIgnore("", tt.rules)}
			for _, c := range tt.cases {
				if got := s.Ignored(c.path, c.dir); got != c.want {
					t.Errorf("Ignored(%q, dir=%v) = %v, want %v", c.path, c.dir, got, c.want)
				}
			}
		})
	}
}

func TestIgnoreNestedPrecedence(t *testing.T) {
	s := ignoreStack{
		parseIgnore("", "*.gen\n/rootonly\n"),
		parseIgnore("sub", "!keep.gen\nlocal\n/top\n"),
	}
	cases := []struct {
		path string
		want bool
	}{
		{"x.gen", true},
		{"sub/x.gen", true},
		{"sub/keep.gen", false}, // nested negation overrides parent
		{"keep.gen", true},      // nested file does not apply outside its dir
		{"sub/a/local", true},
		{"local", false},
		{"sub/top", true},
		{"sub/a/top", false},
		{"rootonly", true},
		{"sub/rootonly", false},
	}
	for _, c := range cases {
		if got := s.Ignored(c.path, false); got != c.want {
			t.Errorf("Ignored(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}
