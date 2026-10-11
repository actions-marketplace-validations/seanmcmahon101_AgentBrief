package instructions

import (
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
)

// pathIndex lazily indexes every file and directory under root by basename,
// so that shorthand like `components/Foo.tsx` can match `src/components/Foo.tsx`.
type pathIndex struct {
	root   string
	built  bool
	byBase map[string][]string
	top    map[string]bool
}

var skipIndexDirs = map[string]bool{".git": true, "node_modules": true, ".venv": true, "venv": true, "target": true, "__pycache__": true}

func (p *pathIndex) build() {
	if p.built {
		return
	}
	p.built = true
	p.byBase = map[string][]string{}
	p.top = map[string]bool{}
	n := 0
	_ = filepath.WalkDir(p.root, func(abs string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if abs == p.root {
			return nil
		}
		if d.IsDir() && skipIndexDirs[d.Name()] {
			return filepath.SkipDir
		}
		if n++; n > 300000 {
			return filepath.SkipAll
		}
		rel, _ := filepath.Rel(p.root, abs)
		rel = filepath.ToSlash(rel)
		p.byBase[d.Name()] = append(p.byBase[d.Name()], rel)
		if !strings.Contains(rel, "/") {
			p.top[rel] = true
		}
		return nil
	})
}

// suffixMatch reports whether some repo path equals rel or ends with "/"+rel.
func (p *pathIndex) suffixMatch(rel string) bool {
	p.build()
	rel = strings.TrimSuffix(rel, "/")
	for _, c := range p.byBase[path.Base(rel)] {
		if c == rel || strings.HasSuffix(c, "/"+rel) {
			return true
		}
	}
	return false
}

func (p *pathIndex) topLevel(name string) bool { p.build(); return p.top[name] }

var knownExts = set(`.md .mdx .ts .tsx .js .jsx .mjs .cjs .go .py .rs .json .jsonc .yml .yaml .toml .sh .bash .txt .css .scss .html .java .kt .rb .php .c .h .cpp .hpp .cs .swift .sql .lock .cfg .ini .mod .sum .gradle .xml .proto .mdc .vue .svelte .rst .tf .env .mts .cts .pyi .ex .exs .kts .scala .dart .lua .conf .properties .ps1 .bat .cmd .gemspec .rake .csproj .sln .ipynb`)

var placeholderSegs = set(`xxx yyy zzz foo bar baz my_project my-project myproject my_app my-app myapp your-project your_project example filename myfile my_file my-file name project-name app-name java javax jakarta org com android kotlin scala`)

var generatedPrefixes = []string{"dist/", "build/", "node_modules/", "target/", ".venv/", "venv/", "out/", "coverage/", ".next/", "__pycache__/", ".cache/", "tmp/", "vendor/", ".turbo/", ".nuxt/", "site-packages/"}

var (
	pathCharsRe = regexp.MustCompile(`^[A-Za-z0-9._\-/+~@#:]+$`)
	lineSuffix  = regexp.MustCompile(`(:\d+(:\d+)?|#L\d+(-L?\d+)?)$`)
	domainRe    = regexp.MustCompile(`^[A-Za-z0-9-]+(\.[A-Za-z0-9-]+)+$`)
	schemeRe    = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)
	frameworkJS = set(`next node vue react nuxt express three ember d3 p5 chart alpine backbone angular deno nest nuxt3 socket anime knockout moment`)
)

func isGeneratedPath(p string) bool {
	for _, g := range generatedPrefixes {
		if strings.HasPrefix(p, g) || strings.Contains(p, "/"+g) {
			return true
		}
	}
	return false
}

// resolveDirs returns the directories a path in file f may be relative to.
func (c *checker) baseDirs(f model.InstructionFile, forLink bool) []string {
	fd := path.Dir(f.Path)
	dirs := []string{fd}
	if f.Scope != "" && f.Scope != fd {
		dirs = append(dirs, f.Scope)
	}
	if !forLink {
		dirs = append(dirs, ".")
	}
	return dirs
}

