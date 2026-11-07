package panel

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// mountedFile is a path in the repository that a stack service mounts.
type mountedFile struct {
	From string `json:"from"`
	To   string `json:"to"`
	// Content is written to To instead of copying From, for a config written in the Compose file.
	Content *string `json:"content,omitempty"`
}

// mountsFromRepo says whether any of a service's mounted files is copied from its repository, which the deploy then has to clone.
func mountsFromRepo(encoded string) bool {
	var files []mountedFile
	if json.Unmarshal([]byte(encoded), &files) != nil {
		return encoded != ""
	}
	return slices.ContainsFunc(files, func(f mountedFile) bool { return f.Content == nil })
}

const mountedDir = ".skiff-mounts"

// prepareMountedFiles builds a service's mounted files into its image, on top of the image or Dockerfile it asked for, so replicas and rollbacks get them without reaching into the server's disk. work is the checkout and ctxDir the folder being built.
func prepareMountedFiles(src *Source, work, ctxDir string) error {
	var files []mountedFile
	if err := json.Unmarshal([]byte(src.MountedFiles), &files); err != nil {
		return errors.New("couldn't read this service's mounted files")
	}
	base := "FROM " + src.Image + "\n"
	if src.Image == "" {
		name := src.Dockerfile
		if name == "" {
			name = "Dockerfile"
		}
		path := filepath.Join(ctxDir, filepath.Clean("/"+name))
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("%s mounts files from the repository, which needs a Dockerfile at %s", src.Service, name)
		}
		base = string(data) + "\n"
	}
	dir := filepath.Join(ctxDir, mountedDir)
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString(base)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for i, f := range files {
		if f.Content != nil {
			copied := filepath.Join(mountedDir, fmt.Sprint(i), path.Base(f.To))
			if err := os.MkdirAll(filepath.Dir(filepath.Join(ctxDir, copied)), 0o755); err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(ctxDir, copied), []byte(*f.Content), 0o644); err != nil {
				return err
			}
			line, err := json.Marshal([]string{filepath.ToSlash(copied), f.To})
			if err != nil {
				return err
			}
			fmt.Fprintf(&b, "COPY %s\n", line)
			continue
		}
		from := filepath.Join(work, filepath.Clean("/"+f.From))
		info, err := os.Lstat(from)
		if err != nil {
			return fmt.Errorf("%s mounts %s, which isn't in the repository any more", src.Service, f.From)
		}
		copied := filepath.Join(mountedDir, fmt.Sprint(i))
		if info.IsDir() {
			err = copyMounted(from, filepath.Join(ctxDir, copied))
		} else {
			copied = filepath.Join(copied, filepath.Base(from))
			if err = os.MkdirAll(filepath.Dir(filepath.Join(ctxDir, copied)), 0o755); err == nil {
				err = copyMountedFile(from, filepath.Join(ctxDir, copied), info)
			}
		}
		if err != nil {
			return fmt.Errorf("couldn't copy %s: %w", f.From, err)
		}
		line, err := json.Marshal([]string{filepath.ToSlash(copied), f.To})
		if err != nil {
			return err
		}
		fmt.Fprintf(&b, "COPY %s\n", line)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(b.String()), 0o644); err != nil {
		return err
	}
	src.Image, src.Dockerfile = "", filepath.ToSlash(filepath.Join(mountedDir, "Dockerfile"))
	return nil
}

// copyMounted copies a folder, leaving out what must never reach an image: .git can hold the credentials a repository was cloned with, and skiff.toml an app's secrets.
func copyMounted(from, to string) error {
	return filepath.WalkDir(from, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == ".git" || d.Name() == mountedDir || d.Name() == "skiff.toml" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		}
		return copyMountedFile(path, target, info)
	})
}

func copyMountedFile(from, to string, info fs.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		// checkSymlinks has already refused any link that leaves the checkout.
		link, err := os.Readlink(from)
		if err != nil {
			return err
		}
		return os.Symlink(link, to)
	}
	if !info.Mode().IsRegular() {
		return nil
	}
	data, err := os.ReadFile(from)
	if err != nil {
		return err
	}
	return os.WriteFile(to, data, info.Mode().Perm())
}

// stackAppTouched says whether a push changes what a stack service builds: its own folder, or a file it mounts from elsewhere in the repository.
func stackAppTouched(src Source, paths []string) bool {
	if src.Image == "" && touched(paths, src.RootDir) {
		return true
	}
	var files []mountedFile
	if src.MountedFiles != "" && json.Unmarshal([]byte(src.MountedFiles), &files) != nil {
		return true
	}
	for _, f := range files {
		if touched(paths, f.From) {
			return true
		}
	}
	return false
}
