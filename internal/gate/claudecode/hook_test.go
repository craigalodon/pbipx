package claudecode_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/craigalodon/pbipx/internal/approval"
	"github.com/craigalodon/pbipx/internal/gate"
	"github.com/craigalodon/pbipx/internal/gate/claudecode"
)

// These tests run the configured hook command string, end to end, as Claude
// Code would: through the shell, with the event on stdin (principle 7). A
// unit test of the gate's code cannot catch a hook that never runs it.

// sentinel is the value the leaking fixture persists. It must appear in no
// output.
const sentinel = "Roe, Richard"

const testPolicy = `version = 1
protected_roots = ["pbip"]

[scan]
report  = ["pbip/**/*.json"]
prose   = ["notes/**/*.md"]
exclude = ["**/.pbi/**"]

[files]
local_state = ["**/.pbi/localSettings.json"]

[columns]
block = ["full_name", "student_id"]
warn  = ["cohort_year"]

[shapes]
person_name = true
email       = true
`

// leakingVisual persists a selection on a block column. No final newline, as
// Desktop writes it.
const leakingVisual = `{
  "name": "0001",
  "visual": {
    "visualType": "slicer",
    "objects": {
      "general": [
        {
          "properties": {
            "filter": {
              "filter": {
                "Version": 2,
                "From": [{"Name": "s", "Entity": "student", "Type": 0}],
                "Where": [
                  {
                    "Condition": {
                      "In": {
                        "Expressions": [
                          {"Column": {"Expression": {"SourceRef": {"Source": "s"}}, "Property": "full_name"}}
                        ],
                        "Values": [[{"Literal": {"Value": "'` + sentinel + `'"}}]]
                      }
                    }
                  }
                ]
              }
            }
          }
        }
      ]
    }
  }
}`

var fixture = map[string]string{
	".pbipx.toml": testPolicy,
	"pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json": leakingVisual,
	"notes/plan.md": "Plan the next release.\n",
}

// hook is a prepared repository and the events to send into it.
type hook struct {
	repo string
	// config is PBIPX_CONFIG_DIR for the hook: a store in which the repo's
	// policy is approved.
	config string
	deny   []byte // a Read of the leaking visual
	allow  []byte // a Read of a clean note outside the protected root
}

