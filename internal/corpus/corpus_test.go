package corpus_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/craigalodon/pbipx/internal/corpus"
)

// corpusDir is the behavior corpus (docs/design/corpus.md).
const corpusDir = "../../testdata/corpus"

// enabledAreas are the areas pbipx is expected to pass. An area is added
// here, in the change that makes it pass (docs/ROADMAP.md, phase 2). Until
// then its cases are skipped by TestCorpus, but still validated by
// TestCorpusValid.
var enabledAreas = []string{"pbir", "text", "baseline", "policy", "sql", "yaml"}

// areasEnv overrides enabledAreas for local work: a comma-separated list of
// areas, or "all".
const areasEnv = "PBIPX_CORPUS_AREAS"

// TestCorpusValid holds every case to the corpus format.
func TestCorpusValid(t *testing.T) {
	c, err := corpus.Open(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range c.Problems() {
		t.Error(e)
	}
	for _, cs := range c.Cases {
		t.Run(cs.ID(), func(t *testing.T) {
			for _, e := range c.Validate(cs) {
				t.Error(e)
			}
		})
	}
}

// TestCorpus runs pbipx against every case of an enabled area.
func TestCorpus(t *testing.T) {
	c, err := corpus.Open(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	areas := runAreas()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH; the corpus needs it")
	}

	parent := t
	var (
		buildOnce sync.Once
		binary    string
		buildErr  string
	)
	build := func(t *testing.T) string {
		t.Helper()
		buildOnce.Do(func() { binary, buildErr = buildPbipx(parent) })
		if buildErr != "" {
			t.Fatal(buildErr)
		}
		return binary
	}

	for _, cs := range c.Cases {
		t.Run(cs.ID(), func(t *testing.T) {
			if !areas[cs.Area] {
				t.Skipf("area %s is not enabled: add it to enabledAreas in corpus_test.go, or list it in %s",
					cs.Area, areasEnv)
			}
			if errs := c.Validate(cs); len(errs) > 0 {
				for _, e := range errs {
					t.Error(e)
				}
				t.FailNow()
			}
			bin := build(t)
			root, err := filepath.EvalSymlinks(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := c.Prepare(cs, root, bin)
			if err != nil {
				t.Fatal(err)
			}
			out, err := corpus.Execute(cs, bin, prepared)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range c.Check(cs, out) {
				t.Error(e)
			}
		})
	}
}

// runAreas is the set of areas to run.
func runAreas() map[string]bool {
	list := enabledAreas
	if v, ok := os.LookupEnv(areasEnv); ok {
		list = strings.Split(v, ",")
		if strings.TrimSpace(v) == "all" {
			list = corpus.Areas
		}
	}
	set := map[string]bool{}
	for _, a := range list {
		if a = strings.TrimSpace(a); slices.Contains(corpus.Areas, a) {
			set[a] = true
		}
	}
	return set
}

// buildPbipx builds the binary into a temporary directory of t, the parent
// test, so that it outlives the subtest that asked for it. It returns a
// problem rather than failing, so that every subtest reports it.
func buildPbipx(t *testing.T) (string, string) {
	bin := filepath.Join(t.TempDir(), "pbipx")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "build", "-o", bin, "github.com/craigalodon/pbipx/cmd/pbipx") //nolint:gosec // G204: the output path is the test's own temporary directory
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", "building pbipx: " + err.Error() + "\n" + string(out)
	}
	return bin, ""
}
