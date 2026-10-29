package corpus

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/approval"
)

func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
}

// gitOut runs git in dir, isolated as GitSetup is, and returns its output.
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...) //nolint:gosec // G204: the tests' own fixed git queries
	cmd.Dir = dir
	cmd.Env = GitEnv(os.Environ())
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %s: %v", args[0], err)
	}
	return strings.TrimSpace(string(out))
}

func TestPrepare(t *testing.T) {
	needGit(t)
	c := openTest(t, testCorpusDir)
	root := t.TempDir()
	if _, err := c.Prepare(findCase(t, c, staging), root, t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}

	policy, err := readIn(root, policyFile)
	if err != nil {
		t.Fatal(err)
	}
	if want, _ := os.ReadFile(filepath.Join(testCorpusDir, policiesDir, "minimal.toml")); string(policy) != string(want) {
		t.Error("the policy written is not policies/minimal.toml")
	}
	if got := gitOut(t, root, "ls-tree", "-r", "--name-only", "HEAD"); got != ".pbipx.toml\nsql/committed.sql" {
		t.Errorf("committed files = %q", got)
	}
	if got := gitOut(t, root, "diff", "--cached", "--name-only"); got != "sql/staged.sql" {
		t.Errorf("staged files = %q", got)
	}
	if got := gitOut(t, root, "ls-files", "--others"); got != "sql/untracked.sql" {
		t.Errorf("untracked files = %q", got)
	}
	want := GitName + " <" + GitEmail + "> 4102444800"
	if got := gitOut(t, root, "log", "-1", "--format=%an <%ae> %at%n%cn <%ce> %ct"); got != want+"\n"+want {
		t.Errorf("author and committer = %q, want %q twice", got, want)
	}
	if got := gitOut(t, root, "rev-parse", "--abbrev-ref", "HEAD"); got != "main" {
		t.Errorf("branch = %q", got)
	}
}

func TestPrepareWithoutGit(t *testing.T) {
	c := openTest(t, testCorpusDir)
	root := t.TempDir()
	if _, err := c.Prepare(findCase(t, c, usage), root, t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
		t.Error("git = false made a repository")
	}
	if _, err := os.Stat(filepath.Join(root, policyFile)); err != nil {
		t.Error("the policy was not written")
	}
}

// TestPrepareApproves checks that a gate case runs against an approved
// policy unless it says approved = false, that after/tree/ is applied after
// the approval, and that no other case is approved.
func TestPrepareApproves(t *testing.T) {
	needGit(t)
	tests := []struct {
		name   string
		id     string
		mutate func(t *testing.T, dir string)
		want   approval.Status
	}{
		{"gate case", gate, nil, approval.Approved},
		{"approved = false", gate, appendTo("case.toml", "approved = false\n"), approval.Unapproved},
		{"policy changed by after/tree/", gate, func(t *testing.T, dir string) {
			data, err := os.ReadFile(filepath.Join(testCorpusDir, policiesDir, "minimal.toml"))
			if err != nil {
				t.Fatal(err)
			}
			write("case.toml", strings.Replace(string(readCase(t, dir)), `"minimal"`, `"tree"`, 1))(t, dir)
			write("tree/"+policyFile, string(data))(t, dir)
			write("after/tree/"+policyFile, string(data)+"# edited\n")(t, dir)
		}, approval.Changed},
		{"not a gate case", slicer, nil, approval.Unapproved},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := copyTestCorpus(t)
			if tt.mutate != nil {
				tt.mutate(t, filepath.Join(dir, filepath.FromSlash(tt.id)))
			}
			c := openTest(t, dir)
			cs := findCase(t, c, tt.id)
			if errs := c.Validate(cs); len(errs) > 0 {
				t.Fatal(errs)
			}
			root, config := t.TempDir(), t.TempDir()
			p, err := c.Prepare(cs, root, config, "")
			if err != nil {
				t.Fatal(err)
			}
			if p.ConfigDir != config {
				t.Error("Prepared.ConfigDir is not the configuration directory")
			}
			st, err := (&approval.Store{Dir: config}).Status(root, filepath.Join(root, policyFile))
			if err != nil {
				t.Fatal(err)
			}
			if st != tt.want {
				t.Errorf("status = %s, want %s", st, tt.want)
			}
		})
	}
}

