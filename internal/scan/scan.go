// Package scan provides a read-only, safety-filtered view of a repository.
//
// Detectors never touch the filesystem directly; they go through a *Repo. This
// keeps three guarantees in one place:
//
//   - Sensitive files (.env, private keys, credentials) are listed but can never
//     be read. Read returns ErrSensitive for them.
//   - Reads are size-capped, and symlinks are not followed out of the root.
//   - Ignored and heavy directories (node_modules, .git, target, ...) are skipped.
//
// FromMap builds an in-memory Repo, which is what most unit tests should use.
package scan

import (
	"errors"
	"io/fs"
	"path"
	"sort"
	"strings"
)

// MaxReadBytes caps how much of any single file Read returns.
const MaxReadBytes = 1 << 20

var (
	// ErrSensitive is returned when reading a file that may contain secrets.
	ErrSensitive = errors.New("refusing to read sensitive file")
	// ErrNotFound is returned for files that are not part of the scanned set.
	ErrNotFound = fs.ErrNotExist
)

// Repo is an immutable snapshot of the files in a repository.
type Repo struct {
	root      string            // absolute path on disk; "" for in-memory repos
	files     []string          // sorted, slash-separated, relative paths
	fileSet   map[string]bool   //
	dirs      map[string]bool   // every ancestor directory of a file, plus "."
	mem       map[string][]byte // in-memory contents (FromMap only)
	truncated bool
	readFile  func(rel string) ([]byte, error) // disk reader (Open only)
}

// Root returns the absolute repository root, or "" for an in-memory repo.
func (r *Repo) Root() string { return r.root }

// Files returns all scanned file paths, sorted. Do not modify the slice.
func (r *Repo) Files() []string { return r.files }

// Truncated reports whether the scan stopped early because of the file limit.
func (r *Repo) Truncated() bool { return r.truncated }

// Has reports whether rel is a scanned file.
func (r *Repo) Has(rel string) bool { return r.fileSet[clean(rel)] }

// IsDir reports whether rel is a directory containing at least one scanned file.
func (r *Repo) IsDir(rel string) bool { return r.dirs[clean(rel)] }

// Exists reports whether rel is a scanned file or a non-empty directory.
func (r *Repo) Exists(rel string) bool { return r.Has(rel) || r.IsDir(rel) }

// Read returns the contents of a scanned file, truncated to MaxReadBytes.
// It returns ErrSensitive for files matched by IsSensitive.
func (r *Repo) Read(rel string) ([]byte, error) {
	rel = clean(rel)
	if !r.fileSet[rel] {
		return nil, ErrNotFound
	}
	if IsSensitive(rel) {
		return nil, ErrSensitive
	}
	var b []byte
	var err error
	if r.mem != nil {
		b = r.mem[rel]
	} else {
		b, err = r.readFile(rel)
		if err != nil {
			return nil, err
		}
	}
	if len(b) > MaxReadBytes {
		b = b[:MaxReadBytes]
	}
	return b, nil
}

// ReadString is Read returning a string, or "" on any error.
func (r *Repo) ReadString(rel string) string {
	b, err := r.Read(rel)
	if err != nil {
		return ""
	}
	return string(b)
}

// Head returns at most n bytes from the start of a file.
func (r *Repo) Head(rel string, n int) ([]byte, error) {
	b, err := r.Read(rel)
	if err != nil {
		return nil, err
	}
	if len(b) > n {
		b = b[:n]
	}
	return b, nil
}

// Glob returns sorted files matching a slash-separated pattern. In addition to
// path.Match syntax, a "**" segment matches zero or more directories.
func (r *Repo) Glob(pattern string) []string {
	var out []string
	for _, f := range r.files {
		if MatchPath(pattern, f) {
			out = append(out, f)
		}
	}
	return out
}

// GlobDirs returns sorted directories (that contain scanned files) matching pattern.
func (r *Repo) GlobDirs(pattern string) []string {
	pattern = clean(pattern)
	var out []string
	for d := range r.dirs {
		if MatchPath(pattern, d) {
			out = append(out, d)
		}
	}
	sort.Strings(out)
	return out
}

// FilesUnder returns files inside dir (recursively). dir "." means all files.
func (r *Repo) FilesUnder(dir string) []string {
	dir = clean(dir)
	if dir == "." {
		return r.files
	}
	prefix := dir + "/"
	i := sort.SearchStrings(r.files, prefix)
	var out []string
	for ; i < len(r.files) && strings.HasPrefix(r.files[i], prefix); i++ {
		out = append(out, r.files[i])
	}
	return out
}

// ChildDirs returns the immediate subdirectory names of dir, sorted.
func (r *Repo) ChildDirs(dir string) []string {
	dir = clean(dir)
	var out []string
	for d := range r.dirs {
		if d != "." && path.Dir(d) == dir {
			out = append(out, path.Base(d))
		}
	}
	sort.Strings(out)
	return out
}

// MatchPath matches a slash-separated path against a pattern where each
// segment uses path.Match syntax and a "**" segment matches any number
// (including zero) of segments.
func MatchPath(pattern, name string) bool {
	return matchSegs(strings.Split(clean(pattern), "/"), strings.Split(clean(name), "/"))
}

func matchSegs(pat, name []string) bool {
	for len(pat) > 0 {
		if pat[0] == "**" {
			for i := 0; i <= len(name); i++ {
				if matchSegs(pat[1:], name[i:]) {
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

// FromMap builds an in-memory Repo from relative path -> contents.
func FromMap(files map[string]string) *Repo {
	mem := make(map[string][]byte, len(files))
	list := make([]string, 0, len(files))
	for p, c := range files {
		p = clean(p)
		mem[p] = []byte(c)
		list = append(list, p)
	}
	r := newRepo("", list)
	r.mem = mem
	return r
}

func newRepo(root string, files []string) *Repo {
	sort.Strings(files)
	r := &Repo{root: root, files: files, fileSet: map[string]bool{}, dirs: map[string]bool{".": true}}
	for _, f := range files {
		r.fileSet[f] = true
		for d := path.Dir(f); d != "." && !r.dirs[d]; d = path.Dir(d) {
			r.dirs[d] = true
		}
	}
	return r
}

func clean(p string) string {
	p = path.Clean(strings.ReplaceAll(p, "\\", "/"))
	return strings.TrimPrefix(p, "/")
}
