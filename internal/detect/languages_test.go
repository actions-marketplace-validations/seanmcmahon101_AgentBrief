package detect

import (
	"reflect"
	"testing"

	"github.com/seanmcmahon101/AgentBrief/internal/model"
	"github.com/seanmcmahon101/AgentBrief/internal/scan"
)

// runOnly runs a single detector function over an in-memory repo.
func runOnly(files map[string]string, fns ...Detector) *Facts {
	r := scan.FromMap(files)
	f := &Facts{Repo: &model.Repo{Schema: model.SchemaVersion}}
	for _, fn := range fns {
		fn(r, f)
	}
	return f
}

func TestLanguages(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
		want  []model.Language
	}{
		{"groups ts and tsx", map[string]string{"a.ts": "", "b.tsx": "", "c.js": "", "README.md": "", "d.json": ""},
			[]model.Language{{Name: "TypeScript", Files: 2}, {Name: "JavaScript", Files: 1}}},
		{"ignores vendored and generated paths", map[string]string{"main.go": "", "vendor/x/y.go": "", "dist/a.js": "", "web/generated/x.ts": ""},
			[]model.Language{{Name: "Go", Files: 1}}},
		{"ignores docs and data", map[string]string{"a.md": "", "b.yaml": "", "c.txt": "", "d.lock": ""}, nil},
		{"drops tiny stragglers", func() map[string]string {
			m := map[string]string{"x.sh": ""}
			for i := 0; i < 60; i++ {
				m["f"+string(rune('a'+i%26))+string(rune('a'+i/26))+".py"] = ""
			}
			return m
		}(), []model.Language{{Name: "Python", Files: 60}}},
		{"empty", map[string]string{}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := runOnly(tt.files, detectLanguages)
			if !reflect.DeepEqual(f.Languages, tt.want) {
				t.Errorf("got %v want %v", f.Languages, tt.want)
			}
		})
	}
}

func TestLanguagesCapped(t *testing.T) {
	files := map[string]string{}
	for i, ext := range []string{".go", ".rs", ".py", ".js", ".java", ".rb", ".php", ".cs", ".swift", ".ex"} {
		files["f"+string(rune('a'+i))+ext] = ""
	}
	if n := len(runOnly(files, detectLanguages).Languages); n != maxLanguages {
		t.Errorf("got %d languages, want %d", n, maxLanguages)
	}
}
