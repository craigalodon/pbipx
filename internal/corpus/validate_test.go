package corpus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The package's own synthetic cases, and the real vocabulary they use.
const (
	testCorpusDir = "testdata/corpus"
	vocabPath     = "../../testdata/corpus/vocabulary.toml"
)

// testSentinel is planted in the test cases. No problem may print it.
const testSentinel = "Doe, Jane"

// copyTestCorpus copies the test corpus to a temporary directory, so that a
// test can break it.
func copyTestCorpus(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "corpus")
	if err := copyTree(testCorpusDir, dir); err != nil {
		t.Fatal(err)
	}
	return dir
}

func openTest(t *testing.T, dir string) *Corpus {
	t.Helper()
	c, err := open(dir, vocabPath)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func findCase(t *testing.T, c *Corpus, id string) *Case {
	t.Helper()
	for _, cs := range c.Cases {
		if cs.ID() == id {
			return cs
		}
	}
	t.Fatalf("no case %s", id)
	return nil
}

func TestValidateAcceptsTestCorpus(t *testing.T) {
	c := openTest(t, testCorpusDir)
	if len(c.Cases) != 8 {
		t.Fatalf("loaded %d cases, want 8", len(c.Cases))
	}
	for _, e := range c.Problems() {
		t.Error(e)
	}
	for _, cs := range c.Cases {
		for _, e := range c.Validate(cs) {
			t.Error(e)
		}
	}
}

func TestOpenEmptyCorpus(t *testing.T) {
	dir := t.TempDir()
	c, err := open(dir, vocabPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Cases) != 0 || len(c.Problems()) != 0 {
		t.Fatalf("an empty corpus has %d cases and %d problems", len(c.Cases), len(c.Problems()))
	}
}

const (
	slicer  = "pbir/slicer-on-block-column"
	gate    = "gate/read-of-blocked-visual-denied"
	usage   = "text/unknown-flag-is-usage-error"
	staging = "sql/staged-and-untracked-stay-clean"

	visual = "tree/pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json"
)

// A finding in the slicer case, for building broken expected.json files.
const blockFinding = `{"path": "pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json",
 "context": "slicer-selection", "column": "student.full_name", "tier": "block",
 "reasons": ["block-column", "person-name-shape"]}`

func TestValidateRules(t *testing.T) {
	tests := []struct {
		name   string
		id     string
		mutate func(t *testing.T, dir string)
		want   string
	}{
		{"unknown key", slicer, appendTo("case.toml", "colour = \"blue\"\n"), `unknown key "colour"`},
		{"unknown key that is not a name", slicer, appendTo("case.toml", "\"Doe, Jane\" = 1\n"), "unknown key (not printed)"},
		{"missing key", slicer, replace("case.toml", "exit        = 1\n", ""), `required key "exit" is missing`},
		{"wrong type", slicer, replace("case.toml", "exit        = 1", `exit = "Doe, Jane"`), "a value of the wrong type"},
		{"bad command", slicer, replace("case.toml", `"scan"`, `"lint"`), "command is not one of"},
		{"policy names no file", slicer, replace("case.toml", `"minimal"`, `"absent"`), "names no file in policies/"},
		{"policy tree without a policy", slicer, replace("case.toml", `"minimal"`, `"tree"`), "tree/.pbipx.toml does not exist"},
		{"tree policy beside a named one", slicer, write("tree/.pbipx.toml", "version = 1\n"), "the runner would overwrite it"},
		{"staged path missing", staging, replace("case.toml", `"sql/staged.sql"`, `"sql/absent.sql"`), "staged[0] names no path in tree/"},
		{"untracked path unclean", staging, replace("case.toml", `"sql/untracked.sql"`, `"sql/../untracked.sql"`), "untracked[0] is not a clean relative path"},
		{"staged without git", staging, appendTo("case.toml", "git = false\n"), "git = false"},
		{"sentinel not planted", slicer, replace("case.toml", `["Doe, Jane"]`, `["Doe, Jane", "Roe, Richard"]`), "sentinels[1] appears nowhere"},
		{"sentinel repeated", slicer, replace("case.toml", `["Doe, Jane"]`, `["Doe, Jane", "Doe, Jane"]`), "sentinels[1] repeats"},
		{"sentinel in expected", slicer, replace("expected.json", `"student.full_name"`, `"Doe, Jane"`), "contains sentinels[0]"},
		{"sentinel in description", slicer, replace("case.toml", "blocks\"", "blocks: Doe, Jane\""), "the description contains sentinels[0]"},
		{"empty sentinels unexplained", staging, replace("case.toml", "; no value is planted", ""), "does not say the case plants no value"},
		{"exit not a gate exit", gate, replace("case.toml", "exit        = 2", "exit = 1"), "gate-exit-codes"},
		{"exit not an exit code", slicer, replace("case.toml", "exit        = 1", "exit = 9"), "exit-codes"},
		{"usage error with a result", usage, write("expected.json", `{"findings": []}`), "present, but a usage error, baseline record or baseline verify case has none"},
		{"result missing", slicer, remove("expected.json"), "missing: only a usage error, baseline record or baseline verify case has none"},
		{"gate without stdin", gate, remove("stdin"), "a gate case needs its hook event"},
		{"gate decision against exit", gate, write("expected.json", `{"decision": "allow"}`), "does not match exit"},
		{"gate allow with a reason", gate, write("expected.json", `{"decision": "allow", "reason": "finding"}`), "an allow has no reason"},
		{"gate reason not in vocabulary", gate, write("expected.json", `{"decision": "deny", "reason": "vibes"}`), "gate-reasons"},
		{"block finding exits 1", slicer, replace("case.toml", "exit        = 1", "exit = 0"), "exits 1"},
		{"unknown result key", slicer, replace("expected.json", `"accepted"`, `"accept"`), `unknown key "accept"`},
		{"unknown finding key", slicer, replace("expected.json", `"tier"`, `"severity"`), `findings[0] has an unknown key "severity"`},
		{"context not in vocabulary", slicer, replace("expected.json", `"slicer-selection"`, `"slicer"`), "findings[0].context is not one of"},
		{"tier not in vocabulary", slicer, replace("expected.json", `"block"`, `"high"`), "findings[0].tier is not one of"},
		{"reason not in vocabulary", slicer, replace("expected.json", `"person-name-shape"`, `"name-shape"`), "findings[0].reasons[1] is not one"},
		{"reasons out of order", slicer, replace("expected.json", `["block-column", "person-name-shape"]`, `["person-name-shape", "block-column"]`), "reason-order"},
		{"path not relative", slicer, replace("expected.json", `"pbip/Example`, `"/pbip/Example`), "findings[0].path is not a clean relative path"},
		{"finding path not in tree", slicer, replace("expected.json", `visual.json"`, `page.json"`), "findings[0].path names no path in tree/"},
		{"findings unsorted", slicer, write("expected.json", `{"findings": [`+
			strings.Replace(blockFinding, `"student.full_name"`, `"student.last_name"`, 1)+`, `+blockFinding+`]}`), "findings[1] is out of order"},
		{"error finding with a context", slicer, write("expected.json", `{"findings": [`+
			strings.Replace(blockFinding, `"block"`, `"error"`, 1)+`]}`), "context and column must be null"},
		{"error finding with a finding reason", slicer, write("expected.json",
			`{"findings": [{"path": "pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json",
			"context": null, "column": null, "tier": "error", "reasons": ["block-column"]}]}`), "exactly one error reason"},
		{"error finding exits 3", slicer, write("expected.json",
			`{"findings": [{"path": "pbip/Example.Report/definition/pages/0000/visuals/0001/visual.json",
			"context": null, "column": null, "tier": "error", "reasons": ["unparseable-json"]}]}`), "an error finding exits 3"},
		{"failure and findings", slicer, write("expected.json", `{"failure": "policy-error", "findings": []}`), "both present"},
		{"failure not in vocabulary", slicer, write("expected.json", `{"failure": "oops"}`), "failure is not one of"},
		{"failure exits 3", slicer, write("expected.json", `{"failure": "policy-error"}`), "a failure exits 3"},
		{"expected not JSON", slicer, write("expected.json", "{\n\"findings\": [Doe, Jane]}"), "not valid JSON (line 2)"},
		{"report with a final newline", slicer, appendTo(visual, "\n"), "ends with a newline"},
		{"report that does not parse", slicer, replace(visual, `{"name"`, `{name`), "report JSON does not parse"},
		{"known is not predecessor", slicer, appendTo("case.toml", "known = \"successor\"\n"), "known is set"},
		{"args set the format", slicer, replace("case.toml", `["--all"]`, `["--all", "--format=json"]`), "args[1] sets --format"},
		{"bad env name", slicer, appendTo("case.toml", "env = { \"A B\" = \"1\" }\n"), "env has a key"},
		{"env sets the config dir", slicer, appendTo("case.toml", "env = { PBIPX_CONFIG_DIR = \"/tmp\" }\n"), "which the runner owns"},
		{"approved outside the gate", slicer, appendTo("case.toml", "approved = true\n"), "only a gate case's policy is approved"},
		{"after in an unapproved gate case", gate, func(t *testing.T, dir string) {
			appendTo("case.toml", "approved = false\n")(t, dir)
			write("after/tree/README.md", "x\n")(t, dir)
		}, "or a gate case whose policy is approved"},
		{"stray file", slicer, write("notes.txt", "x\n"), `unexpected entry "notes.txt"`},
		{"case name not kebab", slicer, rename("../Slicer_Case"), "not kebab case"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := copyTestCorpus(t)
			caseDir := filepath.Join(dir, filepath.FromSlash(tt.id))
			tt.mutate(t, caseDir)
			c := openTest(t, dir)
			var all []string
			for _, cs := range c.Cases {
				for _, e := range c.Validate(cs) {
					all = append(all, e.Error())
				}
			}
			found := false
			for _, e := range all {
				if strings.Contains(e, testSentinel) || strings.Contains(e, "Roe") {
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

func TestValidateIsIdempotent(t *testing.T) {
	dir := copyTestCorpus(t)
	appendTo("case.toml", "colour = \"blue\"\n")(t, filepath.Join(dir, slicer))
	c := openTest(t, dir)
	cs := findCase(t, c, slicer)
	first := len(c.Validate(cs))
	if second := len(c.Validate(cs)); first != second || first == 0 {
		t.Errorf("validate reported %d then %d problems", first, second)
	}
}

func TestStrayAreaIsAProblem(t *testing.T) {
	dir := copyTestCorpus(t)
	if err := os.Mkdir(filepath.Join(dir, "misc"), 0o750); err != nil {
		t.Fatal(err)
	}
	if c := openTest(t, dir); len(c.Problems()) != 1 {
		t.Errorf("got %d layout problems, want 1", len(c.Problems()))
	}
}

func TestPlantsNothing(t *testing.T) {
	for desc, want := range map[string]bool{
		"the case plants no value":                  true,
		"plants nothing":                            true,
		"no value is planted":                       true,
		"a usage error, with no values in the tree": true,
		"nothing planted":                           true,
		"slicer selection blocks":                   false,
		"no finding. The value is elsewhere":        false,
	} {
		if got := plantsNothing.MatchString(desc); got != want {
			t.Errorf("plantsNothing(%q) = %v, want %v", desc, got, want)
		}
	}
}

func TestIsReportFixture(t *testing.T) {
	for p, want := range map[string]bool{
		"pbip/A.Report/definition/report.json":  true,
		"pbip/A.Report/definition.pbir":         true,
		"pbip/A.Report/.pbi/localSettings.json": true,
		"pbip/A.SemanticModel/model.json":       false,
		"pbip/A.Report/StaticResources/a.png":   false,
		"notes/report.json":                     false,
	} {
		if got := isReportFixture(p); got != want {
			t.Errorf("isReportFixture(%q) = %v, want %v", p, got, want)
		}
	}
}

func write(rel, content string) func(*testing.T, string) {
	return func(t *testing.T, dir string) {
		t.Helper()
		r, err := os.OpenRoot(dir)
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		if err := r.MkdirAll(filepath.Dir(filepath.FromSlash(rel)), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := r.WriteFile(filepath.FromSlash(rel), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func appendTo(rel, content string) func(*testing.T, string) {
	return func(t *testing.T, dir string) {
		t.Helper()
		data, err := readIn(dir, filepath.FromSlash(rel))
		if err != nil {
			t.Fatal(err)
		}
		write(rel, string(data)+content)(t, dir)
	}
}

func replace(rel, old, repl string) func(*testing.T, string) {
	return func(t *testing.T, dir string) {
		t.Helper()
		data, err := readIn(dir, filepath.FromSlash(rel))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), old) {
			t.Fatalf("%s does not contain the text to replace", rel)
		}
		write(rel, strings.Replace(string(data), old, repl, 1))(t, dir)
	}
}

func remove(rel string) func(*testing.T, string) {
	return func(t *testing.T, dir string) {
		t.Helper()
		if err := os.Remove(filepath.Join(dir, rel)); err != nil {
			t.Fatal(err)
		}
	}
}

func rename(to string) func(*testing.T, string) {
	return func(t *testing.T, dir string) {
		t.Helper()
		if err := os.Rename(dir, filepath.Join(dir, filepath.FromSlash(to))); err != nil {
			t.Fatal(err)
		}
	}
}
