package sqlfile_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/craigalodon/pbipx/internal/corpus"
	"github.com/craigalodon/pbipx/internal/finding"
	"github.com/craigalodon/pbipx/internal/policy"
	"github.com/craigalodon/pbipx/internal/scan"
	"github.com/craigalodon/pbipx/internal/sqlfile"
)

const corpusDir = "../../testdata/corpus"

// TestCorpusCases runs Scan, as a scan reads a file, over the SQL files of
// every sql scan case on the default policy and compares the normalized
// findings with the case's expected.json. A usage-error case has no
// result to compare.
func TestCorpusCases(t *testing.T) {
	c, err := corpus.Open(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := policy.Load(filepath.Join(corpusDir, "policies", "default.toml"))
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, cs := range c.Cases {
		if cs.Area != "sql" || cs.Spec.Command != "scan" || cs.Spec.Policy != "default" || cs.Spec.Exit == finding.ExitUsage {
			continue
		}
		ran++
		t.Run(cs.Name, func(t *testing.T) {
			if cs.Expected == nil {
				t.Fatal("the case has no expected result")
			}
			got := scanTree(t, cs.TreeDir(), p)
			for _, d := range corpus.CompareResults(cs.Expected, got) {
				t.Error(d)
			}
		})
	}
	if ran == 0 {
		t.Fatal("no sql cases ran")
	}
}

// scanTree scans every SQL file under dir, as a scan would classify it.
func scanTree(t *testing.T, dir string, p *policy.Policy) *corpus.Result {
	t.Helper()
	res := &corpus.Result{}
	err := filepath.WalkDir(dir, func(full string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, full)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		class, kind := p.Classify(rel)
		if class != policy.Scanned || kind != policy.SQL {
			t.Fatalf("%s: classified %s %q; this test scans SQL files only", rel, class, kind)
		}
		data, err := os.ReadFile(full) //nolint:gosec // G304: a path under the corpus directory
		if err != nil {
			return err
		}
		// scan.File is how a scan reads a file: the UTF-8 check, the byte
		// order mark, and a parse error as one error finding.
		findings := scan.File(rel, data, sqlfile.Scan, p)
		for _, f := range findings {
			if p.Accepts(f) {
				res.Accepted++
				continue
			}
			res.Findings = append(res.Findings, normalize(f))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func normalize(f finding.Finding) corpus.Finding {
	out := corpus.Finding{Path: f.Path, Tier: string(f.Tier)}
	if f.Context != "" {
		ctx := string(f.Context)
		out.Context = &ctx
	}
	if f.Column != nil {
		col := f.Column.String()
		out.Column = &col
	}
	for _, r := range f.Reasons {
		out.Reasons = append(out.Reasons, string(r))
	}
	return out
}
