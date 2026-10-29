package setup

import (
	"bufio"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// IgnoreLines are the .gitignore lines every PBIP repository needs: Desktop's
// local state and data cache, and binary report files, none of which can be
// scanned (principle 8).
var IgnoreLines = []string{".pbi/", "*.pbix", "*.abf"}

// ignoreHeader introduces the lines AppendIgnores adds.
const ignoreHeader = "# Power BI Desktop's local state and data cache, and binary report files.\n"

// MissingIgnores lists the IgnoreLines that the .gitignore at the root of the
// repository at root does not have. A line counts when it is written the
// same way, with or without a leading "/" or "**/". Ignores elsewhere (a
// nested .gitignore, .git/info/exclude, a global file) are not consulted, so
// a line may be listed that git already ignores another way; adding it is
// harmless.
func MissingIgnores(root string) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(root, ".gitignore")) //nolint:gosec // G304: the repository's own .gitignore
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	have := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		line = strings.TrimPrefix(line, "**/")
		line = strings.TrimPrefix(line, "/")
		have[line] = true
		if line == ".pbi" {
			have[".pbi/"] = true
		}
	}
	var missing []string
	for _, l := range IgnoreLines {
		if !have[l] {
			missing = append(missing, l)
		}
	}
	return missing, nil
}

// AppendIgnores appends lines to the .gitignore at the root of the
// repository at root, creating it if needed.
func AppendIgnores(root string, lines []string) error {
	if len(lines) == 0 {
		return nil
	}
	path := filepath.Join(root, ".gitignore")
	old, err := os.ReadFile(path) //nolint:gosec // G304: the repository's own .gitignore
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	var b strings.Builder
	if len(old) > 0 {
		if !bytes.HasSuffix(old, []byte("\n")) {
			b.WriteByte('\n')
		}
		b.WriteByte('\n')
	}
	b.WriteString(ignoreHeader)
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644) //nolint:gosec // G302,G304: a repository file every collaborator reads
	if err != nil {
		return err
	}
	if _, err := f.WriteString(b.String()); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
