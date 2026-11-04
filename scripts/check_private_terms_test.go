// Package scripts tests the maintainer scripts. It holds no code of its own.
package scripts

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// term is synthetic. The real list never enters a test.
const term = "zzsyntheticterm"

type env struct {
	t     *testing.T
	repo  string
	list  string
	extra []string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the script is bash")
	}
	for _, tool := range []string{"bash", "git"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not installed", tool)
		}
	}
	e := &env{t: t, repo: t.TempDir()}
	e.list = filepath.Join(t.TempDir(), "terms.txt")
	e.write(e.list, "# synthetic\n\n"+term+"\n")
	e.git("init", "-q", "-b", "main")
	e.git("config", "user.name", "Test Author")
	e.git("config", "user.email", "test@example.invalid")
	e.git("config", "commit.gpgsign", "false")
	e.git("config", "tag.gpgsign", "false")
	return e
}

func (e *env) write(path, content string) {
	e.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		e.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) file(name, content string) {
	e.t.Helper()
	e.write(filepath.Join(e.repo, name), content)
}

func (e *env) git(args ...string) string {
	e.t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // test helper; arguments are literals in this file
	cmd.Dir = e.repo
	cmd.Env = append(os.Environ(), e.extra...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		e.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// commit stages everything and commits. The temporary repository has no hooks.
func (e *env) commit(msg string) {
	e.t.Helper()
	e.git("add", "-A")
	e.git("commit", "-q", "--allow-empty", "-m", msg)
}

// run runs the script and returns its exit code and combined output, and
// fails the test if the output holds the term.
func (e *env) run(args ...string) (int, string) {
	e.t.Helper()
	script, err := filepath.Abs("check-private-terms.sh")
	if err != nil {
		e.t.Fatal(err)
	}
	cmd := exec.Command("bash", append([]string{script}, args...)...) //nolint:gosec // test helper; arguments are literals in this file
	cmd.Dir = e.repo
	cmd.Env = append(os.Environ(), "PBIPX_PRIVATE_TERMS="+e.list)
	cmd.Env = append(cmd.Env, e.extra...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	code := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			e.t.Fatal(err)
		}
		code = ee.ExitCode()
	}
	if strings.Contains(strings.ToLower(out.String()), term) {
		e.t.Fatalf("output holds the term:\n%s", out.String())
	}
	return code, out.String()
}

func (e *env) want(code int, args ...string) string {
	e.t.Helper()
	got, out := e.run(args...)
	if got != code {
		e.t.Fatalf("%v: exit %d, want %d\n%s", args, got, code, out)
	}
	return out
}

// big is larger than any pipe buffer, with the marker at either end.
func big(prefix, suffix string) string {
	return prefix + strings.Repeat("filler line of text\n", 150000) + suffix
}

func TestStagedLargeFile(t *testing.T) {
	// A match at the start of a file larger than a pipe buffer once exited
	// 0: grep quit early, the writer took SIGPIPE, and pipefail read the
	// pipeline as "no match". Repeat to catch a race.
	for _, tc := range []struct {
		name     string
		content  string
		wantExit int
	}{
		{"term first", big(term+"\n", ""), 1},
		{"term last", big("", term+"\n"), 1},
		{"no term", big("", ""), 0},
		{"small term", term + "\n", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			e.file("data.txt", tc.content)
			e.git("add", "data.txt")
			for range 5 {
				out := e.want(tc.wantExit, "--staged")
				if tc.wantExit == 1 && !strings.Contains(out, "data.txt") {
					t.Fatalf("the report names no file: %s", out)
				}
			}
		})
	}
}

func TestTrackedLargeFile(t *testing.T) {
	e := newEnv(t)
	e.file("data.txt", big(term+"\n", ""))
	e.commit("add data")
	for range 3 {
		e.want(1, "--tracked")
	}
}

func TestMessageLargeFile(t *testing.T) {
	e := newEnv(t)
	msg := filepath.Join(t.TempDir(), "msg")
	e.write(msg, big(term+"\n", ""))
	e.want(1, "--message", msg)
	e.write(msg, big("", ""))
	e.want(0, "--message", msg)
}

