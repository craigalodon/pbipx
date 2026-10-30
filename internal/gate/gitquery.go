package gate

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"
)

// The gate asks git questions about the repository a command runs in:
// what an object name resolves to, what an alias or a setting is, which
// repository a directory belongs to, and which files a diff, log or grep
// would print content from. Each is one git command, run with an argv
// (never a shell), standard input empty and standard error discarded,
// within a time limit. Its output is read here and never put in a message:
// a type name, a setting or a file's path decides, and nothing else leaves
// this file.

const (
	// gitCallTimeout bounds one git command the gate runs.
	gitCallTimeout = 10 * time.Second
	// gitDecisionBudget bounds every git command one decision runs.
	gitDecisionBudget = 30 * time.Second
	// gitOutputLimit bounds the output read from a question that is not
	// streamed (an object type, a setting, a directory).
	gitOutputLimit = 1 << 20
)

var (
	// errNotSet is a configuration key that is not set (git config exits 1).
	errNotSet = errors.New("not set")
	// errGitBudget is a decision that has used its time for git.
	errGitBudget = errors.New("the time for git is used up")
	// errGitOutput is output that is too long or cannot be read.
	errGitOutput = errors.New("git's output cannot be read")
)

// gitSession is the git commands one decision runs: they share one time
// budget.
type gitSession struct {
	deadline time.Time
	// common is this repository's common directory, resolved, once asked.
	common string
}

func newGitSession() *gitSession {
	return &gitSession{deadline: time.Now().Add(gitDecisionBudget)}
}

// gitEnv is the environment of every git command the gate runs: the gate's
// own, with no pager, no external diff, no lazy fetch from a promisor
// remote, no optional lock (git status and diff would otherwise refresh
// the index), and no prompt.
func gitEnv() []string {
	drop := map[string]bool{
		"GIT_PAGER": true, "PAGER": true, "GIT_EXTERNAL_DIFF": true, "GIT_NO_LAZY_FETCH": true,
		"GIT_OPTIONAL_LOCKS": true, "GIT_TERMINAL_PROMPT": true,
	}
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if !drop[name] {
			env = append(env, kv)
		}
	}
	return append(env, "GIT_PAGER=cat", "PAGER=cat", "GIT_NO_LAZY_FETCH=1",
		"GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
}

// run runs git with args in dir and hands its standard output to consume,
// which must read it to the end or return an error. It returns git's exit
// code. An error is anything but git running to completion within the
// time: a timeout, the budget used up, git missing, or consume failing
// (git is then killed).
func (g *gitSession) run(dir string, args []string, consume func(*bufio.Reader) error) (int, error) {
	left := time.Until(g.deadline)
	if left <= 0 {
		return -1, errGitBudget
	}
	ctx, cancel := context.WithTimeout(context.Background(), min(gitCallTimeout, left))
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // G204: git with an argv, never a shell; the words come from the command being judged, which runs them itself
	cmd.Dir = dir
	cmd.Env = gitEnv()
	cmd.Stdin = nil
	cmd.Stderr = nil
	cmd.WaitDelay = time.Second
	out, err := cmd.StdoutPipe()
	if err != nil {
		return -1, err
	}
	if err := cmd.Start(); err != nil {
		return -1, err
	}
	cerr := consume(bufio.NewReader(out))
	if cerr != nil {
		cancel()
	}
	werr := cmd.Wait()
	switch {
	case cerr != nil:
		return -1, cerr
	case ctx.Err() != nil:
		return -1, ctx.Err()
	case werr != nil:
		var exitErr *exec.ExitError
		if errors.As(werr, &exitErr) && exitErr.Exited() {
			return exitErr.ExitCode(), nil
		}
		return -1, werr
	}
	return 0, nil
}

// output runs git with args in dir and returns its standard output,
// trimmed of the final newline, and its exit code. err is set when git
// did not run to completion, or printed more than gitOutputLimit.
func (g *gitSession) output(dir string, args ...string) (string, int, error) {
	var buf bytes.Buffer
	exit, err := g.run(dir, args, func(r *bufio.Reader) error {
		n, err := io.Copy(&buf, io.LimitReader(r, gitOutputLimit+1))
		if err != nil || n > gitOutputLimit {
			return errGitOutput
		}
		return nil
	})
	return strings.TrimSuffix(buf.String(), "\n"), exit, err
}

// value is output for a question that must succeed: any exit but 0 is an
// error, and exit 1 of git config is errNotSet.
func (g *gitSession) value(dir string, args ...string) (string, error) {
	out, exit, err := g.output(dir, args...)
	switch {
	case err != nil:
		return "", err
	case exit == 1 && args[0] == "config":
		return "", errNotSet
	case exit != 0:
		return "", errors.New("git failed")
	}
	return out, nil
}

// objectType is what git cat-file -t says rev names in dir: "blob",
// "tree", "commit" or "tag", or "" when it names nothing git can resolve.
// A rev starting with "-" is refused, though --end-of-options would keep
// git from reading it as an option.
func (g *gitSession) objectType(dir, rev string) string {
	if rev == "" || strings.HasPrefix(rev, "-") {
		return ""
	}
	t, err := g.value(dir, "cat-file", "-t", "--end-of-options", rev)
	if err != nil {
		return ""
	}
	switch t {
	case "blob", "tree", "commit", "tag":
		return t
	}
	return ""
}

