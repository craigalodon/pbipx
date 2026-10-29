package corpus

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/craigalodon/pbipx/internal/approval"
)

// The fixed synthetic identity and date of a case's commit. The date is
// 2100-01-01T00:00:00Z as a raw timestamp: git rejects a calendar date in
// 2100 or later.
const (
	GitName  = "Corpus Runner"
	GitEmail = "corpus-runner@example.com"
	GitDate  = "@4102444800 +0000"
)

// rootPlaceholder is replaced with the root's absolute path in args and stdin.
const rootPlaceholder = "{root}"

// stdinPath is the path of a finding read from standard input.
const stdinPath = "-"

// fingerprintPlaceholder is replaced in args with the fingerprint of the
// finding [fingerprint] describes.
const fingerprintPlaceholder = "{fingerprint}"

// commandTimeout bounds one run of the binary.
const commandTimeout = 2 * time.Minute

// redactionEnv is unset for every command unless a case's env sets it.
var redactionEnv = []string{"CI", "PRE_COMMIT", "PBIPX_NO_REVEAL"}

// configDirEnv is the variable that points pbipx at its configuration
// directory, where policy approvals are kept (ADR-0006). The runner sets it
// for every command to the case's own directory.
const configDirEnv = approval.EnvConfigDir

// Prepared is a case's root, ready to run in.
type Prepared struct {
	Root string
	// ConfigDir is the case's private configuration directory, outside the
	// root: PBIPX_CONFIG_DIR for every command the case runs.
	ConfigDir string
	// Fingerprint is the resolved fingerprint of the case's [fingerprint],
	// empty when it has none.
	Fingerprint string
}

// Prepare builds a case's root (corpus.md, "Running a case", steps 1 to 3,
// "Baseline cases" and "Policy approval"): it copies tree/ (a case without
// one starts from an empty root) and writes the policy. A gate case then has
// its policy approved in configDir, unless approved = false. A case with
// [[baseline]] or [fingerprint] has its findings resolved by a scan with
// binary and its baseline written. Then after/tree/ is copied over the
// root, and last, unless git = false, the root is committed. root and
// configDir must be empty directories, configDir outside root; binary is
// needed only to resolve findings.
func (c *Corpus) Prepare(cs *Case, root, configDir, binary string) (*Prepared, error) {
	if configDir == "" || !filepath.IsAbs(configDir) {
		return nil, fmt.Errorf("%s: the configuration directory must be an absolute path", cs.ID())
	}
	p := &Prepared{Root: root, ConfigDir: configDir}
	if cs.HasTree {
		if err := copyTree(cs.TreeDir(), root); err != nil {
			return nil, fmt.Errorf("%s: copying tree/: %w", cs.ID(), err)
		}
	}
	if cs.Spec.Policy != policyInTree {
		data, err := readIn(filepath.Join(c.Dir, policiesDir), cs.Spec.Policy+".toml")
		if err != nil {
			return nil, fmt.Errorf("%s: reading the policy: %w", cs.ID(), err)
		}
		if err := writeIn(root, policyFile, data); err != nil {
			return nil, fmt.Errorf("%s: writing the policy: %w", cs.ID(), err)
		}
	}
	if cs.Spec.Approves() {
		if err := Approve(root, configDir); err != nil {
			return nil, fmt.Errorf("%s: %w", cs.ID(), err)
		}
	}
	if cs.Spec.NeedsResolve() {
		if err := c.resolve(cs, binary, p); err != nil {
			return nil, fmt.Errorf("%s: %w", cs.ID(), err)
		}
	}
	if cs.HasAfter {
		if err := copyTree(cs.AfterTreeDir(), root); err != nil {
			return nil, fmt.Errorf("%s: copying after/tree/: %w", cs.ID(), err)
		}
	}
	if cs.Spec.UsesGit() {
		if err := GitSetup(root, cs.Spec.Untracked, cs.Spec.Staged); err != nil {
			return nil, fmt.Errorf("%s: %w", cs.ID(), err)
		}
	}
	return p, nil
}