func TestStagedPath(t *testing.T) {
	e := newEnv(t)
	e.file("dir-"+term+"/a.txt", "clean\n")
	e.git("add", "-A")
	out := e.want(1, "--staged")
	if !strings.Contains(out, "a path contains") {
		t.Fatalf("unexpected report: %s", out)
	}
}

func TestFailsClosed(t *testing.T) {
	t.Run("missing list", func(t *testing.T) {
		e := newEnv(t)
		e.list = filepath.Join(t.TempDir(), "absent")
		e.want(1, "--staged")
		e.want(1, "--history")
	})
	t.Run("empty list", func(t *testing.T) {
		e := newEnv(t)
		e.write(e.list, "# only a comment\n\n")
		e.want(1, "--staged")
		e.want(1, "--history")
	})
	t.Run("unreadable tracked file", func(t *testing.T) {
		// git ls-files names a file the working tree no longer has; the
		// read fails, and a failed read must not count as "no match".
		e := newEnv(t)
		e.file("gone.txt", "clean\n")
		e.commit("add")
		if err := os.Remove(filepath.Join(e.repo, "gone.txt")); err != nil {
			t.Fatal(err)
		}
		e.want(1, "--tracked")
	})
	t.Run("unknown mode", func(t *testing.T) {
		e := newEnv(t)
		e.want(2, "--bogus")
	})
}

func TestHistory(t *testing.T) {
	t.Run("clean", func(t *testing.T) {
		e := newEnv(t)
		e.file("a.txt", "clean\n")
		e.commit("first")
		e.file("b.txt", big("", ""))
		e.commit("second")
		e.git("tag", "-a", "-m", "release", "v1")
		e.want(0, "--history")
	})

	// Each case plants the term in one place, then removes or hides it so
	// that only history can see it.
	for _, tc := range []struct {
		name  string
		plant func(e *env)
		want  string
	}{
		{"deleted blob", func(e *env) {
			e.file("a.txt", term+"\n")
			e.commit("add")
			e.git("rm", "-q", "a.txt")
			e.commit("remove")
		}, "blob"},
		{"large deleted blob", func(e *env) {
			e.file("a.txt", big(term+"\n", ""))
			e.commit("add")
			e.git("rm", "-q", "a.txt")
			e.commit("remove")
		}, "blob"},
		{"path", func(e *env) {
			e.file(term+"/a.txt", "clean\n")
			e.commit("add")
			e.git("rm", "-q", "-r", term)
			e.commit("remove")
		}, "path of"},
		{"commit message", func(e *env) {
			e.file("a.txt", "clean\n")
			e.commit("note " + term)
		}, "message of commit"},
		{"author identity", func(e *env) {
			e.file("a.txt", "clean\n")
			e.git("add", "-A")
			e.git("-c", "user.name=Person "+term, "commit", "-q", "-m", "x")
		}, "identity of commit"},
		{"committer identity", func(e *env) {
			e.file("a.txt", "clean\n")
			e.git("add", "-A")
			e.extra = append(e.extra, "GIT_COMMITTER_EMAIL=x@"+term+".invalid")
			e.git("commit", "-q", "-m", "x")
		}, "identity of commit"},
		{"tag message", func(e *env) {
			e.file("a.txt", "clean\n")
			e.commit("first")
			e.git("tag", "-a", "-m", "tag about "+term, "v1")
		}, "message of tag"},
		{"pull ref only", func(e *env) {
			e.file("a.txt", "clean\n")
			e.commit("first")
			e.git("checkout", "-q", "-b", "side")
			e.file("b.txt", "text "+term+"\n")
			e.commit("side")
			sha := e.git("rev-parse", "HEAD")
			e.git("update-ref", "refs/pull/1/head", sha)
			e.git("checkout", "-q", "main")
			e.git("branch", "-q", "-D", "side")
		}, "blob"},
		{"ref name", func(e *env) {
			e.file("a.txt", "clean\n")
			e.commit("first")
			e.git("branch", "feature-"+term)
		}, "ref"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			tc.plant(e)
			out := e.want(1, "--history")
			if !strings.Contains(out, tc.want) {
				t.Fatalf("report lacks %q:\n%s", tc.want, out)
			}
		})
	}

	t.Run("blob report names the object and a clean path", func(t *testing.T) {
		e := newEnv(t)
		e.file("notes/a.txt", term+"\n")
		e.commit("add")
		oid := e.git("rev-parse", "HEAD:notes/a.txt")
		out := e.want(1, "--history")
		if !strings.Contains(out, "blob "+oid+" (path notes/a.txt)") {
			t.Fatalf("report lacks object and path:\n%s", out)
		}
	})
	t.Run("blob under a path with the term omits the path", func(t *testing.T) {
		e := newEnv(t)
		e.file(term+"/a.txt", term+"\n")
		e.commit("add")
		out := e.want(1, "--history")
		if strings.Contains(out, "(path") {
			t.Fatalf("a path was printed:\n%s", out)
		}
	})
}

