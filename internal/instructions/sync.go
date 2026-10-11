package instructions

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/seanmcmahon101/AgentBrief/internal/brief"
	"github.com/seanmcmahon101/AgentBrief/internal/detect"
	"github.com/seanmcmahon101/AgentBrief/internal/model"
)

// SyncOptions controls Sync.
type SyncOptions struct {
	DryRun    bool // compute changes without writing
	NoImports bool // do not add `@AGENTS.md` to CLAUDE.md / GEMINI.md
}

// File statuses reported by Sync.
const (
	StatusCreated   = "created"
	StatusUpdated   = "updated"
	StatusUnchanged = "unchanged"
	StatusSkipped   = "skipped"
)

// FileResult is the outcome for one file.
type FileResult struct {
	Path   string `json:"path"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"` // why a file was skipped
	Diff   string `json:"diff,omitempty"`   // unified-ish diff (changed files)
}

// SyncResult lists what Sync did (or, with DryRun, would do).
type SyncResult struct {
	Files  []FileResult `json:"files"`
	DryRun bool         `json:"dry_run"`
}

// Changed returns the paths that were (or would be) created or updated.
func (r SyncResult) Changed() []string {
	var out []string
	for _, f := range r.Files {
		if f.Status == StatusCreated || f.Status == StatusUpdated {
			out = append(out, f.Path)
		}
	}
	return out
}

// Unchanged returns the paths left as they were.
func (r SyncResult) Unchanged() []string {
	var out []string
	for _, f := range r.Files {
		if f.Status == StatusUnchanged {
			out = append(out, f.Path)
		}
	}
	return out
}

const newFileHint = "<!-- Add your own guidance anywhere outside the AgentBrief block; `agentbrief sync` only rewrites between its markers. -->"

// Sync writes the managed block to root/AGENTS.md and adds import shims.
func Sync(root string, repo *model.Repo, opts SyncOptions) (SyncResult, error) {
	res := SyncResult{DryRun: opts.DryRun, Files: []FileResult{}}
	block := brief.Build(repo, ".").Markdown()

	// ---- AGENTS.md
	agents := filepath.Join(root, "AGENTS.md")
	old, mode, exists, err := readExisting(agents)
	if err != nil {
		return res, err
	}
	var want string
	switch {
	case !exists:
		want = "# AGENTS.md\n\n" + newFileHint + "\n\n" + block + "\n"
	default:
		eol := detectEOL(old)
		if _, ferr := FindBlock(old); ferr == nil {
			want, err = ReplaceBlock(old, block)
			if err != nil {
				return res, err
			}
		} else if errors.Is(ferr, ErrNoBlock) {
			sep := ""
			switch {
			case old == "":
			case strings.HasSuffix(old, eol+eol):
			case strings.HasSuffix(old, "\n"):
				sep = eol
			default:
				sep = eol + eol
			}
			want = old + sep + toEOL(block, eol) + eol
		} else {
			return res, fmt.Errorf("AGENTS.md: %w", ferr)
		}
	}
	fr, err := apply(agents, "AGENTS.md", old, want, exists, mode, opts.DryRun)
	if err != nil {
		return res, err
	}
	res.Files = append(res.Files, fr)

	// ---- import shims
	if !opts.NoImports {
		for _, name := range []string{"CLAUDE.md", "GEMINI.md"} {
			p := filepath.Join(root, name)
			fi, err := os.Lstat(p)
			if err != nil {
				continue // absent: nothing to do
			}
			if !fi.Mode().IsRegular() {
				res.Files = append(res.Files, FileResult{Path: name, Status: StatusSkipped, Reason: "not a regular file (symlink?)"})
				continue
			}
			cur, mode, _, err := readExisting(p)
			if err != nil {
				return res, err
			}
			if importsAgents(cur) {
				res.Files = append(res.Files, FileResult{Path: name, Status: StatusUnchanged})
				continue
			}
			fr, err := apply(p, name, cur, addImport(cur), true, mode, opts.DryRun)
			if err != nil {
				return res, err
			}
			res.Files = append(res.Files, fr)
		}
	}
	return res, nil
}

func importsAgents(text string) bool {
	for _, t := range detect.ExtractImports(text) {
		if cleanImport(t) == "AGENTS.md" {
			return true
		}
	}
	return false
}

func cleanImport(t string) string {
	return strings.TrimPrefix(filepath.ToSlash(filepath.Clean(t)), "./")
}

// addImport inserts "@AGENTS.md" and a blank line at the top of text.
func addImport(text string) string {
	bom := ""
	if strings.HasPrefix(text, utf8BOM) {
		bom, text = utf8BOM, text[len(utf8BOM):]
	}
	eol := detectEOL(text)
	if strings.TrimSpace(text) == "" {
		return bom + "@AGENTS.md" + eol
	}
	return bom + "@AGENTS.md" + eol + eol + text
}

func readExisting(p string) (text string, mode os.FileMode, exists bool, err error) {
	fi, err := os.Lstat(p)
	if errors.Is(err, fs.ErrNotExist) {
		return "", 0o644, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return "", 0, false, fmt.Errorf("%s is a symlink; refusing to rewrite it", filepath.Base(p))
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return "", 0, false, err
	}
	return string(b), fi.Mode().Perm(), true, nil
}

func apply(abs, rel, old, want string, exists bool, mode os.FileMode, dry bool) (FileResult, error) {
	if exists && old == want {
		return FileResult{Path: rel, Status: StatusUnchanged}, nil
	}
	fr := FileResult{Path: rel, Status: StatusUpdated, Diff: unifiedDiff(rel, old, want)}
	if !exists {
		fr.Status = StatusCreated
	}
	if dry {
		return fr, nil
	}
	if err := writeAtomic(abs, []byte(want), mode); err != nil {
		return fr, err
	}
	return fr, nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".agentbrief-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	cleanup := func() { _ = os.Remove(name) }
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(name, mode); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(name, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

const utf8BOM = "\xef\xbb\xbf"
