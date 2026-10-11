package scan

import (
	"io"
	"io/fs"
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

// Open walks root and returns a Repo of the files found.
// TODO(scan): honour .gitignore files (root and nested) and .git/info/exclude.
func Open(root string, opts Options) (*Repo, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if opts.MaxFiles <= 0 {
		opts.MaxFiles = DefaultMaxFiles
	}
	var files []string
	truncated := false
	err = filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if d.IsDir() {
			if p != abs && skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() { // symlinks, sockets, devices: never followed
			return nil
		}
		if len(files) >= opts.MaxFiles {
			truncated = true
			return filepath.SkipAll
		}
		rel, err := filepath.Rel(abs, p)
		if err != nil {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		return nil, err
	}
	r := newRepo(abs, files)
	r.truncated = truncated
	r.readFile = func(rel string) ([]byte, error) {
		f, err := os.Open(filepath.Join(abs, filepath.FromSlash(rel)))
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return io.ReadAll(io.LimitReader(f, MaxReadBytes))
	}
	return r, nil
}