// fakeGH puts a gh in PATH that answers from files named after the endpoint's
// last path element, or fails if there is none. It returns the directory the
// answers go in.
func fakeGH(t *testing.T, e *env) string {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not installed")
	}
	bin, answers := t.TempDir(), t.TempDir()
	script := `#!/usr/bin/env bash
ep="${@: -1}"
ep="${ep%%\?*}"
f="` + answers + `/$(basename "$ep")"
if [ -f "$f" ]; then cat "$f"; else echo "HTTP 403" >&2; exit 1; fi
`
	e.write(filepath.Join(bin, "gh"), script)
	if err := os.Chmod(filepath.Join(bin, "gh"), 0o700); err != nil { //nolint:gosec // a test stub must be executable
		t.Fatal(err)
	}
	e.extra = append(e.extra, "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, name := range []string{"issues", "comments", "releases"} {
		e.write(filepath.Join(answers, name), "[]")
	}
	e.write(filepath.Join(answers, "reviews"), "[]")
	return answers
}

func TestGitHub(t *testing.T) {
	const repo = "o/r"
	t.Run("clean", func(t *testing.T) {
		e := newEnv(t)
		a := fakeGH(t, e)
		e.write(filepath.Join(a, "issues"), `[{"number":7,"title":"t","body":"b","pull_request":{}}]`)
		e.want(0, "--github", repo)
	})
	for _, tc := range []struct{ name, file, json, want string }{
		{"pull request body", "issues", `[{"number":7,"title":"t","body":"x ` + term + `","pull_request":{}}]`, "pull request #7"},
		{"issue title", "issues", `[{"number":8,"title":"` + term + `","body":null}]`, "issue #8"},
		{"issue comment", "comments", `[{"id":5,"body":"` + term + `","issue_url":"https://x/issues/9"}]`, "issue comment 5 on #9"},
		{"release", "releases", `[{"id":3,"name":"n","tag_name":"v1","body":"` + term + `"}]`, "release 3"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			a := fakeGH(t, e)
			e.write(filepath.Join(a, tc.file), tc.json)
			out := e.want(1, "--github", repo)
			if !strings.Contains(out, tc.want) {
				t.Fatalf("report lacks %q:\n%s", tc.want, out)
			}
		})
	}
	t.Run("review", func(t *testing.T) {
		e := newEnv(t)
		a := fakeGH(t, e)
		e.write(filepath.Join(a, "issues"), `[{"number":7,"title":"t","body":"b","pull_request":{}}]`)
		e.write(filepath.Join(a, "reviews"), `[{"id":4,"body":"`+term+`"}]`)
		out := e.want(1, "--github", repo)
		if !strings.Contains(out, "review 4 on #7") {
			t.Fatalf("unexpected report:\n%s", out)
		}
	})
	t.Run("an API error is a failure", func(t *testing.T) {
		e := newEnv(t)
		a := fakeGH(t, e)
		if err := os.Remove(filepath.Join(a, "releases")); err != nil {
			t.Fatal(err)
		}
		out := e.want(1, "--github", repo)
		if !strings.Contains(out, "refused") {
			t.Fatalf("unexpected report:\n%s", out)
		}
	})
	t.Run("a bad response is a failure", func(t *testing.T) {
		e := newEnv(t)
		a := fakeGH(t, e)
		e.write(filepath.Join(a, "comments"), `{"message":"nope"}`)
		e.want(1, "--github", repo)
	})
	t.Run("a bad repository", func(t *testing.T) {
		e := newEnv(t)
		fakeGH(t, e)
		e.want(2, "--github", "not a repo")
	})
}
