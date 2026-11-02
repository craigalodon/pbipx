package gate

import (
	"os"
	"path/filepath"
	"strings"
)

// linkedWorktrees are the linked work trees of the clone whose work tree
// is root, as git worktree list reports them, read from the files git
// keeps without running git: the common git directory (root/.git, or the
// one a linked work tree's .git file and commondir point at) holds
// worktrees/*/gitdir, each the path of a work tree's .git file. root
// itself is left out; a file that cannot be read is skipped.
func linkedWorktrees(root string) []string {
	common := filepath.Join(root, ".git")
	st, err := os.Stat(common)
	if err != nil {
		return nil
	}
	if !st.IsDir() {
		data, err := os.ReadFile(common) //nolint:gosec // G304: a work tree's own .git file
		if err != nil {
			return nil
		}
		gitdir, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir:")
		if !ok {
			return nil
		}
		gitdir = absFrom(root, strings.TrimSpace(gitdir))
		cd, err := os.ReadFile(filepath.Join(gitdir, "commondir")) //nolint:gosec // G304: git's own file
		if err != nil {
			return nil
		}
		common = absFrom(gitdir, strings.TrimSpace(string(cd)))
	}
	entries, err := os.ReadDir(filepath.Join(common, "worktrees"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		dir := filepath.Join(common, "worktrees", e.Name())
		data, err := os.ReadFile(filepath.Join(dir, "gitdir")) //nolint:gosec // G304: git's own file
		if err != nil {
			continue
		}
		dotGit := absFrom(dir, strings.TrimSpace(string(data)))
		if wt := filepath.Dir(dotGit); filepath.Clean(wt) != filepath.Clean(root) {
			out = append(out, wt)
		}
	}
	return out
}

// absFrom is p, absolute, or relative to dir.
func absFrom(dir, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(dir, p)
}