func readCase(t *testing.T, dir string) []byte {
	t.Helper()
	data, err := readIn(dir, caseFile)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestPrepareNeedsAConfigDir(t *testing.T) {
	c := openTest(t, testCorpusDir)
	for _, config := range []string{"", "relative"} {
		if _, err := c.Prepare(findCase(t, c, gate), t.TempDir(), config, ""); err == nil {
			t.Errorf("Prepare accepted the configuration directory %q", config)
		}
	}
}

func TestPrepareKeepsReportBytes(t *testing.T) {
	c := openTest(t, testCorpusDir)
	root := t.TempDir()
	if _, err := c.Prepare(findCase(t, c, slicer), root, t.TempDir(), ""); err != nil {
		t.Fatal(err)
	}
	rel := filepath.FromSlash(strings.TrimPrefix(visual, "tree/"))
	got, err := readIn(root, rel)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := os.ReadFile(filepath.Join(testCorpusDir, slicer, filepath.FromSlash(visual)))
	if string(got) != string(want) {
		t.Error("the copied report differs from the fixture")
	}
}

func TestGitEnvIgnoresTheCallersRepository(t *testing.T) {
	env := GitEnv([]string{"PATH=/bin", "GIT_DIR=/elsewhere/.git", "GIT_INDEX_FILE=/elsewhere/index", "GITHUB=x"})
	for _, kv := range env {
		if strings.HasPrefix(kv, "GIT_DIR=") || strings.HasPrefix(kv, "GIT_INDEX_FILE=") {
			t.Errorf("GitEnv keeps %s", strings.SplitN(kv, "=", 2)[0])
		}
	}
	for _, want := range []string{"PATH=/bin", "GITHUB=x", "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_DATE=" + GitDate} {
		if !slices.Contains(env, want) {
			t.Errorf("GitEnv lacks %s", want)
		}
	}
}

func TestCommandEnv(t *testing.T) {
	base := []string{"PATH=/bin", "CI=true", "PRE_COMMIT=1", "PBIPX_NO_REVEAL=1", "HOME=/home/example", "PBIPX_CONFIG_DIR=/home/example/.config/pbipx"}
	env := CommandEnv(base, &Spec{}, "/cfg")
	if !slices.Contains(env, "PBIPX_CONFIG_DIR=/cfg") || slices.Contains(env, "PBIPX_CONFIG_DIR=/home/example/.config/pbipx") {
		t.Error("PBIPX_CONFIG_DIR is not the case's own directory")
	}
	for _, name := range redactionEnv {
		for _, kv := range env {
			if strings.HasPrefix(kv, name+"=") {
				t.Errorf("%s is set", name)
			}
		}
	}
	env = CommandEnv(base, &Spec{Env: map[string]string{"CI": "1", "HOME": "/tmp/h"}}, "/cfg")
	if !slices.Contains(env, "CI=1") || slices.Contains(env, "CI=true") {
		t.Error("the case's CI does not replace the caller's")
	}
	if !slices.Contains(env, "HOME=/tmp/h") || slices.Contains(env, "HOME=/home/example") {
		t.Error("the case's HOME does not replace the caller's")
	}
}

func TestCommandArgs(t *testing.T) {
	c := openTest(t, testCorpusDir)
	tests := []struct {
		id   string
		spec func(*Spec)
		want []string
	}{
		{slicer, nil, []string{"scan", "--all", "--format", "json"}},
		{slicer, func(s *Spec) { s.Args = []string{"{root}/pbip"} }, []string{"scan", "/r/pbip", "--format", "json"}},
		{gate, nil, []string{"gate"}},
		{usage, nil, []string{"scan-text", "--no-such-flag", "-"}},
	}
	for _, tt := range tests {
		cs := *findCase(t, c, tt.id)
		if tt.spec != nil {
			tt.spec(&cs.Spec)
		}
		if got := CommandArgs(&cs, &Prepared{Root: "/r"}); !slices.Equal(got, tt.want) {
			t.Errorf("%s: args = %q, want %q", tt.id, got, tt.want)
		}
	}
}
