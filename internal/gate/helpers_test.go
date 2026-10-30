package gate_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/craigalodon/pbipx/internal/corpus"
)

// The values planted in these tests. None may appear in a message.
var sentinels = []string{"Doe, Jane", "jane.doe@example.com", "555-0100"}

// isolateGit removes every GIT_ variable for the test (a pre-commit hook
// sets GIT_INDEX_FILE, which would point git at another index) and turns
// off user and system configuration.
func isolateGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	for _, kv := range os.Environ() {
		name, value, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "GIT_") {
			t.Setenv(name, value) // restored after the test
			_ = os.Unsetenv(name)
		}
	}
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
}

// tempRoot is a fresh empty directory. It is not resolved, so a temporary
// directory reached through a symlink is exercised too.
func tempRoot(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// bashPath is the absolute path p as a command line writes it for Git Bash,
// which runs Claude Code's shell commands on Windows: with forward slashes
// ("C:/x"), since a backslash outside quotes is an escape. Elsewhere it is
// p.
func bashPath(p string) string { return filepath.ToSlash(p) }

// defaultPolicy is the corpus's default policy.
func defaultPolicy(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(corpusDir, "policies", "default.toml"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// newRepo writes files, and the corpus's default policy unless files hold
// one, into a fresh directory and commits them.
func newRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	isolateGit(t)
	root := tempRoot(t)
	if _, ok := files[".pbipx.toml"]; !ok {
		write(t, root, ".pbipx.toml", defaultPolicy(t))
	}
	for name, content := range files {
		write(t, root, name, content)
	}
	if err := corpus.GitSetup(root, nil, nil); err != nil {
		t.Fatal(err)
	}
	return root
}

func write(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