// Approve records the policy at root/.pbipx.toml as approved for root in
// the store at configDir, as `pbipx policy approve` would, with the case's
// fixed date. A root without a policy file has nothing to approve: the gate
// denies it with policy-error first.
func Approve(root, configDir string) error {
	digest, err := approval.DigestFile(filepath.Join(root, policyFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("approving the policy: %w", err)
	}
	store := &approval.Store{Dir: configDir}
	if err := store.Approve(root, digest, time.Unix(caseTime, 0)); err != nil {
		return fmt.Errorf("approving the policy: %w", err)
	}
	return nil
}

// caseTime is GitDate as a Unix time: the approval date of every case.
const caseTime = 4102444800

// GitSetup makes root a git repository and commits every file in it that git
// would add (a file the tree's own .gitignore ignores stays out), except
// untracked (left untracked) and staged (added to the index after the
// commit, even if ignored). The commit has a fixed synthetic author,
// committer and date, and no user or system git configuration takes part.
func GitSetup(root string, untracked, staged []string) error {
	steps := [][]string{
		{"init", "--quiet", "--initial-branch=main"},
		{"add", "--all", "--", "."},
	}
	if held := slices.Concat(untracked, staged); len(held) > 0 {
		steps = append(steps, slices.Concat([]string{"rm", "--cached", "--quiet", "-r", "--ignore-unmatch", "--"}, held))
	}
	steps = append(steps, []string{"commit", "--quiet", "--allow-empty", "--no-verify", "--message", "corpus case"})
	if len(staged) > 0 {
		steps = append(steps, slices.Concat([]string{"add", "--force", "--"}, staged))
	}
	for _, args := range steps {
		cmd := exec.Command("git", slices.Concat(gitConfigArgs, args)...) //nolint:gosec // G204: fixed git subcommands; the only operands are a case's validated tree paths
		cmd.Dir = root
		cmd.Env = GitEnv(os.Environ())
		// git's output is not shown: it can quote a path or a ref.
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("git %s: %w", args[0], err)
		}
	}
	return nil
}

// gitConfigArgs pin the configuration a commit depends on.
var gitConfigArgs = []string{
	"-c", "core.autocrlf=false",
	"-c", "core.hooksPath=" + os.DevNull,
	"-c", "commit.gpgsign=false",
	"-c", "user.name=" + GitName,
	"-c", "user.email=" + GitEmail,
}

// GitEnv is base without any GIT_ variable, which could point git at another
// repository (a pre-commit hook sets GIT_INDEX_FILE), plus the fixed identity
// and date, and with user and system configuration turned off.
func GitEnv(base []string) []string {
	env := slices.DeleteFunc(slices.Clone(base), func(kv string) bool {
		return strings.HasPrefix(kv, "GIT_")
	})
	return append(env,
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_AUTHOR_NAME="+GitName,
		"GIT_AUTHOR_EMAIL="+GitEmail,
		"GIT_AUTHOR_DATE="+GitDate,
		"GIT_COMMITTER_NAME="+GitName,
		"GIT_COMMITTER_EMAIL="+GitEmail,
		"GIT_COMMITTER_DATE="+GitDate,
	)
}

// CommandEnv is the environment the binary runs in: GitEnv(base) without
// CI, PRE_COMMIT and PBIPX_NO_REVEAL, with PBIPX_CONFIG_DIR set to
// configDir, then the case's env.
func CommandEnv(base []string, spec *Spec, configDir string) []string {
	env := slices.DeleteFunc(GitEnv(base), func(kv string) bool {
		name, _, _ := strings.Cut(kv, "=")
		if slices.Contains(redactionEnv, name) || name == configDirEnv {
			return true
		}
		_, set := spec.Env[name]
		return set
	})
	env = append(env, configDirEnv+"="+configDir)
	for _, k := range sortedKeys(spec.Env) {
		env = append(env, k+"="+spec.Env[k])
	}
	return env
}

// CommandArgs is the binary's command line for a prepared case: the command,
// the case's args with {root} and {fingerprint} replaced, and, for a case
// other than the gate that has an expected.json, --format json.
func CommandArgs(cs *Case, p *Prepared) []string {
	args := []string{cs.Spec.Command}
	for _, a := range cs.Spec.Args {
		a = strings.ReplaceAll(a, rootPlaceholder, p.Root)
		args = append(args, strings.ReplaceAll(a, fingerprintPlaceholder, p.Fingerprint))
	}
	if cs.Spec.Command != commandGate && cs.Spec.HasResult() {
		args = append(args, "--format", "json")
	}
	return args
}

// Outcome is what one run of the binary produced.
type Outcome struct {
	Exit   int
	Stdout []byte
	Stderr []byte
}

// Execute runs binary for a prepared case (steps 4 and 5).
func Execute(cs *Case, binary string, p *Prepared) (Outcome, error) {
	var stdin []byte
	if cs.HasStdin {
		stdin = bytes.ReplaceAll(cs.Stdin, []byte(rootPlaceholder), []byte(p.Root))
	}
	out, err := runBinary(binary, p.Root, CommandArgs(cs, p), CommandEnv(os.Environ(), &cs.Spec, p.ConfigDir), stdin)
	if err != nil {
		return out, fmt.Errorf("%s: %w", cs.ID(), err)
	}
	return out, nil
}

