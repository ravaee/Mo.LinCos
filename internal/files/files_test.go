package files

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func setup(t *testing.T) (root, outside string) {
	t.Helper()
	base := t.TempDir()
	root = filepath.Join(base, "data")
	outside = filepath.Join(base, "secret.txt")
	os.MkdirAll(filepath.Join(root, "uploads", "a"), 0o755)
	os.WriteFile(filepath.Join(root, "uploads", "a", "one.txt"), []byte("12345"), 0o644)
	os.WriteFile(filepath.Join(root, "uploads", "two.jpg"), []byte("123"), 0o644)
	os.WriteFile(filepath.Join(root, "readme.md"), []byte("hi"), 0o644)
	os.WriteFile(outside, []byte("do not touch"), 0o644)
	os.Symlink(outside, filepath.Join(root, "escape"))
	return root, outside
}

func TestClean(t *testing.T) {
	for in, want := range map[string]string{"": ".", "/": ".", "uploads/a": "uploads/a", "/uploads/../readme.md": "readme.md", "../../etc/passwd": "etc/passwd"} {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestListFoldersFirst(t *testing.T) {
	root, _ := setup(t)
	list, truncated, err := List(root, "/")
	if err != nil || truncated {
		t.Fatal(err, truncated)
	}
	var names []string
	for _, e := range list {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "uploads,escape,readme.md" {
		t.Errorf("order = %v, want the folder first", names)
	}
}

func TestNothingOutsideTheRoot(t *testing.T) {
	root, outside := setup(t)
	if _, f, _, err := Open(root, "escape"); err == nil {
		f.Close()
		t.Error("a symbolic link led outside the folder")
	}
	if _, f, _, err := Open(root, "../secret.txt"); err == nil {
		f.Close()
		t.Error(".. led outside the folder")
	}
	if err := Remove(root, "/"); err == nil {
		t.Error("the folder itself was removed")
	}
	Remove(root, "escape") // removes the link, never what it points to
	if _, err := os.Stat(outside); err != nil {
		t.Error("removing a link deleted its target outside the folder")
	}
}

func TestUsageRemoveAndSave(t *testing.T) {
	root, _ := setup(t)
	files, bytes, more, err := Usage(root, "uploads")
	if err != nil || files != 2 || bytes != 8 || more {
		t.Errorf("usage = %d files %d bytes more %v err %v", files, bytes, more, err)
	}
	if err := Remove(root, "uploads"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "uploads")); !os.IsNotExist(err) {
		t.Error("the folder is still there")
	}
	if n, err := Save(root, "/", "new.txt", false, strings.NewReader("hello")); err != nil || n != 5 {
		t.Fatal(n, err)
	}
	if _, err := Save(root, "/", "new.txt", false, strings.NewReader("again")); err == nil {
		t.Error("an existing file was replaced without asking")
	}
	if _, err := Save(root, "/", "new.txt", true, strings.NewReader("again")); err != nil {
		t.Error(err)
	}
	if _, err := Save(root, "/", "../x.txt", false, strings.NewReader("")); err == nil {
		t.Error("a name with a path was accepted")
	}
}

func TestListEmptyFolder(t *testing.T) {
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, "empty"), 0o755)
	list, truncated, err := List(root, "empty")
	if err != nil || truncated || len(list) != 0 {
		t.Errorf("empty folder: %v %v %v", list, truncated, err)
	}
}
