package corpus

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// collect is the test corpus's one stage/collect case.
const (
	collect  = "collect/edit-comes-back"
	collectV = "pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json"
)

func TestValidateWorkflowRules(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		mutate func(t *testing.T, dir string)
		want   string
	}{
		{"work outside stage and collect", slicer, replace("case.toml", `["--all"]`, `["--all", "{work}"]`), "{work}, which only a stage or collect case has"},
		{"setup outside stage and collect", slicer, appendTo("case.toml", "setup = [[\"stage\"]]\n"), "setup is set, but only a stage or collect case runs one"},
		{"status outside stage and collect", slicer, appendTo("case.toml", "status = []\n"), "status is set, but only a stage or collect case has a work-tree status"},
		{"work-deleted outside stage and collect", slicer, appendTo("case.toml", "work-deleted = [\"a\"]\n"), "work-deleted is set, but only a stage or collect case has a working copy"},
		{"setup runs another command", collect, replace("case.toml", `"stage", "pbip`, `"scan", "pbip`), "setup[0] does not start with stage or collect"},
		{"setup entry empty", collect, replace("case.toml", `[["stage", "pbip/Example.Report", "--to", "{work}/copy"]]`, `[[]]`), "setup[0] is empty"},
		{"work-deleted unclean", collect, appendTo("case.toml", "work-deleted = [\"../x\"]\n"), "work-deleted[0] is not a clean relative path"},
		{"absent path unclean", collect, appendTo("case.toml", "expect-root-absent = [\"/x\"]\n"), "expect-root-absent[0] is not a clean relative path"},
		{"status line malformed", collect, replace("case.toml", `" M pbip`, `"M pbip`), "status[0] is not a git status --porcelain line"},
		{"expected tree with a final newline", collect, appendTo("expected/root/"+collectV, "\n"), "ends with a newline"},
		{"expected tree that does not parse", collect, write("expected/root/"+collectV, `{name`), "report JSON does not parse"},
		{"expected tree holds a sentinel", collect, func(t *testing.T, dir string) {
			replace("case.toml", "sentinels   = []", `sentinels   = ["Doe, Jane"]`)(t, dir)
			write("after/tree/copy/note.txt", "Doe, Jane\n")(t, dir)
			write("expected/root/note.txt", "Doe, Jane\n")(t, dir)
		}, "an expected tree never holds a sentinel"},
		{"expected holds a stray directory", collect, write("expected/other/x.txt", "x\n"), "expected/ holds only root/ and work/"},
		{"ok with exit 1", collect, replace("case.toml", "exit        = 0", "exit = 1"), "ok exits 0"},
		{"refusal other than findings exits 3", collect, write("expected.json", `{"refusal": "dirty-tree"}`), "a refusal other than findings exits 3"},
		{"refusal not in vocabulary", collect, func(t *testing.T, dir string) {
			replace("case.toml", "exit        = 0", "exit = 3")(t, dir)
			write("expected.json", `{"refusal": "vibes"}`)(t, dir)
		}, "refusal is not one of the vocabulary's refusals"},
		{"findings refusal with a block finding exits 1", collect, func(t *testing.T, dir string) {
			replace("case.toml", "exit        = 0", "exit = 3")(t, dir)
			write("expected.json", `{"refusal": "findings", "findings": [`+blockFinding+`]}`)(t, dir)
		}, "a block finding, with no error finding, exits 1"},
		{"findings without the findings refusal", collect, write("expected.json", `{"ok": true, "findings": []}`), "findings only go with the refusal findings"},
		{"findings refusal without findings", collect, func(t *testing.T, dir string) {
			replace("case.toml", "exit        = 0", "exit = 3")(t, dir)
			write("expected.json", `{"refusal": "findings"}`)(t, dir)
		}, "the refusal findings needs findings"},
		{"ok and refusal", collect, write("expected.json", `{"ok": true, "refusal": "dirty-tree"}`), "exactly one of ok and refusal"},
		{"neither ok nor refusal", collect, write("expected.json", `{}`), "exactly one of ok and refusal"},
		{"unknown key", collect, write("expected.json", `{"ok": true, "done": 1}`), `unknown key "done"`},
		{"usage error with a result", collect, func(t *testing.T, dir string) {
			replace("case.toml", "exit        = 0", "exit = 4")(t, dir)
		}, "present, but a usage error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := copyTestCorpus(t)
			tt.mutate(t, filepath.Join(dir, filepath.FromSlash(tt.id)))
			c := openTest(t, dir)
			var all []string
			for _, cs := range c.Cases {
				for _, e := range c.Validate(cs) {
					all = append(all, e.Error())
				}
			}
			found := false
			for _, e := range all {
				if strings.Contains(e, testSentinel) {
					t.Errorf("a problem prints a sentinel")
				}
				found = found || strings.Contains(e, tt.want)
			}
			if !found {
				t.Errorf("no problem contains %q; got %q", tt.want, all)
			}
		})
	}
}

