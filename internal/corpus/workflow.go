package corpus

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/craigalodon/pbipx/internal/approval"
)

// The two trees under a case's expected/ directory (corpus.md, "Stage and
// collect cases"): files that must exist, byte for byte, in the root and in
// the working-copy base.
const (
	expectedRoot = "root"
	expectedWork = "work"
)

// workPlaceholder is replaced in args and setup with the working-copy base.
const workPlaceholder = "{work}"

// statusLine is a `git status --porcelain` line: two status characters, a
// space, then the path.
var statusLine = regexp.MustCompile(`^[ MADRCU?!]{2} \S`)

// prepareWork makes the working-copy base: an empty directory outside the
// root and the configuration directory, with symbolic links resolved so
// that a link in its path means what the case says.
func prepareWork(configDir string) (string, error) {
	dir, err := os.MkdirTemp(filepath.Dir(configDir), "work-")
	if err != nil {
		return "", fmt.Errorf("making the working-copy base: %w", err)
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return "", fmt.Errorf("making the working-copy base: %w", err)
	}
	return dir, nil
}

// registerWorkingCopies makes each of a gate case's working-copies folder,
// under the working-copy base, and registers it in the store at configDir as
// `pbipx stage` will: the gate then protects it.
func registerWorkingCopies(cs *Case, configDir, work string) error {
	store := &approval.Store{Dir: configDir}
	for _, rel := range cs.Spec.WorkingCopies {
		dir := filepath.Join(work, filepath.FromSlash(rel))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("working-copies: making a folder: %w", err)
		}
		if err := store.AddWorkingCopy(approval.WorkingCopy{Path: dir}); err != nil {
			return fmt.Errorf("working-copies: registering a folder: %w", err)
		}
	}
	return nil
}

// withPlaceholders is spec with {root} and {work} replaced in its env
// values. The spec itself is not changed.
func withPlaceholders(spec *Spec, p *Prepared) *Spec {
	out := *spec
	out.Env = make(map[string]string, len(spec.Env))
	for k, v := range spec.Env {
		v = strings.ReplaceAll(v, rootPlaceholder, p.Root)
		out.Env[k] = strings.ReplaceAll(v, workPlaceholder, p.Work)
	}
	return &out
}

// SetupArgs is one setup command line with {root} and {work} replaced.
func SetupArgs(args []string, p *Prepared) []string {
	out := make([]string, len(args))
	for i, a := range args {
		a = strings.ReplaceAll(a, rootPlaceholder, p.Root)
		out[i] = strings.ReplaceAll(a, workPlaceholder, p.Work)
	}
	return out
}

// prepareWorkflow runs a stage or collect case's setup commands in the root,
// then lays after/tree/ over the working-copy base (Desktop's save, or a
// folder that was already there) and removes work-deleted paths.
func (c *Corpus) prepareWorkflow(cs *Case, binary string, p *Prepared) error {
	env := CommandEnv(os.Environ(), withPlaceholders(&cs.Spec, p), p.ConfigDir)
	for i, line := range cs.Spec.Setup {
		out, err := runBinary(binary, p.Root, SetupArgs(line, p), env, nil)
		if err != nil {
			return fmt.Errorf("setup[%d]: %w", i, err)
		}
		if out.Exit != 0 {
			return fmt.Errorf("setup[%d] exited %d, want 0", i, out.Exit)
		}
	}
	if cs.HasAfter {
		if err := copyTree(cs.AfterTreeDir(), p.Work); err != nil {
			return fmt.Errorf("copying after/tree/ over the working copy: %w", err)
		}
	}
	for _, rel := range cs.Spec.WorkDeleted {
		if err := os.Remove(filepath.Join(p.Work, filepath.FromSlash(rel))); err != nil {
			return fmt.Errorf("work-deleted: removing a path: %w", err)
		}
	}
	return nil
}

// CheckTrees compares the root and the working-copy base with the case's
// expectations, after the command ran: the files of expected/root and
// expected/work, byte for byte; the paths that must be absent; and the
// root's git status. A key the case omits is not checked. Problems name the
// case and a path, never file content.
func CheckTrees(cs *Case, p *Prepared) []error {
	var errs []error
	addf := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", cs.ID(), fmt.Sprintf(format, args...)))
	}
	for _, t := range []struct{ name, base string }{{expectedRoot, p.Root}, {expectedWork, p.Work}} {
		dir := filepath.Join(cs.Dir, expectedTree, t.name)
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() {
				return err
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			want, err := os.ReadFile(path) //nolint:gosec // G304: a file under the case directory, found by walking it
			if err != nil {
				return err
			}
			label := expectedTree + "/" + t.name + "/" + filepath.ToSlash(rel)
			got, err := os.ReadFile(filepath.Join(t.base, rel)) //nolint:gosec // G304: a path under the root or working copy, from a walk of expected/
			switch {
			case errors.Is(err, fs.ErrNotExist):
				addf("%s is missing", label)
			case err != nil:
				addf("%s cannot be read", label)
			case !bytes.Equal(got, want):
				addf("%s differs", label)
			}
			return nil
		})
		if err != nil {
			addf("expected/%s cannot be read", t.name)
		}
	}
	for _, t := range []struct {
		key   string
		base  string
		paths []string
	}{{"expect-root-absent", p.Root, cs.Spec.ExpectRootAbsent}, {"expect-work-absent", p.Work, cs.Spec.ExpectWorkAbsent}} {
		for _, rel := range t.paths {
			if _, err := os.Lstat(filepath.Join(t.base, filepath.FromSlash(rel))); err == nil {
				addf("%s: %s exists", t.key, rel)
			}
		}
	}
	if cs.Spec.Status != nil {
		got, err := gitStatus(p.Root)
		if err != nil {
			addf("status: %v", err)
			return errs
		}
		want := slices.Clone(*cs.Spec.Status)
		for _, line := range got {
			if i := slices.Index(want, line); i >= 0 {
				want = slices.Delete(want, i, i+1)
				continue
			}
			addf("status: unexpected line %q", line)
		}
		for _, line := range want {
			addf("status: missing line %q", line)
		}
	}
	return errs
}

// gitStatus is the root's `git status --porcelain` lines, with every
// untracked file listed, as the runner's git runs: no configuration of the
// user's takes part.
func gitStatus(root string) ([]string, error) {
	args := slices.Concat(gitConfigArgs, []string{"status", "--porcelain", "--untracked-files=all"})
	cmd := exec.Command("git", args...) //nolint:gosec // G204: a fixed git subcommand
	cmd.Dir = root
	cmd.Env = GitEnv(os.Environ())
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New("git status failed")
	}
	var lines []string
	for _, l := range strings.Split(string(out), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines, nil
}
