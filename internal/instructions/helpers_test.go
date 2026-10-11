package instructions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/seanmcmahon101/AgentBrief/internal/detect"
	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// fixture writes files to a temp dir and returns the root plus a Repo whose
// instruction files were discovered from disk; callers add tasks etc.
func fixture(t *testing.T, files map[string]string, repo model.Repo) (string, *model.Repo) {
	t.Helper()
	root := t.TempDir()
	for p, c := range files {
		write(t, root, p, c)
	}
	return root, refresh(t, root, repo)
}

func refresh(t *testing.T, root string, repo model.Repo) *model.Repo {
	t.Helper()
	sr, err := scan.Open(root, scan.Options{})
	if err != nil {
		t.Fatal(err)
	}
	repo.Schema = model.SchemaVersion
	repo.Instructions = detect.InstructionFiles(sr)
	return &repo
}

func npmTask(name, dir string) model.Task {
	return model.Task{Runner: "npm-script", Name: name, Dir: dir, Source: model.Source{Path: dir + "/package.json"}}
}

func has(fs []model.Finding, rule, substr string) bool {
	for _, f := range fs {
		if f.Rule == rule && strings.Contains(f.Message+" "+f.Suggestion, substr) {
			return true
		}
	}
	return false
}

func count(fs []model.Finding, rule string) int {
	n := 0
	for _, f := range fs {
		if f.Rule == rule {
			n++
		}
	}
	return n
}