func (c *checker) existsAny(f model.InstructionFile, rel string, forLink bool) bool {
	for _, d := range c.baseDirs(f, forLink) {
		p := path.Clean(path.Join(d, rel))
		if strings.HasPrefix(p, "..") {
			continue
		}
		if c.exists(p) {
			return true
		}
	}
	return false
}

func (c *checker) checkLink(f model.InstructionFile, line int, dest string) {
	d := strings.TrimSpace(dest)
	if d == "" || strings.HasPrefix(d, "#") || strings.HasPrefix(d, "/") || strings.HasPrefix(d, "//") || schemeRe.MatchString(d) || strings.HasPrefix(d, "~") {
		return
	}
	if i := strings.IndexAny(d, "#?"); i >= 0 {
		d = d[:i]
	}
	if u, err := url.PathUnescape(d); err == nil {
		d = u
	}
	if d == "" || strings.ContainsAny(d, "*<>${}|") || strings.Contains(d, "...") || strings.Contains(d, "{{") {
		return
	}
	clean := path.Clean(path.Join(path.Dir(f.Path), d))
	if strings.HasPrefix(clean, "..") || isGeneratedPath(clean) || isGeneratedPath(strings.TrimPrefix(d, "./")) {
		return
	}
	if c.existsAny(f, d, true) {
		return
	}
	c.add(model.Finding{Rule: RuleMissingPath, Severity: model.SeverityError, Path: f.Path, Line: line,
		Message: "link target `" + d + "` does not exist"})
}

// spanPath normalises a code span that may be a repo path; ok=false if it is not.
func spanPath(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 200 || !pathCharsRe.MatchString(s) {
		return "", false
	}
	if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "/") || strings.HasPrefix(s, "~") || strings.HasPrefix(s, "@") || strings.HasPrefix(s, "#") {
		return "", false
	}
	s = lineSuffix.ReplaceAllString(s, "")
	if strings.Contains(s, ":") || strings.Contains(s, "#") || strings.Contains(s, "...") {
		return "", false
	}
	if strings.HasPrefix(path.Base(s), ".env") {
		return "", false
	}
	for _, seg := range strings.Split(strings.ToLower(s), "/") {
		stem := strings.TrimSuffix(seg, path.Ext(seg))
		if placeholderSegs[seg] || placeholderSegs[stem] {
			return "", false
		}
	}
	if strings.Contains(s, "path/to/") || strings.HasPrefix(s, "path/to") {
		return "", false
	}
	hasSlash := strings.Contains(s, "/")
	ext := strings.ToLower(path.Ext(strings.TrimSuffix(s, "/")))
	if !hasSlash && !knownExts[ext] {
		return "", false
	}
	if hasSlash && ext == "" && !strings.HasSuffix(s, "/") && !strings.HasPrefix(s, "./") && !strings.HasPrefix(s, "../") && !strings.HasPrefix(s, ".") {
		return s, true // refined by caller with top-level check
	}
	if !hasSlash {
		stem := strings.ToLower(strings.TrimSuffix(path.Base(s), ext))
		base := path.Base(s)
		if ext == ".js" && (frameworkJS[stem] || (base[0] >= 'A' && base[0] <= 'Z')) {
			return "", false
		}
		if stem == "" {
			return "", false
		}
	}
	if strings.Contains(s, "..") && !strings.HasPrefix(s, "../") && !strings.Contains(s, "/../") {
		return "", false
	}
	return s, true
}

