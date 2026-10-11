package instructions

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
)

func syncRepo() model.Repo {
	return model.Repo{
		PackageManagers: []model.PackageManager{{Name: "pnpm", Ecosystem: "node", Dir: ".", Version: "9.0.0", Source: model.Source{Path: "package.json"}}},
		Commands:        []model.Command{{Kind: "test", Run: "pnpm test", Dir: ".", Evidence: []string{"declared"}, Source: model.Source{Path: "package.json"}}},
	}
}

func TestSyncCreateIdempotent(t *testing.T) {
	root, repo := fixture(t, nil, syncRepo())
	res, err := Sync(root, repo, SyncOptions{})
	if err != nil || len(res.Changed()) != 1 || res.Files[0].Status != StatusCreated {
		t.Fatalf("%v %+v", err, res)
	}
	first := read(t, root, "AGENTS.md")
	if !strings.HasPrefix(first, "# AGENTS.md\n\n<!--") || !strings.HasSuffix(first, EndMarker+"\n") || !strings.Contains(first, "`pnpm test`") {
		t.Fatalf("%s", first)
	}
	fi1, _ := os.Stat(filepath.Join(root, "AGENTS.md"))
	repo = refresh(t, root, syncRepo())
	res, err = Sync(root, repo, SyncOptions{})
	if err != nil || len(res.Changed()) != 0 || res.Files[0].Status != StatusUnchanged {
		t.Fatalf("second run must not write: %v %+v", err, res)
	}
	if read(t, root, "AGENTS.md") != first {
		t.Error("bytes changed")
	}
	fi2, _ := os.Stat(filepath.Join(root, "AGENTS.md"))
	if !fi1.ModTime().Equal(fi2.ModTime()) {
		t.Error("file rewritten")
	}
	if f := Check(root, repo, CheckOptions{}); count(f, RuleStaleBlock) != 0 {
		t.Errorf("fresh block flagged stale: %+v", f)
	}
}

func TestSyncAppendKeepsHandWrittenText(t *testing.T) {
	for _, tc := range []struct{ name, in string }{
		{"newline", "# Mine\n\nKeep me.\n"},
		{"no trailing newline", "# Mine\n\nKeep me."},
		{"crlf", "# Mine\r\n\r\nKeep me.\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, repo := fixture(t, map[string]string{"AGENTS.md": tc.in}, syncRepo())
			if _, err := Sync(root, repo, SyncOptions{}); err != nil {
				t.Fatal(err)
			}
			got := read(t, root, "AGENTS.md")
			if !strings.HasPrefix(got, tc.in) {
				t.Errorf("prefix lost: %q", got)
			}
			if strings.Contains(tc.in, "\r\n") && strings.Contains(strings.ReplaceAll(got, "\r\n", ""), "\n") {
				t.Errorf("mixed line endings: %q", got)
			}
			if !strings.HasSuffix(got, "\n") {
				t.Error("missing trailing newline")
			}
			// Edits outside the block survive a re-sync with changed facts.
			r2 := syncRepo()
			r2.Commands[0].Run = "pnpm vitest"
			repo = refresh(t, root, r2)
			if _, err := Sync(root, repo, SyncOptions{}); err != nil {
				t.Fatal(err)
			}
			got2 := read(t, root, "AGENTS.md")
			if !strings.HasPrefix(got2, tc.in) || !strings.Contains(got2, "pnpm vitest") || strings.Contains(got2, "`pnpm test`") {
				t.Errorf("replace failed: %q", got2)
			}
		})
	}
}

func TestSyncMalformedBlockErrors(t *testing.T) {
	root, repo := fixture(t, map[string]string{"AGENTS.md": "x\n" + StartMarker + "\nno end\n"}, syncRepo())
	if _, err := Sync(root, repo, SyncOptions{}); err == nil {
		t.Fatal("want error")
	}
	if read(t, root, "AGENTS.md") != "x\n"+StartMarker+"\nno end\n" {
		t.Error("file modified despite error")
	}
}

func TestSyncImports(t *testing.T) {
	files := map[string]string{
		"AGENTS.md": "# a\n",
		"CLAUDE.md": "# Claude\nrules\n",
		"GEMINI.md": "@./AGENTS.md\nrest\n",
	}
	root, repo := fixture(t, files, syncRepo())
	res, err := Sync(root, repo, SyncOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if read(t, root, "CLAUDE.md") != files["CLAUDE.md"] || read(t, root, "AGENTS.md") != "# a\n" {
		t.Fatal("dry run wrote")
	}
	if len(res.Changed()) != 2 || !strings.Contains(res.Files[1].Diff, "+@AGENTS.md") {
		t.Errorf("%+v", res)
	}
	if _, err := Sync(root, repo, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "CLAUDE.md"); got != "@AGENTS.md\n\n# Claude\nrules\n" {
		t.Errorf("%q", got)
	}
	if read(t, root, "GEMINI.md") != files["GEMINI.md"] {
		t.Error("gemini already imports; must be untouched")
	}
	repo = refresh(t, root, syncRepo())
	res, _ = Sync(root, repo, SyncOptions{})
	if len(res.Changed()) != 0 {
		t.Errorf("second run changed %v", res.Changed())
	}
	if f := Check(root, repo, CheckOptions{}); count(f, RuleAgentsNotLoaded) != 0 {
		t.Errorf("%+v", f)
	}
}

func TestSyncImportCRLFAndNoImports(t *testing.T) {
	root, repo := fixture(t, map[string]string{"AGENTS.md": "a\r\n", "CLAUDE.md": "# C\r\nx\r\n"}, syncRepo())
	if _, err := Sync(root, repo, SyncOptions{NoImports: true}); err != nil {
		t.Fatal(err)
	}
	if read(t, root, "CLAUDE.md") != "# C\r\nx\r\n" {
		t.Error("NoImports ignored")
	}
	if _, err := Sync(root, repo, SyncOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := read(t, root, "CLAUDE.md"); got != "@AGENTS.md\r\n\r\n# C\r\nx\r\n" {
		t.Errorf("%q", got)
	}
}

func TestSyncSymlinkAndMode(t *testing.T) {
	root, repo := fixture(t, map[string]string{"AGENTS.md": "# a\n"}, syncRepo())
	if err := os.Chmod(filepath.Join(root, "AGENTS.md"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("AGENTS.md", filepath.Join(root, "CLAUDE.md")); err != nil {
		t.Skip("symlinks unavailable")
	}
	res, err := Sync(root, repo, SyncOptions{})
	if err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(filepath.Join(root, "AGENTS.md"))
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 { // Windows has no Unix permission bits
		t.Errorf("mode %v", fi.Mode())
	}
	for _, f := range res.Files {
		if f.Path == "CLAUDE.md" && f.Status != StatusSkipped {
			t.Errorf("symlink not skipped: %+v", f)
		}
	}
}