// runBinary runs binary in dir and returns what it produced.
func runBinary(binary, dir string, args, env []string, stdin []byte) (Outcome, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, args...) //nolint:gosec // G204: the binary is the one the test built
	cmd.Dir = dir
	cmd.Env = env
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var out Outcome
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	out.Stdout, out.Stderr = stdout.Bytes(), stderr.Bytes()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr) && exitErr.ExitCode() >= 0:
		out.Exit = exitErr.ExitCode()
	default:
		return out, fmt.Errorf("running the binary: %w", err)
	}
	return out, nil
}

// Check compares an outcome with the case (step 6): the exit code, that no
// sentinel appears in stdout or stderr, then the normalized result. Problems
// name the case and the rule; output is never quoted, and a sentinel is
// named by its index.
func (c *Corpus) Check(cs *Case, out Outcome) []error {
	var errs []error
	addf := func(format string, args ...any) {
		errs = append(errs, fmt.Errorf("%s: %s", cs.ID(), fmt.Sprintf(format, args...)))
	}
	if out.Exit != cs.Spec.Exit {
		addf("exit = %d, want %d", out.Exit, cs.Spec.Exit)
	}
	for i, s := range cs.Spec.Sentinels {
		for _, stream := range []struct {
			name string
			data []byte
		}{{"stdout", out.Stdout}, {"stderr", out.Stderr}} {
			if containsSentinel(stream.data, s) {
				addf("sentinels[%d] appears in %s", i, stream.name)
			}
		}
	}
	if cs.Expected == nil {
		return errs
	}
	var actual *Result
	if cs.Spec.Command == commandGate {
		res, err := GateResult(out.Exit, out.Stdout, out.Stderr)
		if err != nil {
			addf("%v", err)
			return errs
		}
		actual = res
	} else {
		res, problems := ParseResult(out.Stdout, cs.Spec.Command, c.Vocab, false)
		if len(problems) > 0 {
			addf("stdout is not a JSON result: %s", strings.Join(problems, "; "))
			return errs
		}
		actual = res
	}
	for _, d := range CompareResults(cs.Expected, actual) {
		addf("%s", d)
	}
	return errs
}

// denyClass is how pbipx starts a deny reason: "pbipx: <class>:".
var denyClass = regexp.MustCompile(`^pbipx: ([a-z][a-z0-9-]*):`)

// GateResult normalizes a gate run. Exit 0 is an allow; exit 2 a deny, whose
// class is read from the first line of stderr, "pbipx: <class>:", which is
// authoritative: Claude Code ignores stdout when a hook exits 2. Only when
// stderr has no class is it read from permissionDecisionReason in the hook
// JSON on stdout.
func GateResult(exit int, stdout, stderr []byte) (*Result, error) {
	switch exit {
	case 0:
		return &Result{Decision: decisionAllow}, nil
	case 2:
	default:
		return nil, fmt.Errorf("the gate exited %d, neither 0 (allow) nor 2 (deny)", exit)
	}
	var hook struct {
		HookSpecificOutput struct {
			PermissionDecision       string `json:"permissionDecision"`
			PermissionDecisionReason string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	line, _, _ := strings.Cut(string(stderr), "\n")
	if m := denyClass.FindStringSubmatch(line); m != nil {
		return &Result{Decision: decisionDeny, Reason: m[1]}, nil
	}
	if len(bytes.TrimSpace(stdout)) == 0 {
		return nil, errors.New("the deny has no reason: stderr does not start with \"pbipx: <class>:\" and stdout is empty")
	}
	if json.Unmarshal(stdout, &hook) != nil {
		return nil, errors.New("the gate's stdout is not hook JSON")
	}
	if d := hook.HookSpecificOutput.PermissionDecision; d != decisionDeny {
		return nil, errors.New("the gate exited 2, but its hook JSON is not a deny")
	}
	m := denyClass.FindStringSubmatch(hook.HookSpecificOutput.PermissionDecisionReason)
	if m == nil {
		return nil, errors.New("the deny reason does not start with \"pbipx: <class>:\"")
	}
	return &Result{Decision: decisionDeny, Reason: m[1]}, nil
}

// writeIn writes a file by a name that must stay inside dir.
func writeIn(dir, name string, data []byte) error {
	r, err := os.OpenRoot(dir)
	if err != nil {
		return err
	}
	defer r.Close()
	return r.WriteFile(name, data, 0o600)
}
