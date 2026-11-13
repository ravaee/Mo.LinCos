// Package files reads and changes files inside one folder, such as a folder or
// volume bound into a container. Every operation goes through os.Root, so no
// path, ".." or symbolic link can reach outside that folder.
package files

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"time"
)

// Entry is one file or folder in a listing.
type Entry struct {
	Name string    `json:"name"`
	Dir  bool      `json:"dir"`
	Link bool      `json:"link,omitempty"`
	Size int64     `json:"size"`
	Mod  time.Time `json:"mod"`
	Mode string    `json:"mode"`
}

// MaxList is how many entries a listing returns; bigger folders are cut off.
const MaxList = 5000

// Clean turns a path from the page into one relative to the folder: "" and "/"
// are the folder itself.
func Clean(rel string) string {
	c := strings.TrimPrefix(path.Clean("/"+rel), "/")
	if c == "" {
		return "."
	}
	return c
}

// List returns a folder's entries, folders first, then by name.
func List(root, rel string) (entries []Entry, truncated bool, err error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, false, err
	}
	defer r.Close()
	f, err := r.Open(Clean(rel))
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	list, err := f.ReadDir(MaxList + 1)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, err
	}
	if len(list) > MaxList {
		list, truncated = list[:MaxList], true
	}
	for _, d := range list {
		info, err := d.Info()
		if err != nil {
			continue
		}
		e := Entry{Name: d.Name(), Dir: d.IsDir(), Size: info.Size(), Mod: info.ModTime(), Mode: info.Mode().String()}
		if info.Mode()&fs.ModeSymlink != 0 {
			e.Link = true
			// A link to a folder inside the root opens as a folder
			if st, err := r.Stat(path.Join(Clean(rel), d.Name())); err == nil && st.IsDir() {
				e.Dir = true
			}
		}
		if e.Dir {
			e.Size = 0
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return entries, truncated, nil
}

// Open opens a file for reading. The caller closes the root and the file.
func Open(root, rel string) (*os.Root, *os.File, fs.FileInfo, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, nil, nil, err
	}
	f, err := r.Open(Clean(rel))
	if err != nil {
		r.Close()
		return nil, nil, nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		r.Close()
		return nil, nil, nil, err
	}
	if info.IsDir() {
		f.Close()
		r.Close()
		return nil, nil, nil, errors.New("is a folder")
	}
	return r, f, info, nil
}

// Usage counts the files and bytes under a path, so a delete can say what it
// will remove. It stops counting at a limit and says so.
func Usage(root, rel string) (files int, bytes int64, more bool, err error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return 0, 0, false, err
	}
	defer r.Close()
	const limit = 100000
	err = fs.WalkDir(r.FS(), Clean(rel), func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if files >= limit {
			more = true
			return fs.SkipAll
		}
		if !d.IsDir() {
			files++
			if info, err := d.Info(); err == nil {
				bytes += info.Size()
			}
		}
		return nil
	})
	return files, bytes, more, err
}

// Remove deletes a file, or a folder with everything in it. The folder itself
// can't be removed.
func Remove(root, rel string) error {
	c := Clean(rel)
	if c == "." {
		return errors.New("the folder itself can't be deleted, only what is in it")
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return err
	}
	defer r.Close()
	if _, err := r.Lstat(c); err != nil {
		return err
	}
	return r.RemoveAll(c)
}

// Save writes a new file into a folder from r. An existing file is replaced
// only when replace is set.
func Save(root, dir, name string, replace bool, src io.Reader) (int64, error) {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\\x00") {
		return 0, errors.New("not a file name: " + name)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return 0, err
	}
	defer r.Close()
	flags := os.O_WRONLY | os.O_CREATE | os.O_EXCL
	if replace {
		flags = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	}
	target := path.Join(Clean(dir), name)
	f, err := r.OpenFile(target, flags, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return 0, errors.New(name + " already exists")
		}
		return 0, err
	}
	n, err := io.Copy(f, src)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		r.Remove(target) // leave no half-written file behind
	}
	return n, err
}