func (c *checker) checkSpanPath(f model.InstructionFile, line int, span string) {
	p, ok := spanPath(span)
	if !ok {
		return
	}
	trim := strings.TrimPrefix(p, "./")
	if isGeneratedPath(trim) {
		return
	}
	if c.existsAny(f, p, false) {
		return
	}
	segs := strings.Split(strings.TrimSuffix(trim, "/"), "/")
	hasExt := knownExts[strings.ToLower(path.Ext(trim))]
	// `./main`, `./bin/tool`: an extension-less executable is usually a build
	// output that does not exist until built, so it is not judged.
	if strings.HasPrefix(p, "./") && !hasExt && path.Ext(trim) == "" && !strings.HasSuffix(p, "/") {
		return
	}
	// Module-like specifiers (`react-dom/client`): only judge if the first segment is a real top-level dir.
	if len(segs) > 1 && !hasExt && !strings.HasSuffix(p, "/") && !strings.HasPrefix(p, "./") && !strings.HasPrefix(p, "../") {
		if !c.idx.topLevel(segs[0]) {
			return
		}
	}
	// Go import paths / domains.
	if len(segs) > 1 && domainRe.MatchString(segs[0]) && !strings.HasPrefix(segs[0], ".") {
		return
	}
	if strings.HasPrefix(p, "../") || strings.HasPrefix(p, "./") {
		// explicit relative path: resolved above; nothing else to try
	} else if c.idx.suffixMatch(trim) {
		return
	}
	c.add(model.Finding{Rule: RuleMissingPath, Severity: model.SeverityWarning, Path: f.Path, Line: line,
		Message:    "`" + strings.TrimSuffix(span, "\n") + "` does not exist in the repository",
		Suggestion: ""})
}

var _ = os.Stat

var proseTokenRe = regexp.MustCompile(`[^\s"'()<>\[\],;]+`)

// checkProsePaths flags directory-style references in plain prose (outside
// code spans and links), e.g. "see internal/ghost/". It is deliberately
// narrow: the token must contain a slash, end with "/" or a known extension,
// and start with a top-level directory that really exists, so "and/or" and
// "client/server" are never judged. Always a warning.
func (c *checker) checkProsePaths(f model.InstructionFile, it mdItem) {
	stripped, _ := splitSpans(it.full)
	stripped = linkRe.ReplaceAllString(stripped, " ")
	if refRe.MatchString(stripped) {
		return
	}
	for _, loc := range proseTokenRe.FindAllStringIndex(stripped, -1) {
		tok := stripped[loc[0]:loc[1]]
		tok = strings.TrimRight(strings.TrimLeft(tok, "*_"), "*_.:!?")
		if strings.HasSuffix(tok, ".") || strings.Contains(tok, "://") || strings.HasPrefix(tok, "//") {
			continue
		}
		if !strings.Contains(tok, "/") || len(tok) > 120 || !pathCharsRe.MatchString(tok) {
			continue
		}
		if strings.Contains(tok, ":") || strings.Contains(tok, "#") || strings.Contains(tok, "@") || strings.Contains(tok, "~") {
			continue
		}
		if skipPrefix(stripped[:loc[0]]) {
			continue
		}
		rel := strings.TrimPrefix(tok, "./")
		dirLike := strings.HasSuffix(rel, "/")
		if !dirLike && !knownExts[strings.ToLower(path.Ext(rel))] {
			continue
		}
		segs := strings.Split(strings.TrimSuffix(rel, "/"), "/")
		if len(segs) < 2 || segs[0] == "" || segs[0] == ".." || strings.HasPrefix(segs[0], ".") {
			continue
		}
		if !c.idx.topLevel(segs[0]) || !c.isDir(segs[0]) {
			continue
		}
		clean := strings.TrimSuffix(rel, "/")
		if isGeneratedPath(rel) || spanPlaceholderProse(clean) || strings.ContainsAny(clean, "*{}$|") || strings.Contains(clean, "..") {
			continue
		}
		if c.existsAny(f, clean, false) {
			continue
		}
		c.add(model.Finding{Rule: RuleMissingPath, Severity: model.SeverityWarning, Path: f.Path, Line: it.line,
			Message: "`" + tok + "` is mentioned but does not exist in the repository"})
	}
}

func spanPlaceholderProse(p string) bool { return pathPlaceholder(p) }
