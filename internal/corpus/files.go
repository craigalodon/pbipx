package corpus

import (
	"fmt"
	"io/fs"
	"os"
)

// copyTree copies the files, directories and symbolic links under src into
// dst, keeping the executable bit. dst is created if it does not exist.
func copyTree(src, dst string) error {
	if err := os.MkdirAll(dst, 0o750); err != nil {
		return err
	}
	from, err := os.OpenRoot(src)
	if err != nil {
		return err
	}
	defer from.Close()
	to, err := os.OpenRoot(dst)
	if err != nil {
		return err
	}
	defer to.Close()
	return fs.WalkDir(from.FS(), ".", func(rel string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		switch {
		case d.IsDir():
			return to.MkdirAll(rel, 0o750)
		case d.Type()&fs.ModeSymlink != 0:
			link, err := from.Readlink(rel)
			if err != nil {
				return err
			}
			return to.Symlink(link, rel)
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}
			data, err := from.ReadFile(rel)
			if err != nil {
				return err
			}
			return to.WriteFile(rel, data, info.Mode().Perm()|0o600)
		default:
			return fmt.Errorf("unsupported file type at %s", rel)
		}
	})
}

// readIn reads a file by a name that must stay inside dir.
func readIn(dir, name string) ([]byte, error) {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return r.ReadFile(name)
}
