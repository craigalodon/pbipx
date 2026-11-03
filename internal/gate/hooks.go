package gate

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// indexHookNames are the hooks git runs on commands that change nothing a
// later git command prints: post-index-change (status refreshing the
// index, add) and reference-transaction (any ref update: tag, branch,
// fetch, update-ref). The hooks the working-tree rule counts already
// (gitHooks) run only on commands that are writers anyway.
var indexHookNames = []string{"post-index-change", "reference-transaction"}

// mayWrite reports whether v may write files, for the working-tree rule:
// it is not known to write nothing (writesNothing), or it is a git
// command in a repository with an index hook (indexHooks), which can
// write files whatever git command runs it.
func (w *walker) mayWrite(v view) bool {
	if !writesNothing(v) {
		return true
	}
	return (v.name == "git" || v.name == "hub") && w.s.indexHooks()
}

// indexHooks reports whether the repository has an index hook
// (indexHookNames) in its hooks directory: core.hooksPath (relative to
// the work tree's top), or the common git directory's hooks. A hook is a
// regular file, executable where the file system says so; a .sample is
// none. When the directory cannot be told, it is taken to have one. Asked
// once a decision: a stat per name.
func (s *setup) indexHooks() bool {
	if !s.hooksKnown {
		s.hooks, s.hooksKnown = s.findIndexHooks(), true
	}
	return s.hooks
}

func (s *setup) findIndexHooks() bool {
	if s.repo == nil {
		return false // no repository: git judges no tree here
	}
	var dir string
	switch v, err := s.git().configValue(s.repo.Root, "core.hooksPath", "path"); {
	case err == nil:
		dir = v
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(s.repo.Root, dir)
		}
	case errors.Is(err, errNotSet):
		common := s.commonDir()
		if common == "" {
			return true
		}
		dir = filepath.Join(common, "hooks")
	default:
		return true
	}
	for _, name := range indexHookNames {
		st, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		// Git for Windows runs a hook whatever its mode.
		if runtime.GOOS == "windows" || st.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}