// configValue is the value of key in dir's configuration. err is errNotSet
// when the key is not set, and another error when git could not tell.
// typ, if not empty, is passed as --type.
func (g *gitSession) configValue(dir, key, typ string) (string, error) {
	if key == "" || strings.HasPrefix(key, "-") {
		return "", errors.New("not a key")
	}
	args := []string{"config", "--get"}
	if typ != "" {
		args = append(args, "--type="+typ)
	}
	return g.value(dir, append(args, key)...)
}

// repoInfo is which repository a directory is in.
type repoInfo struct {
	nongit bool   // not in any repository
	common string // the common directory, absolute
	top    string // the work tree's root, absolute; "" when dir is not in one
}

// repoAt says which repository dir is in, from git rev-parse.
func (g *gitSession) repoAt(dir string) (repoInfo, error) {
	out, exit, err := g.output(dir, "rev-parse", "--path-format=absolute",
		"--git-common-dir", "--is-inside-work-tree", "--show-toplevel")
	if err != nil {
		return repoInfo{}, err
	}
	lines := strings.Split(out, "\n")
	switch {
	case exit == 0 && len(lines) == 3 && lines[1] == "true" && lines[0] != "" && lines[2] != "":
		return repoInfo{common: lines[0], top: lines[2]}, nil
	case exit == 128 && len(lines) == 2 && lines[1] == "false" && lines[0] != "":
		// Inside a git directory, or a bare repository: there is no work
		// tree, so --show-toplevel fails.
		return repoInfo{common: lines[0]}, nil
	case exit == 128 && out == "":
		return repoInfo{nongit: true}, nil
	}
	return repoInfo{}, errors.New("git rev-parse gave no answer")
}

// commonDir is this repository's common directory (root is its work
// tree), resolved, asked once per decision.
func (g *gitSession) commonDir(root string) (string, error) {
	if g.common != "" {
		return g.common, nil
	}
	info, err := g.repoAt(root)
	if err != nil || info.nongit {
		return "", errors.New("the repository's git directory cannot be found")
	}
	g.common = resolve(info.common)
	return g.common, nil
}

// errNamed is a name git printed that is not allowed; errUnparsed is
// output that cannot be read as the form asked for.
var (
	errNamed    = errors.New("a path git names is not allowed")
	errUnparsed = errors.New("git's output cannot be parsed")
)

// nameRun is a git command run in a form that prints only file names: a
// diff, log or show with --name-status -z, or grep with -l -z.
type nameRun struct {
	argv []string
	grep bool // the output is grep -l -z: names only
	// combined is whether --combined-all-paths was given: a combined
	// status (one letter per parent) is followed by a path for each
	// parent and the result's.
	combined bool
	// dirRel is whether a path may be printed relative to the directory
	// git runs in rather than the top of its work tree.
	dirRel bool
}

// names runs r in dir and calls judge on each path it prints. It returns
// false when judge rejects one, when git fails or times out, or when the
// output does not parse: --name-status -z prints a status (a letter and a
// score, or one letter per parent of a merge) and then one path, or two for
// a rename or copy, or one per parent and the result's for a combined
// diff with --combined-all-paths; grep -l -z prints names. A path holding
// a control character is not parsed: git prints none with -z, and a
// stray color escape or line break would hide a path inside another.
func (g *gitSession) names(dir string, r nameRun, judge func(string) bool) bool {
	exit, err := g.run(dir, r.argv, func(br *bufio.Reader) error {
		want := 0 // paths still to read for the current status; 0 reads a status
		for {
			tok, err := br.ReadBytes(0)
			if errors.Is(err, io.EOF) {
				if len(tok) > 0 || want > 0 {
					return errUnparsed
				}
				return nil
			}
			if err != nil {
				return err
			}
			tok = tok[:len(tok)-1]
			if !printable(tok) {
				return errUnparsed
			}
			switch {
			case r.grep || want > 0:
				if !judge(string(tok)) {
					return errNamed
				}
				if want > 0 {
					want--
				}
			default:
				n, ok := statusPaths(string(tok), r.combined)
				if !ok {
					return errUnparsed
				}
				want = n
			}
		}
	})
	return err == nil && (exit == 0 || exit == 1)
}

// printable reports whether a name git printed is not empty and holds no
// control character.
func printable(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, c := range b {
		if c < 0x20 || c == 0x7f {
			return false
		}
	}
	return true
}

// statusPaths is how many paths follow a --name-status status: one letter
// with an optional score (two paths for a rename or copy), or, in a
// combined diff, one letter per parent.
func statusPaths(st string, combined bool) (int, bool) {
	letters := strings.TrimRight(st, "0123456789")
	if letters == "" || len(letters) > 16 {
		return 0, false
	}
	for _, c := range letters {
		if !strings.ContainsRune("ACDMRTUXB", c) {
			return 0, false
		}
	}
	score := len(st) - len(letters)
	switch {
	case len(letters) == 1 && score <= 3:
		if letters == "R" || letters == "C" {
			return 2, true
		}
		return 1, true
	case score == 0 && combined:
		return len(letters) + 1, true
	}
	return 0, false
}
