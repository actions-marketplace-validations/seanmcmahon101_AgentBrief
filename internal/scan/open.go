package scan

import (
	"io"
	"os"
	"path/filepath"
)

// Options controls a disk scan.
type Options struct {
	// MaxFiles stops the walk after this many files (0 = DefaultMaxFiles).
	MaxFiles int
}

// DefaultMaxFiles bounds scan time on very large repositories.
const DefaultMaxFiles = 200000

// skipDirs are never descended into, whatever .gitignore says.
var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true, ".venv": true,
	"venv": true, "__pycache__": true, ".tox": true, ".nox": true, ".mypy_cache": true,
	".ruff_cache": true, ".pytest_cache": true, "target": true, ".gradle": true,
	".next": true, ".nuxt": true, ".svelte-kit": true, ".turbo": true, ".cache": true,
	"bower_components": true, ".terraform": true, "_build": true, ".dart_tool": true,
	"Pods": true, "DerivedData": true, ".build": true,
}

// Open walks root and returns a Repo of the files found. Files excluded by the
// root and nested .gitignore files and by .git/info/exclude are left out, as are
// the heavy directories in skipDirs. Symlinks are never followed.
func Open(root string, opts Options) (*Repo, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(abs); err != nil {
		return nil, err
	}
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = DefaultMaxFiles
	}
	w := &walker{abs: abs, max: opts.MaxFiles}
	var stack ignoreStack
	if b, err := readCapped(filepath.Join(abs, ".git", "info", "exclude")); err == nil {
		stack = append(stack, parseIgnore("", string(b)))
	}
	w.walk("", stack)
	files := w.files
	r := newRepo(abs, files)
	r.truncated = w.truncated
	r.readFile = func(rel string) ([]byte, error) {
		f, err := os.Open(filepath.Join(abs, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(io.LimitReader(f, MaxReadBytes))
	}
	r.readHead = func(rel string, n int) ([]byte, error) {
		f, err := os.Open(filepath.Join(abs, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(io.LimitReader(f, int64(n)))
	}
	return r, nil
}

type walker struct {
	abs       string
	max       int
	files     []string
	truncated bool
}

// walk lists dir (relative, "" for the root) depth-first. The ignore stack
// grows by the directory's own .gitignore, so deeper files take precedence.
func (w *walker) walk(dir string, stack ignoreStack) {
	entries, err := os.ReadDir(filepath.Join(w.abs, filepath.FromSlash(dir)))
	if err != nil {
		return // unreadable directories are skipped, not fatal
	}
	for _, e := range entries {
		if e.Name() == ".gitignore" && e.Type().IsRegular() {
			if b, err := readCapped(filepath.Join(w.abs, filepath.FromSlash(dir), ".gitignore")); err == nil {
				stack = append(stack[:len(stack):len(stack)], parseIgnore(dir, string(b)))
			}
			break
		}
	}
	for _, e := range entries {
		if w.truncated {
			return
		}
		rel := e.Name()
		if dir != "" {
			rel = dir + "/" + rel
		}
		switch {
		case e.IsDir():
			if skipDirs[e.Name()] || stack.entryIgnored(rel, true) {
				continue
			}
			w.walk(rel, stack)
		case e.Type().IsRegular(): // symlinks, sockets, devices: never followed
			if stack.entryIgnored(rel, false) {
				continue
			}
			if len(w.files) >= w.max {
				w.truncated = true
				return
			}
			w.files = append(w.files, rel)
		}
	}
}

func readCapped(p string) ([]byte, error) {
	f, err := os.Open(p)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, MaxReadBytes))
}