func TestValidateAcceptsWorkflowRefusals(t *testing.T) {
	tests := []struct {
		name     string
		exit     string
		expected string
	}{
		{"a refusal", "3", `{"refusal": "dirty-tree"}`},
		{"findings that block", "1", `{"refusal": "findings", "findings": [` + blockFinding + `]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := copyTestCorpus(t)
			caseDir := filepath.Join(dir, filepath.FromSlash(collect))
			replace("case.toml", "exit        = 0", "exit = "+tt.exit)(t, caseDir)
			write("expected.json", tt.expected)(t, caseDir)
			c := openTest(t, dir)
			if errs := c.Validate(findCase(t, c, collect)); len(errs) != 0 {
				t.Errorf("problems: %v", errs)
			}
		})
	}
}

func TestCompareWorkflowResults(t *testing.T) {
	c := openTest(t, testCorpusDir)
	parse := func(out string) *Result {
		res, problems := ParseResult([]byte(out), commandCollect, c.Vocab, false)
		if len(problems) > 0 {
			t.Fatalf("output does not parse: %q", problems)
		}
		return res
	}
	const finding = `{"path": "a.json", "context": "filter", "column": "student.full_name", "tier": "block", "reasons": ["block-column"]}`
	tests := []struct {
		name string
		want string
		got  string
		diff string
	}{
		{"ok matches", `{"ok": true}`, `{"ok": true, "files": 3}`, ""},
		{"ok expected, refusal got", `{"ok": true}`, `{"refusal": "dirty-tree"}`, "got refusal dirty-tree, want ok"},
		{"refusal expected, ok got", `{"refusal": "dirty-tree"}`, `{"ok": true}`, "got ok, want refusal dirty-tree"},
		{"different refusal", `{"refusal": "dirty-tree"}`, `{"refusal": "sync-folder"}`, "got refusal sync-folder, want refusal dirty-tree"},
		{"findings match", `{"refusal": "findings", "findings": [` + finding + `]}`, `{"refusal": "findings", "findings": [` + finding + `]}`, ""},
		{"finding missing", `{"refusal": "findings", "findings": [` + finding + `]}`, `{"refusal": "findings", "findings": []}`, "missing finding"},
		{"finding unexpected", `{"refusal": "findings", "findings": []}`, `{"refusal": "findings", "findings": [` + finding + `]}`, "unexpected finding"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffs := CompareResults(parse(tt.want), parse(tt.got))
			switch {
			case tt.diff == "" && len(diffs) != 0:
				t.Errorf("differences: %q", diffs)
			case tt.diff != "" && (len(diffs) != 1 || !strings.Contains(diffs[0], tt.diff)):
				t.Errorf("differences = %q, want one containing %q", diffs, tt.diff)
			}
		})
	}
}

func TestCommandArgsWork(t *testing.T) {
	c := openTest(t, testCorpusDir)
	cs := findCase(t, c, collect)
	got := CommandArgs(cs, &Prepared{Root: "/r", Work: "/w"})
	if want := []string{"collect", "/w/copy", "--format", "json"}; !slices.Equal(got, want) {
		t.Errorf("args = %q, want %q", got, want)
	}
	setup := SetupArgs(cs.Spec.Setup[0], &Prepared{Root: "/r", Work: "/w"})
	if want := []string{"stage", "pbip/Example.Report", "--to", "/w/copy"}; !slices.Equal(setup, want) {
		t.Errorf("setup args = %q, want %q", setup, want)
	}
}

// fakeBinary writes a program that stands in for `pbipx stage`: it makes the
// directory named by its fourth argument and puts one file in it. With fail
// set it exits 3 instead.
func fakeBinary(t *testing.T, fail bool) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake binary is a shell script")
	}
	body := "#!/bin/sh\nmkdir -p \"$4\"\nprintf staged > \"$4/staged.txt\"\n"
	if fail {
		body = "#!/bin/sh\nexit 3\n"
	}
	bin := filepath.Join(t.TempDir(), "fake")
	if err := os.WriteFile(bin, []byte(body), 0o700); err != nil { //nolint:gosec // G306: a test program that must be executable
		t.Fatal(err)
	}
	return bin
}

func TestPrepareWorkflow(t *testing.T) {
	needGit(t)
	c := openTest(t, testCorpusDir)
	cs := findCase(t, c, collect)
	root, config := t.TempDir(), t.TempDir()
	p, err := c.Prepare(cs, root, config, fakeBinary(t, false))
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(p.Work) {
		t.Fatalf("Work = %q is not absolute", p.Work)
	}
	if rel, err := filepath.Rel(root, p.Work); err != nil || !strings.HasPrefix(rel, "..") {
		t.Errorf("Work %q is inside the root %q", p.Work, root)
	}
	if rel, err := filepath.Rel(config, p.Work); err != nil || !strings.HasPrefix(rel, "..") {
		t.Errorf("Work %q is inside the configuration directory", p.Work)
	}
	if _, err := os.Stat(filepath.Join(p.Work, "copy", "staged.txt")); err != nil {
		t.Errorf("the setup command did not run: %v", err)
	}
	saved, err := os.ReadFile(filepath.Join(p.Work, "copy", filepath.FromSlash(collectV)))
	if err != nil || !strings.Contains(string(saved), "slicer") {
		t.Errorf("after/tree/ was not applied over the working copy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "copy")); !os.IsNotExist(err) {
		t.Error("after/tree/ was applied to the root")
	}
	if got := gitOut(t, root, "status", "--porcelain"); got != "" {
		t.Errorf("the root is not clean after setup: %q", got)
	}
}

func TestPrepareWorkflowDeletes(t *testing.T) {
	needGit(t)
	dir := copyTestCorpus(t)
	appendTo("case.toml", "work-deleted = [\"copy/staged.txt\"]\n")(t, filepath.Join(dir, filepath.FromSlash(collect)))
	c := openTest(t, dir)
	cs := findCase(t, c, collect)
	if errs := c.Validate(cs); len(errs) != 0 {
		t.Fatal(errs)
	}
	p, err := c.Prepare(cs, t.TempDir(), t.TempDir(), fakeBinary(t, false))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(p.Work, "copy", "staged.txt")); !os.IsNotExist(err) {
		t.Error("work-deleted did not remove the file")
	}
}

func TestPrepareWorkflowSetupFailure(t *testing.T) {
	needGit(t)
	c := openTest(t, testCorpusDir)
	_, err := c.Prepare(findCase(t, c, collect), t.TempDir(), t.TempDir(), fakeBinary(t, true))
	if err == nil || !strings.Contains(err.Error(), "setup[0]") {
		t.Errorf("a failing setup command: err = %v", err)
	}
}

// TestCheckTrees checks the expected-tree comparison: bytes of files under
// expected/root and expected/work, paths that must be absent, and the
// root's git status.
func TestCheckTrees(t *testing.T) {
	needGit(t)
	c := openTest(t, testCorpusDir)
	cs := findCase(t, c, collect)
	root := t.TempDir()
	p, err := c.Prepare(cs, root, t.TempDir(), fakeBinary(t, false))
	if err != nil {
		t.Fatal(err)
	}
	// What a correct collect does: the saved visual lands in the root.
	if err := copyTree(filepath.Join(cs.Dir, "expected", "root"), root); err != nil {
		t.Fatal(err)
	}
	if errs := CheckTrees(cs, p); len(errs) != 0 {
		t.Fatalf("a correct outcome: %v", errs)
	}

	visual := filepath.Join(root, filepath.FromSlash(collectV))
	tests := []struct {
		name   string
		change func(t *testing.T)
		spec   func(s *Spec)
		want   string
	}{
		{"a file differs by a final newline", func(t *testing.T) {
			data, _ := os.ReadFile(visual)                                          //nolint:gosec // G304: a path under the test's own temporary directory
			if err := os.WriteFile(visual, append(data, '\n'), 0o600); err != nil { //nolint:gosec // G703: a path under the test's own temporary directory
				t.Fatal(err)
			}
		}, nil, "expected/root/" + collectV + " differs"},
		{"a file is missing", func(t *testing.T) {
			if err := os.Remove(visual); err != nil {
				t.Fatal(err)
			}
		}, nil, "expected/root/" + collectV + " is missing"},
		{"a path that must be absent exists", nil, func(s *Spec) {
			s.ExpectRootAbsent = []string{collectV}
		}, "expect-root-absent: " + collectV + " exists"},
		{"a working-copy path that must be absent exists", nil, func(s *Spec) {
			s.ExpectWorkAbsent = []string{"copy/staged.txt"}
		}, "expect-work-absent: copy/staged.txt exists"},
		{"the status differs", nil, func(s *Spec) {
			none := []string{}
			s.Status = &none
		}, "status: unexpected"},
		{"the status lacks a line", func(t *testing.T) {
			if err := os.WriteFile(visual, []byte(`{"name":"0001","visual":{"visualType":"card"}}`), 0o600); err != nil {
				t.Fatal(err)
			}
		}, nil, "status: missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Each case starts from the correct outcome.
			good, _ := os.ReadFile(filepath.Join(cs.Dir, "expected", "root", filepath.FromSlash(collectV)))
			if err := os.WriteFile(visual, good, 0o600); err != nil { //nolint:gosec // G703: a path under the test's own temporary directory
				t.Fatal(err)
			}
			if tt.change != nil {
				tt.change(t)
			}
			changed := *cs
			if tt.spec != nil {
				tt.spec(&changed.Spec)
			}
			errs := CheckTrees(&changed, p)
			found := false
			for _, e := range errs {
				if strings.Contains(e.Error(), `"slicer"`) || strings.Contains(e.Error(), `"card"`) {
					t.Errorf("a problem prints file content: %v", e)
				}
				found = found || strings.Contains(e.Error(), tt.want)
			}
			if !found {
				t.Errorf("no problem contains %q; got %v", tt.want, errs)
			}
		})
	}
}

func TestCheckTreesUncheckedWithoutKeys(t *testing.T) {
	needGit(t)
	dir := copyTestCorpus(t)
	caseDir := filepath.Join(dir, filepath.FromSlash(collect))
	replace("case.toml", "status      = [\" M "+collectV+"\"]\n", "")(t, caseDir)
	if err := os.RemoveAll(filepath.Join(caseDir, "expected")); err != nil {
		t.Fatal(err)
	}
	c := openTest(t, dir)
	cs := findCase(t, c, collect)
	if errs := c.Validate(cs); len(errs) != 0 {
		t.Fatal(errs)
	}
	p, err := c.Prepare(cs, t.TempDir(), t.TempDir(), fakeBinary(t, false))
	if err != nil {
		t.Fatal(err)
	}
	if errs := CheckTrees(cs, p); len(errs) != 0 {
		t.Errorf("a case with no tree checks: %v", errs)
	}
}
