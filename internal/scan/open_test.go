package scan

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
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

func TestOpenHonoursGitignore(t *testing.T) {
	root := t.TempDir()
	write(t, root, ".gitignore", "dist/\n*.log\n!keep.log\n")
	write(t, root, ".git/info/exclude", "secret.txt\n")
	write(t, root, ".git/HEAD", "ref")
	write(t, root, "main.go", "")
	write(t, root, "a.log", "")
	write(t, root, "keep.log", "")
	write(t, root, "secret.txt", "")
	write(t, root, "dist/out.js", "")
	write(t, root, "node_modules/x/index.js", "")
	write(t, root, "pkg/.gitignore", "gen/\n!/main.go\n")
	write(t, root, "pkg/gen/x.go", "")
	write(t, root, "pkg/main.go", "")
	write(t, root, "pkg/other/gen/y.go", "")
	if err := os.Symlink(filepath.Join(root, "main.go"), filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	r, err := Open(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{".gitignore", "keep.log", "main.go", "pkg/.gitignore", "pkg/main.go"}
	if !reflect.DeepEqual(r.Files(), want) {
		t.Errorf("files = %v, want %v", r.Files(), want)
	}
}

func TestOpenTruncates(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		write(t, root, n, "")
	}
	r, err := Open(root, Options{MaxFiles: 2})
	if err != nil {
		t.Fatal(err)
	}
	if !r.Truncated() || len(r.Files()) != 2 {
		t.Errorf("truncated=%v files=%v", r.Truncated(), r.Files())
	}
}

func TestHeadReadsPrefixOnly(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.txt", "0123456789")
	write(t, root, ".env", "SECRET=1")
	r, err := Open(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Head("a.txt", 4)
	if err != nil || string(b) != "0123" {
		t.Errorf("Head = %q, %v", b, err)
	}
	if _, err := r.Head(".env", 4); err != ErrSensitive {
		t.Errorf("Head(.env) err = %v, want ErrSensitive", err)
	}
	if _, err := r.Head("missing", 4); err != ErrNotFound {
		t.Errorf("Head(missing) err = %v", err)
	}
}
