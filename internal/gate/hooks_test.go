package gate_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/craigalodon/pbipx/internal/gate"
)

// TestShellIndexHooks: a post-index-change or reference-transaction hook
// runs on git commands that change nothing a later one prints (status
// refreshing the index, add, tag, branch), and can write files. With one
// in the repository's hooks directory, every git command counts as a
// writer for the working-tree rule: the tree is scanned.
func TestShellIndexHooks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hook files are made executable by mode")
	}
	sh := func(cmd string) gate.Event { return gate.Event{Tool: gate.Shell, Command: cmd} }
	finding := gate.ReasonFinding
	hook := func(t *testing.T, dir, name string, mode os.FileMode) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), mode); err != nil {
			t.Fatal(err)
		}
	}

	plain := newRepo(t, leakyFiles())
	run(t, plain, []decideCase{
		{"no hook: status, then diff", sh("git status && git diff"), ""},
		{"no hook: add, then diff", sh("git add README.md && git diff"), ""},
	})

	withIndexHook := newRepo(t, leakyFiles())
	hook(t, filepath.Join(withIndexHook, ".git", "hooks"), "post-index-change", 0o700)
	run(t, withIndexHook, []decideCase{
		{"post-index-change: status, then diff", sh("git status && git diff"), finding},
		{"post-index-change: add, then diff", sh("git add README.md && git diff"), finding},
		{"post-index-change: diff alone", sh("git diff"), ""},
	})

	withRefHook := newRepo(t, leakyFiles())
	hook(t, filepath.Join(withRefHook, ".git", "hooks"), "reference-transaction", 0o700)
	run(t, withRefHook, []decideCase{
		{"reference-transaction: tag, then diff", sh("git tag -f t && git diff"), finding},
	})

	sample := newRepo(t, leakyFiles())
	hook(t, filepath.Join(sample, ".git", "hooks"), "post-index-change.sample", 0o700)
	hook(t, filepath.Join(sample, ".git", "hooks"), "reference-transaction", 0o600) // not executable
	run(t, sample, []decideCase{
		{"a sample and a non-executable hook: status, then diff", sh("git status && git diff"), ""},
	})

	hooksPath := newRepo(t, leakyFiles())
	gitIn(t, hooksPath, "config", "core.hooksPath", "githooks")
	hook(t, filepath.Join(hooksPath, "githooks"), "post-index-change", 0o700)
	run(t, hooksPath, []decideCase{
		{"core.hooksPath: status, then diff", sh("git status && git diff"), finding},
	})
}
