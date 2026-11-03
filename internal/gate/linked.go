package gate

import (
	"os"
	"path/filepath"
	"strings"
)

// linkedWorktrees are the other work trees of the clone whose work tree
// is root, as git worktree list reports them, read from the files git
// keeps without running git: the main work tree and every linked one but
// root. The common git directory (root/.git, or the one a linked work
// tree's .git file and commondir point at) holds worktrees/*/gitdir, each
// a linked work tree's .git file; the main work tree is the common dir's
// parent (a non-bare <main>/.git), or core.worktree in the common config.
// A file that cannot be read is skipped.
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
	var out []string
	add := func(wt string) {
		if wt != "" && filepath.Clean(wt) != filepath.Clean(root) {
			out = append(out, wt)
		}
	}
	add(mainWorktree(common))
	entries, err := os.ReadDir(filepath.Join(common, "worktrees"))
	if err != nil {
		return out
	}
	for _, e := range entries {
		dir := filepath.Join(common, "worktrees", e.Name())
		data, err := os.ReadFile(filepath.Join(dir, "gitdir")) //nolint:gosec // G304: git's own file
		if err != nil {
			continue
		}
		add(filepath.Dir(absFrom(dir, strings.TrimSpace(string(data)))))
	}
	return out
}

// mainWorktree is the main work tree of the clone whose common git
// directory is common: the parent of a non-bare <main>/.git, or
// core.worktree read from the common config. "" for a bare repository.
func mainWorktree(common string) string {
	if filepath.Base(common) == ".git" {
		return filepath.Dir(common)
	}
	data, err := os.ReadFile(filepath.Join(common, "config")) //nolint:gosec // G304: git's own config
	if err != nil {
		return ""
	}
	if wt := configWorktree(string(data)); wt != "" {
		return absFrom(common, wt)
	}
	return ""
}

// configWorktree is the core.worktree value in a git config file's text,
// "" when unset. It reads the [core] section only, the value unquoted.
func configWorktree(text string) string {
	inCore := false
	for _, line := range strings.Split(text, "\n") {
		l := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(l, "[") && strings.Contains(l, "]"):
			head := strings.ToLower(strings.TrimSpace(l[1:strings.IndexByte(l, ']')]))
			inCore = head == "core"
		case inCore:
			if k, v, ok := strings.Cut(l, "="); ok && strings.EqualFold(strings.TrimSpace(k), "worktree") {
				v = strings.TrimSpace(v)
				v = strings.Trim(v, `"`)
				return v
			}
		}
	}
	return ""
}

// absFrom is p, absolute, or relative to dir.
func absFrom(dir, p string) string {
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Join(dir, p)
}
