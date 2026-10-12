package scan

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"syscall"
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

func TestSize(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.txt", "0123456789")
	r, _ := Open(root, Options{})
	if r.Size("a.txt") != 10 || r.Size("nope") != 0 {
		t.Errorf("size = %d", r.Size("a.txt"))
	}
	if FromMap(map[string]string{"x": "abc"}).Size("x") != 3 {
		t.Error("mem size")
	}
}

func TestOpenDoesNotFollowSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, root, "main.go", "")
	write(t, outside, "external.go", "must not read")
	if err := os.Symlink(filepath.Join(outside, "external.go"), filepath.Join(root, "link.go")); err != nil {
		if runtime.GOOS == "windows" && errors.Is(err, syscall.Errno(1314)) {
			t.Skip("Windows account lacks symlink creation privilege")
		}
		t.Fatal(err)
	}
	r, err := Open(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Files(), []string{"main.go"}) {
		t.Fatalf("followed symlink: %v", r.Files())
	}
	if _, err := r.Read("link.go"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("symlink read: %v", err)
	}
}