func newHook(t *testing.T) *hook {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	repo := t.TempDir()
	for name, content := range fixture {
		p := filepath.Join(repo, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git := exec.Command("git", "init", "-q", repo) //nolint:gosec // G204: the repository is the test's own temporary directory
	git.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	if out, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	event := func(path string) []byte {
		data, err := json.Marshal(map[string]any{
			"session_id":      "00000000-0000-0000-0000-000000000000",
			"hook_event_name": "PreToolUse",
			"cwd":             repo,
			"tool_name":       "Read",
			"tool_input":      map[string]string{"file_path": filepath.Join(repo, filepath.FromSlash(path))},
		})
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	config := t.TempDir()
	digest, err := approval.DigestFile(filepath.Join(repo, ".pbipx.toml"))
	if err != nil {
		t.Fatal(err)
	}
	id, err := gate.Identify(repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := (&approval.Store{Dir: config}).Approve(id, digest, time.Now()); err != nil {
		t.Fatal(err)
	}
	return &hook{
		repo:   repo,
		config: config,
		deny:   event("pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json"),
		allow:  event("notes/plan.md"),
	}
}

// built is the binary the tests build, once.
var built struct {
	once sync.Once
	data []byte
	err  string
}

// buildPbipx writes the pbipx binary into dir, building it on first use.
func buildPbipx(t *testing.T, dir string) string {
	t.Helper()
	name := "pbipx"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	built.once.Do(func() {
		tmp, err := os.MkdirTemp("", "pbipx-build-")
		if err != nil {
			built.err = err.Error()
			return
		}
		defer os.RemoveAll(tmp)
		bin := filepath.Join(tmp, name)
		cmd := exec.Command("go", "build", "-o", bin, "github.com/craigalodon/pbipx/cmd/pbipx") //nolint:gosec // G204: the output path is the test's own temporary directory
		if out, err := cmd.CombinedOutput(); err != nil {
			built.err = "building pbipx: " + err.Error() + "\n" + string(out)
			return
		}
		built.data, err = os.ReadFile(bin) //nolint:gosec // G304: the binary this test built
		if err != nil {
			built.err = err.Error()
		}
	})
	if built.err != "" {
		t.Fatal(built.err)
	}
	bin := filepath.Join(dir, name)
	if err := os.WriteFile(bin, built.data, 0o700); err != nil { //nolint:gosec // G306: the binary must be executable
		t.Fatal(err)
	}
	return bin
}

type result struct {
	exit           int
	stdout, stderr string
}

// runHook runs the configured command string through the shell Claude Code
// would use for it, in h's repository and with h's approval store.
func runHook(t *testing.T, h *hook, command string, stdin []byte) result {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command) //nolint:gosec // G204: the command is the hook under test
	} else {
		cmd = exec.Command("sh", "-c", command) //nolint:gosec // G204: the command is the hook under test
	}
	cmd.Dir = h.repo
	cmd.Env = append(os.Environ(), approval.EnvConfigDir+"="+h.config)
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	r := result{stdout: stdout.String(), stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
	case errors.As(err, &exitErr):
		r.exit = exitErr.ExitCode()
	default:
		t.Fatalf("running the shell: %v", err)
	}
	if strings.Contains(r.stdout+r.stderr, sentinel) {
		t.Error("the sentinel appears in the hook's output")
	}
	return r
}

// wantDeny checks a deny of the given class: exit 2 and the reason as the
// first line of stderr.
func wantDeny(t *testing.T, r result, class string) {
	t.Helper()
	if r.exit != claudecode.ExitDeny {
		t.Fatalf("exit = %d, want %d (a deny)", r.exit, claudecode.ExitDeny)
	}
	if !strings.HasPrefix(r.stderr, "pbipx: "+class+":") {
		t.Errorf("stderr does not start with \"pbipx: %s:\"", class)
	}
}

func TestHookCommandDecides(t *testing.T) {
	h := newHook(t)
	bin := buildPbipx(t, t.TempDir())
	command := claudecode.HookCommand(bin, runtime.GOOS)

	t.Run("deny", func(t *testing.T) {
		r := runHook(t, h, command, h.deny)
		wantDeny(t, r, "finding")
		var out struct {
			HookSpecificOutput struct {
				HookEventName            string `json:"hookEventName"`
				PermissionDecision       string `json:"permissionDecision"`
				PermissionDecisionReason string `json:"permissionDecisionReason"`
			} `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal([]byte(r.stdout), &out); err != nil {
			t.Fatalf("stdout is not the hook JSON: %v", err)
		}
		o := out.HookSpecificOutput
		first, _, _ := strings.Cut(r.stderr, "\n")
		if o.HookEventName != "PreToolUse" || o.PermissionDecision != "deny" || o.PermissionDecisionReason != first {
			t.Errorf("hook JSON = %+v; want a PreToolUse deny with stderr's reason", o)
		}
	})
	t.Run("allow", func(t *testing.T) {
		r := runHook(t, h, command, h.allow)
		if r.exit != claudecode.ExitAllow || r.stdout != "" || r.stderr != "" {
			t.Errorf("exit = %d, %d bytes on stdout, %d on stderr; want 0 and no output", r.exit, len(r.stdout), len(r.stderr))
		}
	})
	t.Run("unapproved policy", func(t *testing.T) {
		unapproved := *h
		unapproved.config = t.TempDir()
		wantDeny(t, runHook(t, &unapproved, command, h.allow), "policy-unapproved")
	})
	t.Run("malformed event", func(t *testing.T) {
		wantDeny(t, runHook(t, h, command, []byte(sentinel)), "event-error")
	})
	t.Run("usage error", func(t *testing.T) {
		wantDeny(t, runHook(t, h, strings.Replace(command, "claude-code", "other-agent", 1), h.allow), "internal-error")
	})
}

// TestHookCommandScopesToAdoptedRepositories runs the hook where Claude
// Code runs it once pbipx is installed: in every session on the machine.
// Outside the adopted repository a call allows, unless it reaches into it.
func TestHookCommandScopesToAdoptedRepositories(t *testing.T) {
	h := newHook(t)
	bin := buildPbipx(t, t.TempDir())
	command := claudecode.HookCommand(bin, runtime.GOOS)

	// A repository with the same leaking files and no policy, and no
	// record in the store: it has not adopted pbipx.
	plain := t.TempDir()
	for name, content := range fixture {
		if name == ".pbipx.toml" {
			continue
		}
		p := filepath.Join(plain, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git := exec.Command("git", "init", "-q", plain) //nolint:gosec // G204: the repository is the test's own temporary directory
	git.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull)
	if out, err := git.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	outside := t.TempDir()
	visual := filepath.FromSlash("pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json")

	in := func(dir string) *hook {
		at := *h
		at.repo = dir
		return &at
	}
	allowed := func(t *testing.T, r result) {
		t.Helper()
		if r.exit != claudecode.ExitAllow || r.stdout != "" || r.stderr != "" {
			t.Errorf("exit = %d, %d bytes on stdout, %d on stderr; want an allow", r.exit, len(r.stdout), len(r.stderr))
		}
	}
	t.Run("not adopted, read of a leak", func(t *testing.T) {
		allowed(t, runHook(t, in(plain), command, toolEvent(t, plain, "Read", "file_path", filepath.Join(plain, visual))))
	})
	t.Run("not adopted, cat of a leak", func(t *testing.T) {
		allowed(t, runHook(t, in(plain), command, toolEvent(t, plain, "Bash", "command", "cat "+visual)))
	})
	t.Run("outside every repository", func(t *testing.T) {
		allowed(t, runHook(t, in(outside), command, toolEvent(t, outside, "Bash", "command", "ls")))
	})
	t.Run("adopted, read of a leak", func(t *testing.T) {
		wantDeny(t, runHook(t, h, command, h.deny), "finding")
	})
	t.Run("not adopted, read into the adopted repository", func(t *testing.T) {
		wantDeny(t, runHook(t, in(plain), command, toolEvent(t, plain, "Read", "file_path", filepath.Join(h.repo, visual))), "finding")
	})
	t.Run("outside, cat into the adopted repository", func(t *testing.T) {
		wantDeny(t, runHook(t, in(outside), command, toolEvent(t, outside, "Bash", "command", "cat '"+filepath.Join(h.repo, visual)+"'")), "finding")
	})
	// PowerShell cannot be parsed: in the adopted repository any call
	// scans the protected tree; elsewhere, touching nothing, it allows.
	t.Run("adopted, powershell over a leaking tree", func(t *testing.T) {
		wantDeny(t, runHook(t, h, command, toolEvent(t, h.repo, "PowerShell", "command", "Get-ChildItem")), "finding")
	})
	t.Run("not adopted, powershell", func(t *testing.T) {
		allowed(t, runHook(t, in(plain), command, toolEvent(t, plain, "PowerShell", "command", "Get-Content "+visual)))
	})
	t.Run("outside, powershell into the adopted repository", func(t *testing.T) {
		wantDeny(t, runHook(t, in(outside), command,
			toolEvent(t, outside, "PowerShell", "command", "Get-Content '"+filepath.Join(h.repo, "notes")+"'")), "finding")
	})
	t.Run("powershell without a command", func(t *testing.T) {
		wantDeny(t, runHook(t, in(outside), command, toolEvent(t, outside, "PowerShell", "description", "x")), "event-error")
	})
	t.Run("adopted, lsp on a leaking file", func(t *testing.T) {
		wantDeny(t, runHook(t, h, command, toolEvent(t, h.repo, "LSP", "filePath", filepath.Join(h.repo, visual))), "finding")
	})
}

// toolEvent is a PreToolUse event of tool with one input, from cwd.
func toolEvent(t *testing.T, cwd, tool, key, value string) []byte {
	t.Helper()
	data, err := json.Marshal(map[string]any{
		"session_id":      "00000000-0000-0000-0000-000000000000",
		"hook_event_name": "PreToolUse",
		"cwd":             cwd,
		"tool_name":       tool,
		"tool_input":      map[string]string{key: value},
	})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// TestHookCommandPathQuoting runs the binary from a path a shell would split
// or expand, with an allow event: exit 0 proves the gate itself ran.
func TestHookCommandPathQuoting(t *testing.T) {
	h := newHook(t)
	dir := filepath.Join(t.TempDir(), "it's a $HOME `dir`")
	if err := os.Mkdir(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	bin := buildPbipx(t, dir)
	r := runHook(t, h, claudecode.HookCommand(bin, runtime.GOOS), h.allow)
	if r.exit != claudecode.ExitAllow {
		t.Errorf("exit = %d, want %d", r.exit, claudecode.ExitAllow)
	}
}

// TestHookCommandFailsClosed points the hook at binaries that cannot decide.
// Each must deny, with the event an allow would pass.
func TestHookCommandFailsClosed(t *testing.T) {
	h := newHook(t)
	dir := t.TempDir()
	write := func(name, content string, mode os.FileMode) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := map[string]string{
		"missing":   filepath.Join(dir, "absent", "pbipx"),
		"directory": dir,
	}
	if runtime.GOOS == "windows" {
		cases["exits 1"] = write("exit1.cmd", "@exit /b 1\r\n", 0o600)
		cases["exits 3"] = write("exit3.cmd", "@exit /b 3\r\n", 0o600)
		cases["not a program"] = write("text.exe", "not a program\r\n", 0o600)
	} else {
		noexec := buildPbipx(t, t.TempDir())
		if err := os.Chmod(noexec, 0o600); err != nil {
			t.Fatal(err)
		}
		cases["not executable"] = noexec
		cases["exits 1"] = write("exit1", "#!/bin/sh\nexit 1\n", 0o700)
		cases["exits 3"] = write("exit3", "#!/bin/sh\nexit 3\n", 0o700)
		cases["exits 126"] = write("exit126", "#!/bin/sh\nexit 126\n", 0o700)
		cases["killed"] = write("killed", "#!/bin/sh\nkill -KILL $$\n", 0o700)
		cases["crashes"] = write("crashes", "#!/bin/sh\nkill -SEGV $$\n", 0o700)
		cases["partial output then crash"] = write("partial", "#!/bin/sh\nprintf '{\"hookSpecificOutput\":' \nkill -ABRT $$\n", 0o700)
		cases["bad interpreter"] = write("badinterp", "#!/nonexistent/interpreter\n", 0o700)
	}
	for name, bin := range cases {
		t.Run(name, func(t *testing.T) {
			r := runHook(t, h, claudecode.HookCommand(bin, runtime.GOOS), h.allow)
			if r.exit != claudecode.ExitDeny {
				t.Fatalf("exit = %d, want %d (a deny)", r.exit, claudecode.ExitDeny)
			}
			if !strings.Contains(r.stderr, "pbipx: internal-error:") {
				t.Error("stderr has no internal-error reason")
			}
		})
	}
}

// TestHookCommandFailsClosedUnderErrexit runs the configured string with
// errexit on before its first line: SHELLOPTS=errexit, which bash reads,
// and a BASH_ENV script that runs set -e. Under errexit a binary exiting 1
// would end the shell with 1, which Claude Code proceeds on, unless the
// command turns errexit off first.
func TestHookCommandFailsClosedUnderErrexit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the POSIX command form")
	}
	h := newHook(t)
	dir := t.TempDir()
	script := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o700); err != nil { //nolint:gosec // G306: the script must be executable
			t.Fatal(err)
		}
		return p
	}
	bins := map[string]string{
		"exits 1": script("exit1", "#!/bin/sh\nexit 1\n"),
		"missing": filepath.Join(dir, "absent", "pbipx"),
		"killed":  script("killed", "#!/bin/sh\nkill -KILL $$\n"),
	}
	bashEnv := script("bash_env", "set -e\n")
	type shell struct {
		name, path string
		env        []string
	}
	var shells []shell
	if bash, err := exec.LookPath("bash"); err == nil {
		shells = append(shells,
			shell{"bash, SHELLOPTS=errexit", bash, []string{"SHELLOPTS=errexit"}},
			shell{"bash, BASH_ENV sets -e", bash, []string{"BASH_ENV=" + bashEnv}})
	}
	shells = append(shells,
		shell{"sh, SHELLOPTS=errexit", "sh", []string{"SHELLOPTS=errexit"}},
		shell{"sh", "sh", nil})
	for _, sh := range shells {
		for name, bin := range bins {
			t.Run(sh.name+"/"+name, func(t *testing.T) {
				cmd := exec.Command(sh.path, "-c", claudecode.HookCommand(bin, runtime.GOOS)) //nolint:gosec // G204: the command is the hook under test
				cmd.Dir = h.repo
				cmd.Env = append(os.Environ(), approval.EnvConfigDir+"="+h.config)
				cmd.Env = append(cmd.Env, sh.env...)
				cmd.Stdin = bytes.NewReader(h.allow)
				var stderr bytes.Buffer
				cmd.Stderr = &stderr
				code := 0
				var exitErr *exec.ExitError
				switch err := cmd.Run(); {
				case err == nil:
				case errors.As(err, &exitErr):
					code = exitErr.ExitCode()
				default:
					t.Fatalf("running the shell: %v", err)
				}
				if code != claudecode.ExitDeny {
					t.Fatalf("exit = %d, want %d (a deny)", code, claudecode.ExitDeny)
				}
				if !strings.Contains(stderr.String(), "pbipx: internal-error:") {
					t.Error("stderr has no internal-error reason")
				}
			})
		}
	}
}
